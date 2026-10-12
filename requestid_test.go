package goserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func newRequestIDEcho(t *testing.T, seen *string) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.Pre(EchoRequestIDMiddleware())
	e.GET("/x", func(c echo.Context) error {
		id, _ := c.Request().Context().Value("trace_id").(string) // the exact lookup go-logger does
		*seen = id
		return c.NoContent(http.StatusOK)
	})
	return e
}

func TestEchoRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	var seen string
	e := newRequestIDEcho(t, &seen)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	got := rec.Header().Get(echo.HeaderXRequestID)
	if got == "" {
		t.Fatal("response X-Request-ID must be generated when absent")
	}
	if seen != got {
		t.Fatalf("handler ctx trace_id %q != response id %q", seen, got)
	}
}

func TestEchoRequestIDMiddleware_EchoesIncoming(t *testing.T) {
	var seen string
	e := newRequestIDEcho(t, &seen)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(echo.HeaderXRequestID, "abc")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get(echo.HeaderXRequestID); got != "abc" {
		t.Fatalf("response X-Request-ID = %q, want abc", got)
	}
	if seen != "abc" {
		t.Fatalf("handler ctx trace_id = %q, want abc", seen)
	}
}

type captureRT struct{ header string }

func (c *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.header = req.Header.Get(echo.HeaderXRequestID)
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestRequestIDTransport_CopiesFromContext(t *testing.T) {
	rt := &captureRT{}
	client := &http.Client{Transport: &RequestIDTransport{Base: rt}}
	ctx := context.WithValue(context.Background(), TraceIDKey, "abc")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example/", nil)
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	if rt.header != "abc" {
		t.Fatalf("outbound X-Request-ID = %q, want abc", rt.header)
	}
	if req.Header.Get(echo.HeaderXRequestID) != "" {
		t.Fatal("transport must clone the request, not mutate the caller's headers")
	}
}

func TestRequestIDTransport_KeepsExplicitHeader(t *testing.T) {
	rt := &captureRT{}
	client := &http.Client{Transport: &RequestIDTransport{Base: rt}}
	ctx := context.WithValue(context.Background(), TraceIDKey, "from-ctx")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example/", nil)
	req.Header.Set(echo.HeaderXRequestID, "explicit")
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	if rt.header != "explicit" {
		t.Fatalf("outbound X-Request-ID = %q, want explicit", rt.header)
	}
}
