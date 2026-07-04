package goserver

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metricsRegistry struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func newMetricsRegistry(_ string) *metricsRegistry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests partitioned by method, route, and status code.",
	}, []string{"method", "route", "code"})

	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds partitioned by method, route, and status code.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "code"})

	inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Current number of HTTP requests being served.",
	})

	reg.MustRegister(requests, duration, inFlight)
	return &metricsRegistry{registry: reg, requests: requests, duration: duration, inFlight: inFlight}
}

func (m *metricsRegistry) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: false})
}

func (m *metricsRegistry) echoMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			m.inFlight.Inc()
			start := time.Now()

			err := next(c)

			status := c.Response().Status
			if err != nil {
				var he *echo.HTTPError
				if errors.As(err, &he) {
					status = he.Code
				} else {
					status = http.StatusInternalServerError
				}
			}
			if status == 0 {
				status = http.StatusOK
			}

			route := c.Path()
			if route == "" {
				route = "unknown"
			}

			m.requests.WithLabelValues(c.Request().Method, route, strconv.Itoa(status)).Inc()
			m.duration.WithLabelValues(c.Request().Method, route, strconv.Itoa(status)).Observe(time.Since(start).Seconds())
			m.inFlight.Dec()

			return err
		}
	}
}
