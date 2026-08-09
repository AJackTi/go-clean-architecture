# Go Clean Architecture

A small Go HTTP service template with PostgreSQL, explicit migrations, and a
container-first development workflow.

## Prerequisites

- Docker with the Compose v2 plugin (`docker compose`)
- Go 1.26.5 when running checks or binaries directly on the host
- golangci-lint v2.12.2 or newer for `make lint`

## Quick start

```sh
cp .env.example .env
make compose-up
curl http://127.0.0.1:8080/api/healthz
curl http://127.0.0.1:8080/api/v1/items
```

`compose-up` builds the non-root application image, waits for PostgreSQL,
applies pending migrations as a one-shot job, and then starts the API. Ports
are bound to localhost only. Follow logs with `make logs` and stop the stack
with `make compose-down`; the database volume is preserved.

## Development checks

```sh
make help
make test
make test-race
make check
```

Use `make migrate-version` to inspect migration state. Destructive rollback is
guarded and requires `make migrate-down CONFIRM=1`.
