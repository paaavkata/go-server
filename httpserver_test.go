package goserver

import (
	"io"
	"os"
	"strings"

	"context"
	logger "github.com/paaavkata/go-logger"
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

// A panicking handler served through ServeEcho gets a 500 and a structured
// error line with its request id, and the process keeps serving.
func TestServeEchoRecoversPanic(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	logger.Init("info", "plain", "go-server-test", "test", false, true, false, nil, nil)
	defer func() {
		os.Stdout = stdout
		logger.Init("info", "plain", "go-server-test", "test", false, true, false, nil, nil)
	}()

	mgr := New(Config{ServiceName: "svc"})
	e := echo.New()
	e.GET("/boom", func(c echo.Context) error { var m map[string]int; m["x"] = 1; return nil })
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

	var resp *http.Response
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/boom", nil)
		req.Header.Set(echo.HeaderXRequestID, "req-panic-1")
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "internal server error") {
		t.Fatalf("panic: status %d body %s", resp.StatusCode, body)
	}

	resp, err = http.Get("http://" + addr + "/ok")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("next request: status %d", resp.StatusCode)
	}

	w.Close()
	out, _ := io.ReadAll(r)
	var found bool
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "panic recovered") && strings.Contains(line, `"trace_id":"req-panic-1"`) && strings.Contains(line, "stack") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no structured panic line with trace_id in log output:\n%s", out)
	}
}
