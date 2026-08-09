# HTTP observability

The template keeps telemetry explicit and deployment-controlled.

## Metrics

Set `METRICS_ENABLED=true` to mount `GET /metrics` on the application HTTP
server. The endpoint uses an isolated Prometheus registry with request count,
duration, and in-flight gauges. Labels contain only the method, Gin route
pattern, and normalized status; unmatched requests use `unmatched`. Go/process
collectors are not enabled automatically.

Because the scrape endpoint shares the application listener, expose it only
through a private network, service mesh, or an authenticated reverse proxy.
It is disabled by default.

## Traces

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to an OTLP/HTTP collector. A complete HTTPS
URL is preferred; a host-only value receives `/v1/traces`. Plain HTTP requires
`OTEL_EXPORTER_OTLP_INSECURE=true` and is rejected when `APP_ENV=production`.
The endpoint must not contain credentials, query strings, or fragments.

`OTEL_TRACES_SAMPLER=parentbased_traceidratio` is the supported sampler, and
`OTEL_TRACES_SAMPLER_ARG` is a root sampling ratio from `0` to `1` (the local
default is `0.1`). An empty endpoint creates no exporter and performs no
collector I/O. W3C `traceparent` is extracted from inbound requests.

Server spans and access events use bounded route patterns and omit query
strings, bodies, headers, client addresses, and raw unmatched paths. The
provider flushes pending spans during a bounded graceful shutdown.

## Local checks

```sh
METRICS_ENABLED=true go run ./cmd/app
curl -sS http://127.0.0.1:8080/metrics
```

For a collector, copy `.env.example`, set the OTLP variables, and keep the
collector address on a trusted network. Do not place exporter credentials in
the repository or in logs.
