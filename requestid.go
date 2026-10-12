package goserver

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// TraceIDKey is the request-context key that carries the request id. It is a
// plain untyped string on purpose: go-logger looks it up with
// ctx.Value("trace_id"), so a typed key would hide the id from the logs.
const TraceIDKey = "trace_id"

// EchoRequestIDMiddleware echoes an incoming X-Request-ID (generating one when
// absent) on the response, mirrors it on the request header, and stores it in
// the request context under TraceIDKey so go-logger's *Ctx functions include
// it as trace_id. ServeEcho registers it with e.Pre; call it yourself only for
// an Echo instance served some other way.
func EchoRequestIDMiddleware() echo.MiddlewareFunc {
	return middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		RequestIDHandler: func(c echo.Context, id string) {
			req := c.Request()
			req.Header.Set(echo.HeaderXRequestID, id)
			c.SetRequest(req.WithContext(context.WithValue(req.Context(), TraceIDKey, id)))
		},
	})
}

// RequestIDTransport forwards the request id of the current request (TraceIDKey
// in the outbound request's context) as X-Request-ID on outbound calls, unless
// the caller already set the header. Nil Base means http.DefaultTransport.
//
//	client.Transport = &goserver.RequestIDTransport{Base: client.Transport}
type RequestIDTransport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *RequestIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Header.Get(echo.HeaderXRequestID) == "" {
		if id, _ := req.Context().Value(TraceIDKey).(string); id != "" {
			req = req.Clone(req.Context())
			req.Header.Set(echo.HeaderXRequestID, id)
		}
	}
	return base.RoundTrip(req)
}
