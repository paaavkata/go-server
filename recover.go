package goserver

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"
	logger "github.com/paaavkata/go-logger"
)

// EchoRecoverMiddleware turns a handler panic into a structured error log line
// (with trace_id from the request context and the stack) and a 500 JSON
// envelope, so one bad request cannot take the process down. ServeEcho
// registers it after the access log, so the access line records the 500.
// A service that also registers echo's own Recover keeps working: the inner
// one handles the panic first.
func EchoRecoverMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if r == http.ErrAbortHandler {
					panic(r) // deliberate connection abort, net/http handles it
				}
				req := c.Request()
				logger.ErrorfMap(req.Context(), map[string]interface{}{
					"message": "panic recovered",
					"panic":   fmt.Sprint(r),
					"method":  req.Method,
					"path":    req.URL.Path,
					"stack":   string(debug.Stack()),
				})
				if !c.Response().Committed {
					err = c.JSON(http.StatusInternalServerError, map[string]string{
						"status":  "error",
						"message": "internal server error",
					})
				}
			}()
			return next(c)
		}
	}
}
