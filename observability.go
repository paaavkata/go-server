package goserver

import (
	"errors"
	"net/http"
	"time"

	logger "github.com/paaavkata/go-logger"
)

// startObservabilityServer starts the observability HTTP server on MetricsPort
// serving /livez, /readyz, /startupz, and /metrics. Called once via sync.Once.
func (m *Manager) startObservabilityServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", m.health.livezHandler)
	mux.HandleFunc("/readyz", m.health.readyzHandler)
	mux.HandleFunc("/startupz", m.health.startupzHandler)
	mux.Handle("/metrics", m.metrics.handler())

	srv := &http.Server{
		Addr:         ":" + m.cfg.MetricsPort,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Infof("[%s] observability server on :%s (/livez /readyz /startupz /metrics)",
			m.cfg.ServiceName, m.cfg.MetricsPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf("[%s] observability server: %v", m.cfg.ServiceName, err)
		}
	}()

	return srv
}
