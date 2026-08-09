---
status: accepted
date: 2026-08-09
---

# Apply migrations as an explicit lifecycle step

Database schema changes are applied by the standalone `cmd/migrate` command,
not implicitly by the HTTP process. In Compose, PostgreSQL must pass a real
readiness check, the one-shot migration job must complete successfully, and
only then may the app start; the app performs a bounded ping but never creates
or alters tables.

## Consequences

- A deployment can observe, retry, or roll back migration failure separately
  from application startup, and the migration version is inspectable with
  `make migrate-version`.
- Local and CI workflows have one migration path (`db/migrations`), reducing
  drift between environments.
- Operators must include the migration job in every deployment and must plan
  backwards-compatible schema changes when old and new binaries overlap.
- Rollback is deliberately explicit and guarded (`CONFIRM=1`) because down
  migrations can destroy data.
