---
status: accepted
date: 2026-08-10
---

# Make keyed creates atomic, scoped, and opt-in

## Context

Clients may retry a `POST /api/v1/items` after a timeout without knowing
whether the server committed the Item. A naïve multi-step reservation API
(`begin`, create, `complete`) permits a lease to expire while the first request
is still doing work; a second request can then create a duplicate Item. Keeping
raw client keys or request bodies in memory/logs also creates avoidable privacy
and unbounded-retention risks.

The template has both an in-memory adapter and a PostgreSQL adapter, so the
policy must remain useful in tests while preserving one correctness boundary in
production. Authentication is optional, and a key must not let one caller
replay another caller's response.

## Decision

Expose one cohesive `CreateIdempotent` store operation that receives the
server-generated Item candidate, an opaque scoped key, and a canonical request
fingerprint. The Item write and replay record are committed as one atomic unit.
The Item service advertises this capability only when its own `Store`
implements the atomic seam; it never composes separate Item and idempotency
stores.

The HTTP adapter accepts an optional `Idempotency-Key` header containing exactly
one ASCII HTTP token of 1–255 bytes. It fingerprints the canonical (trimmed)
create fields. The router scopes the key to the authenticated principal when
authentication is enabled, or to the direct peer address otherwise; forwarded
headers are not trusted. Adapters hash the scoped key and fingerprint before
storage and never log or include them in error bodies.

The first successful keyed create returns `201 Created`. A matching retry
within the 24-hour post-completion retention window returns `200 OK` with the
same Item body and `Location`, plus `Idempotency-Replayed: true`. A mismatched
fingerprint returns `409 idempotency_conflict`. A concurrent request that
cannot acquire the same-key operation returns `409 idempotency_in_progress`
with `Retry-After: 1`. A requested key without a cohesive atomic backend
returns `503 idempotency_unavailable`; an unkeyed create keeps the ordinary
create path.

The memory adapter uses hashed keys, a bounded resident-entry cap, TTL cleanup,
and a per-key in-flight marker. The PostgreSQL adapter stores fixed-size hashes
in `item_idempotency_keys`, uses a transaction-scoped advisory lock, cleans
expired rows in bounded batches, and inserts the Item plus replay row in the
same transaction. A rolled-back transaction releases the lock and leaves no
replay record.

## Consequences

- Retries are safe across application replicas that share PostgreSQL, without
  a lease-takeover window that can duplicate side effects.
- The replay window and bounded cleanup are explicit operational policy rather
  than hidden process state; deploy migration `0000002` before enabling keyed
  creates.
- A direct-peer scope is intentionally conservative. Deployments behind a
  trusted proxy should replace the scope policy with an authenticated identity
  or an explicitly configured proxy boundary; they must not blindly trust
  `X-Forwarded-For`.
- Expired records allow a key to be reused, so clients that need a longer
  business deduplication window must choose a durable domain-level operation
  identifier instead of relying solely on this transport feature.
- The replay row uses an explicit `ON DELETE RESTRICT` reference to the Item;
  any future delete use case must choose and document whether retained replay
  history is preserved, snapshotted, or deliberately removed.
- The API has distinct stable error codes for payload conflicts, in-flight
  work, and unavailable capability, allowing clients to choose retry behavior
  without parsing provider errors.
