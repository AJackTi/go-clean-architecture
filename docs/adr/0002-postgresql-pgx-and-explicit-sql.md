---
status: accepted
date: 2026-08-09
---

# Use PostgreSQL with pgx and explicit SQL migrations

PostgreSQL is the durable store for the template, accessed through pgx and
feature-owned, parameterized SQL in `internal/item/postgres`. Schema changes
are explicit, reviewable up/down files under `db/migrations` and are applied by
`cmd/migrate`; the application does not use an ORM or mutate the schema at
startup.

## Considered options

- **ORM or query builder:** rejected for this template because it hides query
  shape and schema ownership, making the persistence adapter shallower and
  harder to inspect during review.
- **A database-agnostic repository package:** rejected until a second durable
  database is a real requirement; the `item.Store` seam is sufficient for the
  current memory and PostgreSQL adapters.

## Consequences

- SQL, indexes, constraints, and scan logic stay close to the PostgreSQL
  adapter and migration that explain them.
- PostgreSQL integration tests are required for query and migration behaviour;
  memory tests do not substitute for them.
- Replacing PostgreSQL requires a new adapter and schema/migration strategy,
  while the Item module can remain unchanged.
- Migration ordering and rollback safety become part of release review; a
  migration must be additive/compatible with the running version unless the
  deployment plan explicitly coordinates a breaking change.
