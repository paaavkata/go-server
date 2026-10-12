package goserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestEchoAccessLogMiddleware_PassesThroughAndMapsErrors(t *testing.T) {
	e := echo.New()
	e.Pre(EchoRequestIDMiddleware())
	e.Use(EchoAccessLogMiddleware())
	e.GET("/ok", func(c echo.Context) error { return c.String(http.StatusCreated, "x") })
	e.GET("/err", func(c echo.Context) error { return echo.NewHTTPError(http.StatusTeapot, "no") })
	e.GET("/boom", func(c echo.Context) error { return errors.New("boom") })
	for path, want := range map[string]int{"/ok": 201, "/err": 418, "/boom": 500} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s: status %d, want %d", path, rec.Code, want)
		}
		if rec.Header().Get(echo.HeaderXRequestID) == "" {
			t.Fatalf("%s: missing X-Request-ID", path)
		}
	}
}
