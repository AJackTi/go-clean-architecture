---
status: accepted
date: 2026-08-09
---

# Generate Item identity and creation time in the Item module

The Item module, rather than an HTTP client or database default, assigns an
RFC 4122 UUIDv4 and a UTC `CreatedAt` value during creation. `CreateInput`
contains only client-controlled fields; the ID generator and clock are small
injection seams for deterministic tests, while production uses random UUIDs
and the wall clock.

## Consequences

- Every adapter receives a complete, canonical Item and can focus on
  persistence rather than duplicating business policy.
- Identity and ordering are consistent across memory and PostgreSQL adapters,
  and database round trips are normalized to UTC.
- Retries without an `Idempotency-Key` are not idempotent and can produce a new
  UUID. The optional policy and atomic persistence seam are defined in
  [ADR-0011](0011-atomic-scoped-idempotent-creates.md).
- Future update/delete use cases must state whether `CreatedAt` remains
  immutable and how changes affect page ordering.
