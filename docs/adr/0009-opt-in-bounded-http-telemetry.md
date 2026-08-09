---
status: accepted
date: 2026-08-10
---

# Use opt-in bounded HTTP metrics and tracing

The HTTP composition module records Prometheus request count, duration, and
in-flight metrics in an isolated registry. Metrics are disabled by default; an
explicit `METRICS_ENABLED=true` mounts `/metrics`. Labels are limited to the
HTTP method, matched route pattern, and normalized status code/class. Raw paths,
queries, headers, bodies, and client addresses are never label values. Go and
process collectors are not registered automatically.

The same module creates OpenTelemetry server spans, extracts W3C trace context,
and attaches the method, matched route, response status, and bounded request ID.
Access events include the active trace and span IDs. The tracing provider uses a
parent-based trace-ID-ratio sampler and an optional OTLP/HTTP exporter. An empty
endpoint creates no exporter or collector traffic. Explicit HTTP endpoints
require an insecure opt-in outside production; production accepts HTTPS only.
Provider shutdown is bounded and flushes pending spans.

## Consequences

- A fresh checkout has no public scrape endpoint and sends no telemetry to an
  external system until a deployment explicitly opts in.
- Route-pattern labels and names keep time-series and span cardinality bounded,
  including 404 responses under one `unmatched` value.
- Operators can correlate client-visible request IDs, access events, and
  distributed traces without copying request contents into telemetry.
- The Prometheus registry intentionally excludes runtime/process collectors;
  downstream services can register reviewed collectors when their deployment
  and disclosure model permits them.
- OTLP credentials and collector-specific headers remain deployment concerns;
  this template does not read, log, or persist them.
