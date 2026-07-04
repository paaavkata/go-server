package goserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// CheckFunc is a health/readiness check. Return nil = healthy.
type CheckFunc func(ctx context.Context) error

type healthRegistry struct {
	liveness  []namedCheck
	readiness []namedCheck
	mu        sync.RWMutex

	started      atomic.Bool
	draining     atomic.Bool
	checkTimeout time.Duration
}

type namedCheck struct {
	name  string
	check CheckFunc
}

func newHealthRegistry(checkTimeout time.Duration) *healthRegistry {
	return &healthRegistry{checkTimeout: checkTimeout}
}

func (h *healthRegistry) addLiveness(name string, fn CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveness = append(h.liveness, namedCheck{name, fn})
}

func (h *healthRegistry) addReadiness(name string, fn CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readiness = append(h.readiness, namedCheck{name, fn})
}

func (h *healthRegistry) setStarted()  { h.started.Store(true) }
func (h *healthRegistry) setDraining() { h.draining.Store(true) }

// livezHandler — process-only; 200 while not draining and all liveness checks pass.
func (h *healthRegistry) livezHandler(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "draining", "")
		return
	}
	h.mu.RLock()
	checks := append([]namedCheck(nil), h.liveness...)
	h.mu.RUnlock()
	if name, err := h.runChecks(r.Context(), checks); err != nil {
		writeUnhealthy(w, name, err)
		return
	}
	writeOK(w)
}

// readyzHandler — 200 only when started, not draining, all readiness checks pass.
func (h *healthRegistry) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !h.started.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "not started", "")
		return
	}
	if h.draining.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "draining", "")
		return
	}
	h.mu.RLock()
	checks := append([]namedCheck(nil), h.readiness...)
	h.mu.RUnlock()
	if name, err := h.runChecks(r.Context(), checks); err != nil {
		writeUnhealthy(w, name, err)
		return
	}
	writeOK(w)
}

// startupzHandler — 503 until SetStarted(), then 200.
func (h *healthRegistry) startupzHandler(w http.ResponseWriter, r *http.Request) {
	if !h.started.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "not started", "")
		return
	}
	writeOK(w)
}

func (h *healthRegistry) runChecks(ctx context.Context, checks []namedCheck) (string, error) {
	timeout := h.checkTimeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	for _, c := range checks {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		err := c.check(checkCtx)
		cancel()
		if err != nil {
			return c.name, err
		}
	}
	return "", nil
}

type healthBody struct {
	Status string `json:"status"`
	Check  string `json:"check,omitempty"`
	Error  string `json:"error,omitempty"`
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthBody{Status: "ok"})
}

func writeUnhealthy(w http.ResponseWriter, check string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(healthBody{Status: "unhealthy", Check: check, Error: err.Error()})
}

func writeStatus(w http.ResponseWriter, code int, status, check string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(healthBody{Status: status, Check: check})
}
