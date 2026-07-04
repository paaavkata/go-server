package goserver

import (
	"context"
	"testing"
	"time"
)

func TestNewDefaultPorts(t *testing.T) {
	mgr := New(Config{ServiceName: "svc"})
	if mgr.cfg.AppPort != "8080" {
		t.Errorf("default AppPort: want 8080, got %s", mgr.cfg.AppPort)
	}
	if mgr.cfg.MetricsPort != "9090" {
		t.Errorf("default MetricsPort: want 9090, got %s", mgr.cfg.MetricsPort)
	}
	if mgr.cfg.ShutdownGrace != 25*time.Second {
		t.Errorf("default ShutdownGrace: want 25s, got %s", mgr.cfg.ShutdownGrace)
	}
	if mgr.cfg.ReadinessDrainWait != 5*time.Second {
		t.Errorf("default ReadinessDrainWait: want 5s, got %s", mgr.cfg.ReadinessDrainWait)
	}
}

func TestSetStartedFlipsState(t *testing.T) {
	mgr := New(Config{ServiceName: "svc"})
	if mgr.health.started.Load() {
		t.Error("should not be started before SetStarted()")
	}
	mgr.SetStarted()
	if !mgr.health.started.Load() {
		t.Error("should be started after SetStarted()")
	}
}

func TestShutdownHooksReverseOrder(t *testing.T) {
	mgr := New(Config{
		ServiceName:        "svc",
		ShutdownGrace:      5 * time.Second,
		ReadinessDrainWait: 0,
	})

	var order []string
	for _, name := range []string{"a", "b", "c"} {
		name := name // capture
		mgr.OnShutdown(name, func(_ context.Context) error {
			order = append(order, name)
			return nil
		})
	}

	mgr.runHooks(context.Background())

	want := []string{"c", "b", "a"}
	if len(order) != len(want) {
		t.Fatalf("got %d hooks ran, want %d", len(order), len(want))
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("hook[%d]: want %q, got %q", i, w, order[i])
		}
	}
}

func TestShutdownHookErrorDoesNotAbort(t *testing.T) {
	mgr := New(Config{ServiceName: "svc", ShutdownGrace: 5 * time.Second})
	secondRan := false

	mgr.OnShutdown("first", func(_ context.Context) error {
		secondRan = true
		return nil
	})
	mgr.OnShutdown("second-fails", func(_ context.Context) error {
		return context.DeadlineExceeded // simulate error
	})

	mgr.runHooks(context.Background())

	// hooks run in reverse: second-fails first, then first
	if !secondRan {
		t.Error("hook after a failing hook should still run")
	}
}

func TestDrainSetsNotReady(t *testing.T) {
	mgr := New(Config{
		ServiceName:        "svc",
		ShutdownGrace:      100 * time.Millisecond,
		ReadinessDrainWait: 0,
	})
	mgr.SetStarted()

	if mgr.health.draining.Load() {
		t.Error("should not be draining before drain()")
	}

	mgr.drain(nil)

	if !mgr.health.draining.Load() {
		t.Error("should be draining after drain()")
	}
}

func TestRegistryIsNotNil(t *testing.T) {
	mgr := New(Config{ServiceName: "svc"})
	if mgr.Registry() == nil {
		t.Error("Registry() must not return nil")
	}
}
