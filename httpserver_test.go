package goserver

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestShutdownBudgetFitsTerminationGrace(t *testing.T) {
	mgr := New(Config{ServiceName: "svc"})
	total := mgr.cfg.ReadinessDrainWait + mgr.cfg.ShutdownGrace
	if total > DefaultTerminationGrace-3*time.Second {
		t.Fatalf("drain %s + grace %s = %s must leave >= 3s inside %s",
			mgr.cfg.ReadinessDrainWait, mgr.cfg.ShutdownGrace, total, DefaultTerminationGrace)
	}
}

func TestHTTPTimeoutDefaultsAndOverrides(t *testing.T) {
	mgr := New(Config{ServiceName: "svc"})
	if mgr.cfg.HTTP.ReadHeader != DefaultReadHeaderTimeout || mgr.cfg.HTTP.Write != DefaultWriteTimeout {
		t.Fatalf("defaults not applied: %+v", mgr.cfg.HTTP)
	}
	t.Setenv("HTTP_WRITE_TIMEOUT_SECONDS", "0")
	t.Setenv("HTTP_READ_TIMEOUT_SECONDS", "7")
	t.Setenv("HTTP_IDLE_TIMEOUT_SECONDS", "junk")
	cfg := configFromEnv("svc")
	cfg.applyDefaults()
	if cfg.HTTP.Write != 0 {
		t.Fatalf("HTTP_WRITE_TIMEOUT_SECONDS=0 must disable, got %s", cfg.HTTP.Write)
	}
	if cfg.HTTP.Read != 7*time.Second {
		t.Fatalf("HTTP_READ_TIMEOUT_SECONDS=7 ignored, got %s", cfg.HTTP.Read)
	}
	if cfg.HTTP.Idle != DefaultIdleTimeout {
		t.Fatalf("invalid env must fall back to default, got %s", cfg.HTTP.Idle)
	}
}

// A client that opens a connection and never sends headers is dropped after
// ReadHeaderTimeout, so slowloris-style connections cannot pin workers.
func TestServeEchoReadHeaderTimeoutClosesSlowClient(t *testing.T) {
	mgr := New(Config{ServiceName: "svc", HTTP: HTTPTimeouts{ReadHeader: 300 * time.Millisecond}})
	e := echo.New()
	e.GET("/ok", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	serve, shutdown := mgr.ServeEcho(e, addr)
	go func() { _ = serve() }()
	defer shutdown(context.Background())

	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send nothing. The server must close the connection on its own.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	start := time.Now()
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected the server to close the idle header-less connection")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("connection closed after %s, expected ~300ms", elapsed)
	}

	// A normal request still works.
	resp, err := http.Get("http://" + addr + "/ok")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
