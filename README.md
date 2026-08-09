# Go Clean Architecture

[![CI](https://github.com/AJackTi/go-clean-architecture/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/AJackTi/go-clean-architecture/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/AJackTi/go-clean-architecture.svg)](https://pkg.go.dev/github.com/AJackTi/go-clean-architecture)
[![Go version](https://img.shields.io/badge/go-1.26.5-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A production-minded Go HTTP service template for teams that want a clear
architecture, a real PostgreSQL adapter, and a dependable delivery workflow
from the first commit.

This repository is intentionally small. The `Item` feature is a complete
vertical slice that demonstrates the seams a new feature should follow; it is
not a framework or a collection of speculative abstractions.

## What is included

- A domain module with validation, stable errors, deterministic pagination,
  and dependency-injected clock/identity seams.
- Memory and PostgreSQL persistence adapters behind one `item.Store`
  interface.
- A strict Gin HTTP adapter with bounded JSON bodies, stable envelopes, and
  explicit error mappings.
- Explicit, repeatable migrations through `cmd/migrate`.
- Liveness (`/api/health`) and dependency readiness (`/api/healthz`) probes.
- An explicit HTTP server lifecycle with bounded timeouts and graceful
  `SIGTERM` shutdown.
- A pinned, non-root, distroless container image and hardened Compose stack.
- CI quality gates for formatting, tests/race tests, PostgreSQL integration,
  cross-platform binaries, dependency vulnerabilities, and container scans.

## Architecture

The composition root wires concrete adapters once. Inner modules do not know
about Gin, PostgreSQL, or Docker; changes at an outer seam stay local.

```mermaid
flowchart LR
    Client[HTTP client] --> Router[HTTP adapter<br/>internal/controller/http]
    Router --> ItemHTTP[Item HTTP adapter<br/>internal/item/httpapi]
    ItemHTTP --> ItemService[Item domain module<br/>internal/item]
    ItemService --> Store{item.Store seam}
    Store --> Memory[Memory adapter<br/>internal/item/memory]
    Store --> Postgres[PostgreSQL adapter<br/>internal/item/postgres]
    Postgres --> DB[(PostgreSQL)]
    Composition[Composition root<br/>internal/app] -. wires .-> Router
    Composition -. wires .-> Postgres
    Migrate[cmd/migrate] --> DB
```

The design vocabulary and decisions are documented in
[`CONTEXT.md`](CONTEXT.md) and [`docs/adr/`](docs/adr/).

## Quick start with Docker Compose

### Prerequisites

- Docker Engine/Desktop with the Compose v2 plugin (`docker compose`)
- `make` and `curl` for the documented commands
- Go 1.26.5 for host-side development
- golangci-lint v2.12.2 for `make lint` (or set `GOLANGCI_LINT` to its path)

### Start the stack

```sh
cp .env.example .env
make compose-up

curl --fail http://127.0.0.1:8080/api/health
curl --fail http://127.0.0.1:8080/api/healthz
curl --fail http://127.0.0.1:8080/api/v1/items
```

`compose-up` builds one image, waits for PostgreSQL to pass both connection
and query health checks, applies pending migrations as a one-shot job, and
starts the non-root app only after the migration succeeds. Ports are bound to
localhost by default. Override `HTTP_PORT` or `POSTGRES_PORT` in `.env` when a
local service already uses the defaults.

```sh
make compose-logs
make compose-down                 # keeps the database volume
docker compose --env-file .env.example down --volumes  # disposable stack
```

### Run without Compose

Start a PostgreSQL instance, export a reachable `DATABASE_URL`, then run:

```sh
go run ./cmd/migrate up
go run ./cmd/app
```

The app reads `APP_ENV`, `HTTP_PORT`, `LOG_LEVEL`, and `DATABASE_URL` from the
environment. Development defaults are documented in [`.env.example`](.env.example);
production requires an explicit `DATABASE_URL`.

## Development workflow

```sh
make help
make fmt                  # format Go files
make test                 # tests with coverage
make test-race            # race detector
make lint                 # golangci-lint v2
make vuln                 # govulncheck
make check                # complete local quality gate
```

The CI workflow is the source of truth for the merge gate. It runs the same
checks with Go selected from the `toolchain` directive in `go.mod`, starts a
real PostgreSQL service for adapter tests, builds Linux `amd64`/`arm64`
binaries, and scans the container.

For a disposable PostgreSQL integration run, set `TEST_DATABASE_URL` and
apply the migration first:

```sh
export TEST_DATABASE_URL='postgres://app:local-dev-password@127.0.0.1:5432/app?sslmode=disable'
go test -race ./internal/item/postgres
```

## Customize the template

Run the bootstrap command from a clean checkout immediately after creating a
repository from this template. Preview the tracked text files it will update,
then repeat the command without `--dry-run`:

```sh
go run ./cmd/bootstrap \
  --module github.com/acme/orders-api \
  --slug orders-api \
  --owner acme \
  --author "Acme Engineering" \
  --email engineering@acme.example \
  --dry-run
```

`--module` and `--slug` are required. The GitHub owner is inferred from a
`github.com/<owner>/...` module when `--owner` is omitted. When the policy
documents contain a maintainer address, `--email` is also required so a fork
cannot accidentally retain the template maintainer's private contact. The
command refuses a dirty worktree by default; use `--force` only when the
existing changes are intentional and reviewed.

## HTTP contract

The running app exposes the following endpoints:

The machine-readable [OpenAPI 3.1 contract](docs/openapi.yaml) is the source
for client generation and endpoint examples. The contract is checked in CI
against the routes mounted by the HTTP adapter.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | Liveness; does not query PostgreSQL |
| `GET` | `/api/healthz` | Readiness; returns `503` when PostgreSQL is unavailable |
| `POST` | `/api/v1/items` | Create an item |
| `GET` | `/api/v1/items/{id}` | Fetch an item by UUID (created IDs are UUIDv4) |
| `GET` | `/api/v1/items?limit=20&offset=0` | Deterministic newest-first listing |

Create an item:

```sh
curl --fail --request POST http://127.0.0.1:8080/api/v1/items \
  --header 'Content-Type: application/json' \
  --data '{"name":"Mechanical keyboard","description":"Hot-swappable"}'
```

Successful responses use a `data` envelope. List responses also include
`meta.limit`, `meta.offset`, and `meta.has_more`. Names and descriptions are
trimmed and validated by Unicode rune count. Unknown JSON fields, malformed
JSON, trailing JSON values, and bodies larger than 1 MiB are rejected.

Errors use a stable shape and never expose provider details:

```json
{"error":{"code":"not_found","message":"item not found"}}
```

Typical status mappings are `400` for malformed transport input, `422` for
domain validation, `404` for a missing item, `409` for a conflict, and `500`
for an unavailable internal dependency.

Every response includes an `X-Request-ID` header. A valid single upstream
value is preserved; missing, malformed, oversized, or duplicate values are
replaced with a canonical UUIDv4. The HTTP adapter emits one structured access
event through the process logger with the request ID, method, route pattern,
status, response bytes, and duration. Query strings, request bodies, and raw
unmatched paths are deliberately excluded from logs.

## Repository map

| Path | Responsibility |
| --- | --- |
| [`cmd/app`](cmd/app) | Application entrypoint and signal lifecycle |
| [`cmd/bootstrap`](cmd/bootstrap) | Customize a clean template checkout |
| [`cmd/migrate`](cmd/migrate) | Explicit migration command (`up`, `down`, `step`, `version`) |
| [`cmd/healthcheck`](cmd/healthcheck) | Static readiness probe used by the image |
| [`internal/app`](internal/app) | Composition root and dependency wiring |
| [`internal/item`](internal/item) | Item domain module and use cases |
| [`internal/item/httpapi`](internal/item/httpapi) | Item HTTP adapter and contract tests |
| [`internal/item/memory`](internal/item/memory) | Deterministic race-safe memory adapter |
| [`internal/item/postgres`](internal/item/postgres) | PostgreSQL adapter and integration test |
| [`internal/controller/http`](internal/controller/http) | Router and operational endpoints |
| [`pkg/httpserver`](pkg/httpserver) | Reusable server lifecycle module |
| [`pkg/logger`](pkg/logger) | Process logging seam |
| [`db/migrations`](db/migrations) | Versioned PostgreSQL schema changes |
| [`scripts/template-smoke.sh`](scripts/template-smoke.sh) | Verify a clean generated repository end to end |
| [`.github`](.github) | CI, dependency updates, and contribution workflow |

## Adding a feature

Use the `Item` vertical slice as the template:

1. Define the domain model, invariants, stable errors, and use-case methods
   under `internal/<feature>`.
2. Keep persistence behind a domain-owned interface; add one adapter per
   technology under the feature directory.
3. Add the HTTP adapter and contract tests beside the feature.
4. Wire the concrete adapters only in `internal/app`.
5. Add an explicit migration when the schema changes.
6. Update `CONTEXT.md`, [`docs/openapi.yaml`](docs/openapi.yaml), an ADR when a
   durable decision changes, and this README when the public contract changes.

Prefer a deep interface with a small caller-facing surface. Apply the deletion
test before introducing another module or seam: if deleting it would not
concentrate complexity, it probably does not earn its place in the template.

## Database migrations

Migrations are immutable SQL files in [`db/migrations`](db/migrations), applied
by the same binary in development, CI, and the container image:

```sh
make migrate-version
make migrate-up
make migrate-down CONFIRM=1
make migrate-step STEP=1
```

`migrate-down` is intentionally guarded because it removes schema state.

## Container and supply-chain posture

The image uses a pinned Go builder and pinned distroless runtime, runs as
`nonroot`, includes CA certificates for TLS database connections, and contains
only the app, migration, healthcheck binaries, and migration SQL. Compose adds
read-only filesystems, a temporary filesystem, dropped Linux capabilities, and
`no-new-privileges`.

Actions and key container images are pinned by digest. Dependabot checks Go
modules, GitHub Actions, and Docker images weekly. CI runs `govulncheck` and a
Trivy HIGH/CRITICAL image scan.

## Releases

Push a semantic-version tag such as `v1.2.3` to trigger
`.github/workflows/release.yml`. GoReleaser publishes reproducible Linux and
macOS `amd64`/`arm64` archives, checksums, an SPDX SBOM, and GitHub provenance
attestations. Keep release notes and migration compatibility in mind before
tagging a version.

## Documentation and project policies

- [Architecture context](CONTEXT.md)
- [Architecture decisions](docs/adr/)
- [OpenAPI 3.1 contract](docs/openapi.yaml)
- [Downstream template smoke test](docs/template-smoke.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Support](SUPPORT.md)
- [Changelog](CHANGELOG.md)
- [Recommended GitHub repository settings](docs/repository-settings.md)

## License

Distributed under the [MIT License](LICENSE).
