---
status: accepted
date: 2026-08-10
---

# Add signed keyset cursors while preserving offset compatibility

## Context

Item pages are ordered newest-first by the immutable tuple
`created_at DESC, id DESC`. Offset pagination is simple and remains useful for
small datasets, but inserts between requests can move rows across an offset
boundary and deep offsets become increasingly expensive in PostgreSQL. The
existing PostgreSQL index already matches the Item ordering, so a keyset
continuation can avoid both problems without a schema change.

The template has memory and PostgreSQL adapters behind one Store seam. A
cursor must therefore describe the same ordering position in both adapters,
must not let an HTTP adapter accidentally combine two independent backends,
and must be cheap to validate when it is supplied by an untrusted client.

## Decision

Keep the existing `offset` query form for backwards compatibility and add an
optional `cursor` query form. The two parameters are mutually exclusive. The
legacy first page may advertise a `meta.next_cursor`; a follow-up request sends
that opaque value as `cursor` and receives cursor metadata without an offset.

The Item module owns a `CursorStore.ListAfter` capability on the same concrete
Store as ordinary Item persistence. It receives a validated
`CursorPosition{CreatedAt, ID}` and applies the strict boundary
`(created_at, id) < (position.created_at, position.id)` under the existing
newest-first order. The Service asks for one look-ahead row, derives the next
position from the final returned Item, and encodes/decodes transport tokens
through an injected `CursorCodec`. A store without the cohesive capability
returns `cursor_unavailable`; it never silently falls back to offset reads.

The built-in codec uses a versioned `v1_` raw-Base64URL envelope containing
only the ordering tuple and an optional expiry, authenticated with
HMAC-SHA-256. It enforces a 512-byte token bound, constant-time MAC
verification, canonical encoding, and a 24-hour default TTL (zero may
explicitly disable expiry). The composition root supplies a stable 32-byte
hex-decoded `CURSOR_SIGNING_KEY`; all replicas serving the same cursors must
share it. The token is integrity-protected, not encrypted, and carries no
authorization claims. Authentication and resource authorization are evaluated
again on every request. The codec purpose includes the Item route and sort
version so a token cannot be reused for a different collection contract.

Cursor pages are not snapshots: rows inserted newer than the boundary are not
retroactively inserted into a continuation, and deletes can shorten a later
page. Immutable creation time and identity plus the UUID tie-breaker prevent
duplicates caused by equal timestamps. Keyset reads use the existing
`idx_items_created_at_id` index; no migration is required.

## Consequences

- Deep-page reads avoid `OFFSET` work and remain stable when newer Items are
  created between requests.
- Existing clients and stores can continue using `ListParams` and `List`; a
  cursor-capable client must use an adapter that implements the cohesive
  capability.
- A signing-key rotation invalidates outstanding cursors. Rotate deliberately
  and let clients restart at the first page; a future key-ring design would be
  a separate decision.
- Cursors expose no Item payload, but their timestamp/UUID boundary is
  base64-decodable. They must not be logged or treated as credentials.
- Keyset pagination does not provide a historical snapshot or solve future
  per-user filtering by itself. A filtered collection must bind its canonical
  filter and authorization scope into a new codec purpose before sharing a
  cursor contract.
