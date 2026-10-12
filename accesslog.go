package goserver

import (
	"time"

	"github.com/labstack/echo/v4"
	logger "github.com/paaavkata/go-logger"
)

// EchoAccessLogMiddleware writes one structured line per request through
// go-logger with the request context, so every entry carries trace_id (see
// EchoRequestIDMiddleware). ServeEcho registers it after the request id; the
// health endpoints live on the metrics server and never hit it.
func EchoAccessLogMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			if err != nil {
				c.Error(err) // let Echo's handler set the status before we read it
			}
			req, res := c.Request(), c.Response()
			logger.InfofMap(req.Context(), map[string]interface{}{
				"message":    "access",
				"method":     req.Method,
				"path":       req.URL.Path,
				"status":     res.Status,
				"bytes":      res.Size,
				"latency_ms": time.Since(start).Milliseconds(),
				"remote_ip":  c.RealIP(),
				"user_agent": req.UserAgent(),
			})
			return nil
		}
	}
}
