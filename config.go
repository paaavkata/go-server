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
	ShutdownGrace      time.Duration // SHUTDOWN_GRACE_SECONDS env, default 25s
	ReadinessDrainWait time.Duration // READINESS_DRAIN_SECONDS env, default 5s
	CheckTimeout       time.Duration // CHECK_TIMEOUT_SECONDS env, default 2s
}

func (c *Config) applyDefaults() {
	if c.AppPort == "" {
		c.AppPort = "8080"
	}
	if c.MetricsPort == "" {
		c.MetricsPort = "9090"
	}
	if c.ShutdownGrace == 0 {
		c.ShutdownGrace = 25 * time.Second
	}
	if c.ReadinessDrainWait == 0 {
		c.ReadinessDrainWait = 5 * time.Second
	}
	if c.CheckTimeout == 0 {
		c.CheckTimeout = 2 * time.Second
	}
}

func configFromEnv(serviceName string) Config {
	return Config{
		ServiceName:        serviceName,
		AppPort:            getEnv("APP_PORT", "8080"),
		MetricsPort:        getEnv("METRICS_PORT", "9090"),
		ShutdownGrace:      getDurationSecs("SHUTDOWN_GRACE_SECONDS", 25),
		ReadinessDrainWait: getDurationSecs("READINESS_DRAIN_SECONDS", 5),
		CheckTimeout:       getDurationSecs("CHECK_TIMEOUT_SECONDS", 2),
	}
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
