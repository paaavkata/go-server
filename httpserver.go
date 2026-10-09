package goserver

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// Default timeouts for the business HTTP server (ServeEcho). They bound slow or
// stalled clients so a hung connection cannot hold a worker forever. Override
// per service through the HTTP_*_TIMEOUT_SECONDS env vars; 0 disables one.
const (
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 120 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
)

// HTTPTimeouts configures the business http.Server built by ServeEcho.
type HTTPTimeouts struct {
	ReadHeader time.Duration // HTTP_READ_HEADER_TIMEOUT_SECONDS, default 10s
	Read       time.Duration // HTTP_READ_TIMEOUT_SECONDS, default 30s
	Write      time.Duration // HTTP_WRITE_TIMEOUT_SECONDS, default 120s; 0 for streaming / large downloads
	Idle       time.Duration // HTTP_IDLE_TIMEOUT_SECONDS, default 120s
}

// ServeEcho returns the (serve, shutdown) pair Run expects, backed by an
// http.Server with the Manager's timeouts instead of Echo's bare e.Start.
//
//	mgr.Run(mgr.ServeEcho(e, ":"+mgr.Config().AppPort))
func (m *Manager) ServeEcho(e *echo.Echo, addr string) (func() error, func(context.Context) error) {
	return m.ServeHTTP(e, addr)
}

// ServeHTTP is ServeEcho for any http.Handler (net/http mux, WebSocket server).
func (m *Manager) ServeHTTP(h http.Handler, addr string) (func() error, func(context.Context) error) {
	t := m.cfg.HTTP
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: t.ReadHeader,
		ReadTimeout:       t.Read,
		WriteTimeout:      t.Write,
		IdleTimeout:       t.Idle,
	}
	serve := func() error { return srv.ListenAndServe() }
	shutdown := func(ctx context.Context) error { return srv.Shutdown(ctx) }
	return serve, shutdown
}

// Config returns the effective configuration (after defaults).
func (m *Manager) Config() Config { return m.cfg }
