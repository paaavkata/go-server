package goserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLivezAlwaysOK(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	h.livezHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

func TestLivezDraining(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setDraining()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()
	h.livezHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 while draining, got %d", rec.Code)
	}
}

func TestReadyzNotStarted(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.readyzHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 before started, got %d", rec.Code)
	}
}

func TestReadyzStartedNoChecks(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setStarted()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.readyzHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200 after started with no checks, got %d", rec.Code)
	}
}

func TestReadyzDraining(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setStarted()
	h.setDraining()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.readyzHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 while draining, got %d", rec.Code)
	}
}

func TestReadyzCheckFailure503WithBody(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setStarted()
	h.addReadiness("postgres", func(ctx context.Context) error {
		return errors.New("connection refused")
	})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.readyzHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 on check failure, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "postgres") {
		t.Errorf("response body should name the failed check 'postgres', got: %s", body)
	}
	if !strings.Contains(body, "unhealthy") {
		t.Errorf("response body should contain 'unhealthy', got: %s", body)
	}
}

func TestReadyzCheckPass(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setStarted()
	h.addReadiness("db", func(ctx context.Context) error { return nil })
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.readyzHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200 when all checks pass, got %d", rec.Code)
	}
}

func TestStartupzBeforeSetStarted(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	rec := httptest.NewRecorder()
	h.startupzHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 before SetStarted, got %d", rec.Code)
	}
}

func TestStartupzAfterSetStarted(t *testing.T) {
	h := newHealthRegistry(2 * time.Second)
	h.setStarted()
	req := httptest.NewRequest(http.MethodGet, "/startupz", nil)
	rec := httptest.NewRecorder()
	h.startupzHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200 after SetStarted, got %d", rec.Code)
	}
}

func TestCheckTimeoutEnforced(t *testing.T) {
	h := newHealthRegistry(10 * time.Millisecond) // very short timeout
	h.setStarted()
	h.addReadiness("slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			return nil
		}
	})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	h.readyzHandler(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 on timed-out check, got %d", rec.Code)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("check timeout not enforced: took %s", elapsed)
	}
}
