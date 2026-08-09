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

## Adding a decision

Use the next four-digit sequence, a short kebab-case filename, and a concise
statement of context, decision, and rationale. Add consequences only when
they constrain future work. Do not rewrite accepted history: add a new ADR and
mark the old record `Superseded by ADR-NNNN`.
