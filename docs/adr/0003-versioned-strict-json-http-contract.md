---
status: accepted
date: 2026-08-09
---

# Expose a versioned, strict JSON HTTP contract

The HTTP adapter exposes health endpoints at `/api/health` (liveness) and
`/api/healthz` (readiness), and Item routes under `/api/v1/items`. Successful
single-resource responses use `{"data": ...}`, list responses use
`{"data": [...], "meta": {"limit", "offset", "has_more"}}`, and failures use
`{"error": {"code", "message"}}` with stable status mappings. Create requests
are strict JSON (unknown fields and trailing values are rejected), are bounded
to 1 MiB by default, and return `201 Created` with a `Location` header.

## Consequences

- Clients can generate predictable integrations from one documented envelope
  and can distinguish validation (422), not found (404), conflict (409), and
  internal failures (500) without parsing provider error text.
- Breaking changes require a new route version; additive fields should remain
  backwards-compatible and be covered by contract tests.
- Strict decoding catches typos early but intentionally rejects clients that
  send unknown fields or multiple JSON documents.
- Offset pagination is simple and deterministic, with a default of 20, a
  maximum of 100, and one-row look-ahead for `has_more`; it is not a snapshot
  or cursor guarantee.
