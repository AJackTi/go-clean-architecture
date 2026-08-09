# Architecture decision records

Architecture decision records (ADRs) preserve decisions that are costly or
surprising to reverse. They explain the trade-off so a later refactor can
distinguish intentional constraints from accidental code shape.

## Index

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](0001-item-vertical-slice-and-dependency-direction.md) | Keep the Item feature as a vertical slice | Accepted |
| [0002](0002-postgresql-pgx-and-explicit-sql.md) | Use PostgreSQL with pgx and explicit SQL migrations | Accepted |
| [0003](0003-versioned-strict-json-http-contract.md) | Expose a versioned, strict JSON HTTP contract | Accepted |
| [0004](0004-server-owned-item-identity-and-time.md) | Generate Item identity and creation time in the Item module | Accepted |
| [0005](0005-explicit-migration-lifecycle.md) | Apply migrations as an explicit lifecycle step | Accepted |
| [0006](0006-environment-first-config-and-explicit-http-lifecycle.md) | Use environment-first configuration and an explicit HTTP lifecycle | Accepted |
| [0007](0007-request-ids-and-structured-access-events.md) | Assign request IDs and emit structured access events | Accepted |
| [0008](0008-openapi-contract-and-route-drift-check.md) | Keep a checked-in OpenAPI contract beside the HTTP adapter | Accepted |
| [0009](0009-opt-in-bounded-http-telemetry.md) | Use opt-in bounded HTTP metrics and tracing | Accepted |
| [0010](0010-opt-in-authentication-and-rate-limiting.md) | Keep starter authentication and rate limiting opt-in | Accepted |
| [0011](0011-atomic-scoped-idempotent-creates.md) | Make keyed creates atomic, scoped, and opt-in | Accepted |
| [0012](0012-signed-keyset-cursor-pagination.md) | Add signed keyset cursors while preserving offset compatibility | Accepted |

## Adding a decision

Use the next four-digit sequence, a short kebab-case filename, and a concise
statement of context, decision, and rationale. Add consequences only when
they constrain future work. Do not rewrite accepted history: add a new ADR and
mark the old record `Superseded by ADR-NNNN`.
