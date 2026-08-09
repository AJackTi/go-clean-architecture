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

Unexpected handler panics use a sanitized recovery callback that writes the
stable internal-error JSON envelope. Recovery diagnostics are discarded rather
than emitted with Gin's default request dump, which can contain query strings
and sensitive headers. Automatic trailing-slash and fixed-path redirects are
disabled so route-shape errors also pass through the middleware and receive a
request ID and access event.

## Consequences

- Operators can correlate a client-visible response with one log event without
  learning a new logging interface.
- Route patterns keep cardinality bounded and avoid logging user-controlled
  path values; the unmatched route uses a fixed label.
- Panic responses do not disclose panic values or request metadata, and
  malformed route spellings produce a regular 404 instead of bypassing
  observability through an automatic redirect.
- The middleware remains transport-owned and has no persistence dependency.
- Metrics and distributed tracing follow the separate, opt-in policy in
  [ADR-0009](0009-opt-in-bounded-http-telemetry.md); this decision remains the
  privacy boundary shared by all HTTP telemetry.
