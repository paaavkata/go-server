package goserver

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all Manager configuration.
type Config struct {
	ServiceName        string
	AppPort            string        // APP_PORT env, default "8080"
	MetricsPort        string        // METRICS_PORT env, default "9090"
	ShutdownGrace      time.Duration // SHUTDOWN_GRACE_SECONDS env, default 22s
	ReadinessDrainWait time.Duration // READINESS_DRAIN_SECONDS env, default 5s
	CheckTimeout       time.Duration // CHECK_TIMEOUT_SECONDS env, default 2s
	HTTP               HTTPTimeouts  // business http.Server timeouts (ServeEcho)
}

// Shutdown budget: ReadinessDrainWait + ShutdownGrace must stay below the pod's
// terminationGracePeriodSeconds (chart default 30s) with margin, or late hooks
// are SIGKILLed. 5s + 22s = 27s leaves 3s. Raise both together.
const (
	DefaultShutdownGrace      = 22 * time.Second
	DefaultReadinessDrainWait = 5 * time.Second
	DefaultTerminationGrace   = 30 * time.Second
)

func (c *Config) applyDefaults() {
	if c.AppPort == "" {
		c.AppPort = "8080"
	}
	if c.MetricsPort == "" {
		c.MetricsPort = "9090"
	}
	if c.ShutdownGrace == 0 {
		c.ShutdownGrace = DefaultShutdownGrace
	}
	if c.ReadinessDrainWait == 0 {
		c.ReadinessDrainWait = DefaultReadinessDrainWait
	}
	if c.CheckTimeout == 0 {
		c.CheckTimeout = 2 * time.Second
	}
	// HTTP timeouts: -1 means "explicitly disabled" (0 after normalisation);
	// zero-value fields take the defaults.
	c.HTTP.ReadHeader = defaultDur(c.HTTP.ReadHeader, DefaultReadHeaderTimeout)
	c.HTTP.Read = defaultDur(c.HTTP.Read, DefaultReadTimeout)
	c.HTTP.Write = defaultDur(c.HTTP.Write, DefaultWriteTimeout)
	c.HTTP.Idle = defaultDur(c.HTTP.Idle, DefaultIdleTimeout)
}

func defaultDur(v, def time.Duration) time.Duration {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return def
	default:
		return v
	}
}

func configFromEnv(serviceName string) Config {
	return Config{
		ServiceName:        serviceName,
		AppPort:            getEnv("APP_PORT", "8080"),
		MetricsPort:        getEnv("METRICS_PORT", "9090"),
		ShutdownGrace:      getDurationSecs("SHUTDOWN_GRACE_SECONDS", 22),
		ReadinessDrainWait: getDurationSecs("READINESS_DRAIN_SECONDS", 5),
		CheckTimeout:       getDurationSecs("CHECK_TIMEOUT_SECONDS", 2),
		HTTP: HTTPTimeouts{
			ReadHeader: getTimeoutSecs("HTTP_READ_HEADER_TIMEOUT_SECONDS"),
			Read:       getTimeoutSecs("HTTP_READ_TIMEOUT_SECONDS"),
			Write:      getTimeoutSecs("HTTP_WRITE_TIMEOUT_SECONDS"),
			Idle:       getTimeoutSecs("HTTP_IDLE_TIMEOUT_SECONDS"),
		},
	}
}

// getTimeoutSecs reads an HTTP timeout env var: unset or invalid -> 0 (take the
// default), "0" -> -1 (explicitly disabled), n > 0 -> n seconds.
func getTimeoutSecs(key string) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	if n == 0 {
		return -1
	}
	return time.Duration(n) * time.Second
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDurationSecs(key string, defSecs int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return time.Duration(defSecs) * time.Second
}
