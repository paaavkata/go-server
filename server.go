package goserver

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	logger "github.com/paaavkata/go-logger"
	"github.com/prometheus/client_golang/prometheus"
)

// Manager coordinates the lifecycle of a service: observability server,
// signal handling, and graceful shutdown.
type Manager struct {
	cfg     Config
	health  *healthRegistry
	metrics *metricsRegistry

	shutdownHooks []shutdownHook
	hooksMu       sync.Mutex

	obsServer *http.Server
	obsOnce   sync.Once
}

type shutdownHook struct {
	name string
	fn   func(context.Context) error
}

// New creates a Manager with the given Config, applying sensible defaults for
// any zero-value fields.
func New(cfg Config) *Manager {
	cfg.applyDefaults()
	return &Manager{
		cfg:     cfg,
		health:  newHealthRegistry(cfg.CheckTimeout),
		metrics: newMetricsRegistry(cfg.ServiceName),
	}
}

// FromViper creates a Manager by reading APP_PORT, METRICS_PORT,
// SHUTDOWN_GRACE_SECONDS, READINESS_DRAIN_SECONDS, CHECK_TIMEOUT_SECONDS from
// the environment, applying defaults for absent vars.
func FromViper(serviceName string) *Manager {
	return New(configFromEnv(serviceName))
}

// AddReadinessCheck registers a dependency check run by /readyz.
// All checks must pass (and SetStarted() must have been called) for the pod
// to be ready. Suitable for: db.Ping, broker dial, etc.
// Checks must not be expensive — a per-check timeout (CheckTimeout) is applied.
func (m *Manager) AddReadinessCheck(name string, check CheckFunc) {
	m.health.addReadiness(name, check)
}

// AddLivenessCheck registers a check run by /livez. Keep these cheap and
// dependency-free — a failed liveness check triggers a pod restart.
func (m *Manager) AddLivenessCheck(name string, check CheckFunc) {
	m.health.addLiveness(name, check)
}

// SetStarted marks the service as fully initialised: /startupz returns 200 and
// /readyz begins running dependency checks. Call after migrations, seed, or any
// slow initialisation is complete.
func (m *Manager) SetStarted() {
	m.health.setStarted()
	logger.Infof("[%s] startup complete", m.cfg.ServiceName)
}

// OnShutdown registers a cleanup function run during graceful shutdown.
// Functions run in reverse registration order (LIFO), so register in
// dependency order: first what you want closed last.
//
// Example registration order: "postgres" then "kafka-consumer" → kafka-consumer
// closes first on shutdown (correct teardown order).
func (m *Manager) OnShutdown(name string, fn func(context.Context) error) {
	m.hooksMu.Lock()
	defer m.hooksMu.Unlock()
	m.shutdownHooks = append(m.shutdownHooks, shutdownHook{name: name, fn: fn})
}

// Registry returns the Prometheus registry so services can register custom
// metrics that are served on /metrics alongside the default Go/process/HTTP metrics.
func (m *Manager) Registry() *prometheus.Registry {
	return m.metrics.registry
}

// EchoMetricsMiddleware returns an Echo middleware that records
// http_requests_total, http_request_duration_seconds, and
// http_requests_in_flight, all labelled by {method, route, code}.
// The route label uses Echo's matched route pattern (bounded cardinality).
func (m *Manager) EchoMetricsMiddleware() echo.MiddlewareFunc {
	return m.metrics.echoMiddleware()
}

// StartObservability starts the observability HTTP server on :METRICS_PORT
// serving /livez, /readyz, /startupz, and /metrics. Safe to call multiple
// times; only the first call has effect. Returns immediately.
func (m *Manager) StartObservability() {
	m.obsOnce.Do(func() {
		m.obsServer = m.startObservabilityServer()
	})
}

// Run starts the business server, blocks until SIGTERM/SIGINT, then drains:
//  1. Flip /readyz to 503 (pod leaves Service endpoints).
//  2. Wait ReadinessDrainWait for endpoint-removal propagation.
//  3. Call shutdown(ctx) to drain in-flight HTTP requests.
//  4. Run OnShutdown hooks in reverse registration order.
//  5. Shutdown the observability server.
//
// The entire sequence is bounded by ShutdownGrace. serve should call
// e.Start(":"+port); shutdown should call e.Shutdown(ctx).
func (m *Manager) Run(serve func() error, shutdown func(context.Context) error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	serveErr := make(chan error, 1)
	go func() {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		} else {
			serveErr <- nil
		}
	}()

	select {
	case sig := <-sigCh:
		logger.Infof("[%s] received %s, starting graceful shutdown", m.cfg.ServiceName, sig)
	case err := <-serveErr:
		if err != nil {
			logger.Errorf("[%s] business server error: %v", m.cfg.ServiceName, err)
		}
		m.drain(shutdown)
		return
	}

	m.drain(shutdown)
}

// RunWorker is for pure workers (no business HTTP server). It blocks until
// SIGTERM/SIGINT or ctx cancellation, then drains in the same sequence as
// Run (without the HTTP drain step). Register ctx cancel as an OnShutdown
// hook so consumer loops are stopped before the process exits.
func (m *Manager) RunWorker(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		logger.Infof("[%s] received %s, shutting down worker", m.cfg.ServiceName, sig)
	case <-ctx.Done():
		logger.Infof("[%s] context cancelled, shutting down worker", m.cfg.ServiceName)
	}

	m.health.setDraining()
	if m.cfg.ReadinessDrainWait > 0 {
		time.Sleep(m.cfg.ReadinessDrainWait)
	}

	graceCtx, cancel := context.WithTimeout(context.Background(), m.cfg.ShutdownGrace)
	defer cancel()

	m.runHooks(graceCtx)
	m.stopObservability()

	logger.Infof("[%s] shutdown complete", m.cfg.ServiceName)
}

// drain executes the full shutdown sequence without blocking on signals.
func (m *Manager) drain(shutdown func(context.Context) error) {
	m.health.setDraining()
	logger.Infof("[%s] readiness 503; waiting %s for endpoint removal",
		m.cfg.ServiceName, m.cfg.ReadinessDrainWait)

	if m.cfg.ReadinessDrainWait > 0 {
		time.Sleep(m.cfg.ReadinessDrainWait)
	}

	graceCtx, cancel := context.WithTimeout(context.Background(), m.cfg.ShutdownGrace)
	defer cancel()

	if shutdown != nil {
		logger.Infof("[%s] draining business server", m.cfg.ServiceName)
		if err := shutdown(graceCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			logger.Errorf("[%s] business server shutdown: %v", m.cfg.ServiceName, err)
		}
	}

	m.runHooks(graceCtx)
	m.stopObservability()

	logger.Infof("[%s] shutdown complete", m.cfg.ServiceName)
}

func (m *Manager) runHooks(ctx context.Context) {
	m.hooksMu.Lock()
	hooks := make([]shutdownHook, len(m.shutdownHooks))
	copy(hooks, m.shutdownHooks)
	m.hooksMu.Unlock()

	for i := len(hooks) - 1; i >= 0; i-- {
		h := hooks[i]
		logger.Infof("[%s] shutdown hook: %s", m.cfg.ServiceName, h.name)
		if err := h.fn(ctx); err != nil {
			logger.Errorf("[%s] shutdown hook %q: %v", m.cfg.ServiceName, h.name, err)
		}
	}
}

func (m *Manager) stopObservability() {
	if m.obsServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.obsServer.Shutdown(ctx); err != nil {
		logger.Errorf("[%s] observability server shutdown: %v", m.cfg.ServiceName, err)
	}
}
