# go-server

Shared server-lifecycle library for the platform's Go microservices. Standardizes the
production-readiness contract from `K8S_HARDENING_PLAN`:

- **Two ports**: business API on `APP_PORT` (default `8080`); observability on `METRICS_PORT`
  (default `9090`) — the latter is **never** exposed via public ingress.
- **Probes** on the observability port: `/livez` (process-only), `/readyz` (dependency checks,
  503 while draining), `/startupz` (200 once init done).
- **Metrics** at `/metrics`: Go runtime + process collectors + HTTP middleware
  (`http_requests_total`, `http_request_duration_seconds`, `http_requests_in_flight` by
  `{method, route, code}`).
- **Graceful shutdown**: SIGTERM → readiness 503 → wait for endpoint removal → drain in-flight
  HTTP → run shutdown hooks (LIFO) → close observability server, bounded by `ShutdownGrace`.

## Config (env)

| Env | Default | Meaning |
|---|---|---|
| `APP_PORT` | `8080` | Business API port |
| `METRICS_PORT` | `9090` | Observability port (livez/readyz/startupz/metrics) |
| `SHUTDOWN_GRACE_SECONDS` | `25` | Hard bound on the whole shutdown sequence |
| `READINESS_DRAIN_SECONDS` | `5` | Wait after flipping 503 so endpoint removal propagates |
| `CHECK_TIMEOUT_SECONDS` | `2` | Per-readiness/liveness-check timeout |

Keep `SHUTDOWN_GRACE_SECONDS` < the chart's `terminationGracePeriodSeconds`.

## HTTP service (Echo)

```go
mgr := goserver.FromViper("identity-service")
mgr.StartObservability()

mgr.AddReadinessCheck("postgres", func(ctx context.Context) error { return db.PingContext(ctx) })
mgr.AddReadinessCheck("nats", func(ctx context.Context) error { return producer.Ping(ctx) })

e := echo.New()
e.Use(mgr.EchoMetricsMiddleware())
// ... register routes ...

mgr.SetStarted() // after migrations / warm-up

mgr.Run(
    func() error { return e.Start(":" + os.Getenv("APP_PORT")) },
    func(ctx context.Context) error { return e.Shutdown(ctx) },
)
```

## HTTP + NATS consumer

Register the consumer-stop and resource closes as shutdown hooks (they run LIFO):

```go
mgr.OnShutdown("postgres", func(ctx context.Context) error { return db.Close() })
mgr.OnShutdown("nats-consumer", func(ctx context.Context) error { consumer.Stop(); return nil })
// nats-consumer stops first, postgres closes last.
```

## Pure worker (no business HTTP server)

```go
mgr := goserver.FromViper("k8s-scheduler")
mgr.StartObservability()
ctx, cancel := context.WithCancel(context.Background())
mgr.OnShutdown("consumer", func(context.Context) error { cancel(); return nil })
mgr.SetStarted()
go consumer.Run(ctx)
mgr.RunWorker(ctx)
```

## Custom metrics

```go
myCounter := prometheus.NewCounter(prometheus.CounterOpts{Name: "jobs_processed_total"})
mgr.Registry().MustRegister(myCounter)
```

## Chart wiring

Probes target the observability port; `service-deployment-helm-chart` (v1.1.0+) renders the
`metrics` port, three probes, and a `ServiceMonitor` when its hardening flags are enabled.
