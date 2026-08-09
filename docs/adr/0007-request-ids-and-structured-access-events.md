---
status: accepted
date: 2026-08-10
---

# Assign request IDs and emit structured access events

The HTTP composition module assigns one bounded `X-Request-ID` to every
response. It preserves exactly one valid upstream HTTP token so a caller can
correlate a request across hops; missing, malformed, oversized, or duplicate
values are replaced with a canonical UUIDv4. After the handler chain returns,
the module emits one structured access event through the process logger.

The event contains the request ID, method, matched route pattern, status,
response bytes, and duration. It intentionally excludes query strings,
request bodies, headers, client addresses, and raw unmatched paths so logs do
not become an accidental secret or personal-data sink. Events below 400 are
informational, 4xx events are warnings, and 5xx events are errors.

## Consequences

- Operators can correlate a client-visible response with one log event without
  learning a new logging interface.
- Route patterns keep cardinality bounded and avoid logging user-controlled
  path values; the unmatched route uses a fixed label.
- The middleware remains transport-owned and has no persistence dependency.
- Metrics and distributed tracing remain separate decisions because they need a
  scrape/exporter contract, cardinality policy, and deployment-specific
  credentials that this small template cannot safely assume.
