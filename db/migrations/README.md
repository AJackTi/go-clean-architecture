# Database migrations

Migration files are immutable, numbered SQL pairs consumed by `cmd/migrate`.
Run them explicitly during deployment; application startup never changes the
schema:

```sh
go run ./cmd/migrate up
go run ./cmd/migrate version
```

`0000001_create_items` creates the example Item table. Migration
`0000002_create_item_idempotency_keys` adds the replay record used by keyed
`POST /api/v1/items` requests:

- `key_hash` is a 32-byte SHA-256 digest of a route- and caller-scoped key.
- `request_hash` is a 32-byte SHA-256 digest of canonical create fields.
- `item_id` references the committed Item without cascading deletion, so a
  future delete policy cannot silently make a retained key reusable.
- `created_at` and `expires_at` define the fixed 24-hour replay window.
- An index on `expires_at` supports bounded cleanup of expired records.

The PostgreSQL adapter acquires a transaction-scoped advisory lock for the
hashed key, inserts the Item and replay row in one transaction, and commits
them together. A cancelled or failed transaction leaves neither row and
releases the lock automatically. Expired rows are cleaned opportunistically in
batches during idempotent operations; deployments with sustained high unique
key volume should also schedule an indexed expiry delete/vacuum so storage
does not depend on future keyed traffic.

Apply all migrations before enabling clients to send `Idempotency-Key`. The
down migration for `0000002` removes replay metadata but deliberately does not
remove Items; review the impact and use the guarded `migrate-down` workflow in
an approved maintenance window.
