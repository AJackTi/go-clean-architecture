# Go Clean Architecture

[![CI](https://github.com/AJackTi/go-clean-architecture/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/AJackTi/go-clean-architecture/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/AJackTi/go-clean-architecture.svg)](https://pkg.go.dev/github.com/AJackTi/go-clean-architecture)
[![Go version](https://img.shields.io/badge/go-1.26.6-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
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
- Opt-in idempotent item creation with canonical request fingerprints, scoped
  keys, replay-safe responses, and atomic memory/PostgreSQL persistence.
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
- Go 1.26.6 for host-side development
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

Runtime configuration is environment-first and validated before the app starts:

| Variable | Development default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Runtime environment; `production` enables production safeguards. |
| `HTTP_PORT` | `8080` | HTTP listen port (`1`–`65535`). |
| `LOG_LEVEL` | `info` | Structured log verbosity. |
| `DATABASE_URL` | Local PostgreSQL URL | Database connection; it must be set explicitly in production. |
| `METRICS_ENABLED` | `false` | Exposes Prometheus metrics at `/metrics` when enabled. |
| `OTEL_SERVICE_NAME` | `github.com/AJackTi/go-clean-architecture` | OpenTelemetry `service.name`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Empty | OTLP/HTTP collector URL; an empty value disables trace export. |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Allows plaintext OTLP/HTTP outside production only. |
| `OTEL_TRACES_SAMPLER` | `parentbased_traceidratio` | Supported parent-based trace sampler. |
| `OTEL_TRACES_SAMPLER_ARG` | `0.1` | Root-span sampling ratio from `0` to `1`. |
| `AUTH_ENABLED` | `false` | Protects `/api/v1` with an optional Bearer credential. |
| `AUTH_BEARER_TOKEN_SHA256` | Empty | SHA-256 hex digest of the service Bearer secret; required when auth is enabled. |
| `RATE_LIMIT_ENABLED` | `false` | Enables a bounded in-process token bucket on `/api/v1`. |
| `RATE_LIMIT_REQUESTS_PER_SECOND` | `10` | Token refill rate when limiting is enabled. |
| `RATE_LIMIT_BURST` | `20` | Per-key burst capacity. |
| `RATE_LIMIT_MAX_CLIENTS` | `10000` | Maximum resident limiter buckets. |
| `RATE_LIMIT_IDLE_TTL` | `10m` | Idle bucket retention before cleanup. |
| `CURSOR_SIGNING_KEY` | Empty outside Compose | 64-character hex HMAC key for signed cursor pagination; required in production. |

Copy [`.env.example`](.env.example) for the complete local contract. Explicit
HTTP collector URLs require `OTEL_EXPORTER_OTLP_INSECURE=true`; production
rejects plaintext telemetry and requires HTTPS.

When `METRICS_ENABLED=true`, the app mounts a Prometheus scrape handler at
`/metrics`. The isolated registry exposes HTTP request count, duration, and
in-flight gauges using method, matched route pattern, and bounded status labels.
It deliberately omits Go/process collectors and never uses raw URLs. Keep the
scrape endpoint behind deployment-level network controls; it is disabled by
default.

HTTP tracing accepts W3C `traceparent`, creates server spans with matched route
patterns, and correlates access events through `trace_id`, `span_id`, and
`request_id`. An empty OTLP endpoint creates no exporter and performs no
collector I/O. Exported spans exclude queries, bodies, request headers, client
addresses, and raw unmatched paths. Shutdown flushes the batch exporter within
a bounded deadline.

Authentication and rate limiting are deliberately opt-in. Set
`AUTH_ENABLED=true` and provide a SHA-256 digest (for example,
`printf %s 'a-long-random-secret' | sha256sum | awk '{print $1}'`) to protect only the
versioned `/api/v1` routes; liveness, readiness, and the private metrics
endpoint remain available to deployment tooling. The built-in verifier is a
single service-to-service credential, not end-user authorization. Downstream
services should replace it with an OIDC/JWT/JWKS authenticator when they need
identity, issuer/audience checks, key rotation, or per-resource authorization.
Missing or invalid credentials return `401` with a generic message and a
`WWW-Authenticate` challenge; no credential is logged or echoed.

Set `RATE_LIMIT_ENABLED=true` to add a bounded in-memory token bucket to the
same API group. Authenticated principals receive separate buckets; failed or
missing credentials share one anonymous bucket, and auth-disabled deployments
use the direct peer IP (never `X-Forwarded-For`). A `429` response includes
`Retry-After`. The limiter is per process and intentionally not a replacement
for an API gateway or shared Redis policy in a multi-replica deployment.

The same bounded identity policy scopes idempotent creates. With authentication
enabled, a key belongs to the authenticated principal; with authentication
disabled, it belongs to the direct peer address. Forwarded headers are never
trusted for either policy.

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

This repository is designed to be used as a GitHub template. In the canonical
repository, enable **Settings → General → Template repository** before sharing
it with a team. Then protect `main` with the rules and required checks listed
in [`docs/repository-settings.md`](docs/repository-settings.md). Those are
GitHub settings and cannot be represented in Git history.

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
| `GET` | `/api/v1/items?limit=20&offset=0` | Deterministic newest-first listing (legacy offset form) |

The first list response can include `meta.next_cursor`. Use that opaque value
for a stable keyset continuation:

```sh
curl --fail --get 'http://127.0.0.1:8080/api/v1/items?limit=20&cursor=<next_cursor>'
```

Never combine `cursor` and `offset`. Cursors are signed with
`CURSOR_SIGNING_KEY`, are valid for 24 hours, and should be treated as
untrusted transport tokens rather than credentials.

Create an item:

```sh
curl --fail --request POST http://127.0.0.1:8080/api/v1/items \
  --header 'Content-Type: application/json' \
  --data '{"name":"Mechanical keyboard","description":"Hot-swappable"}'
```

### Retrying a create safely

Add one `Idempotency-Key` HTTP token when a client may retry after a timeout or
connection failure:

```sh
curl --fail --request POST http://127.0.0.1:8080/api/v1/items \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: order-20260810-0001' \
  --data '{"name":"Mechanical keyboard","description":"Hot-swappable"}'
```

The first successful call returns `201 Created`. Repeating the same key with
the same canonical payload (name and description after edge trimming) returns
`200 OK`, the identical Item envelope and `Location`, and
`Idempotency-Replayed: true`. Records are retained for 24 hours after the
successful atomic write; after expiry the key can represent a new operation.
Use exactly one non-empty HTTP token value (1–255 ASCII bytes). Duplicate,
malformed, or oversized headers are rejected with `400` before the service is
called. A retained key with a different payload returns `409` with
`idempotency_conflict`; a concurrent request may return `409` with
`idempotency_in_progress` and `Retry-After: 1`. If the configured store cannot
provide the atomic capability, the keyed request returns sanitized `503`
`idempotency_unavailable` rather than silently falling back to a non-atomic
create. Raw keys, scopes, and fingerprints are hashed before persistence and
are not included in logs or error bodies.

Successful responses use a `data` envelope. List responses include
`meta.limit` and `meta.has_more`; the legacy offset form includes `meta.offset`
and may include a signed `meta.next_cursor`. Follow-up requests may send the
opaque `cursor` query parameter instead of `offset`; the two parameters are
mutually exclusive. Cursor pages use the immutable `(created_at,id)` keyset,
so inserts newer than the boundary do not shift already-issued pages. The
cursor is integrity-protected, expires after 24 hours, and contains no item
payload, but it is not encrypted. Names and descriptions are trimmed and
validated by Unicode rune count. Unknown JSON fields, malformed
JSON, trailing JSON values, and bodies larger than 1 MiB are rejected.

Errors use a stable shape and never expose provider details:

```json
{"error":{"code":"not_found","message":"item not found"}}
```

Typical status mappings are `400` for malformed transport input, `422` for
domain validation, `404` for a missing item, `409` for a conflict (including
the idempotency conflict/in-progress outcomes), `500` for an unavailable
internal dependency, and `503` when an explicitly requested idempotency or
cursor capability is unavailable. Forged or expired cursors use `400
invalid_cursor`.

Every response includes an `X-Request-ID` header. A valid single upstream
value is preserved; missing, malformed, oversized, or duplicate values are
replaced with a canonical UUIDv4. The HTTP adapter emits one structured access
event through the process logger with the request ID, method, route pattern,
status, response bytes, and duration. Query strings, request bodies, and raw
unmatched paths are deliberately excluded from logs. Handler panics return the
stable internal-error envelope without exposing panic details or request
headers; automatic trailing-slash redirects are disabled so malformed route
spellings still receive an ID and access event.
When the optional security controls are enabled, API errors use stable
`unauthorized` and `rate_limited` codes and retain the same request-ID and
privacy guarantees.

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
| [`internal/item/postgres`](internal/item/postgres) | PostgreSQL adapter, atomic idempotency path, and integration test |
| [`internal/controller/http`](internal/controller/http) | Router and operational endpoints |
| [`pkg/httpserver`](pkg/httpserver) | Reusable server lifecycle module |
| [`pkg/logger`](pkg/logger) | Process logging seam |
| [`pkg/metrics`](pkg/metrics) | Isolated, bounded Prometheus HTTP metrics |
| [`pkg/telemetry`](pkg/telemetry) | Optional OTLP/HTTP tracing provider |
| [`pkg/auth`](pkg/auth) | Strict Bearer parsing and digest verification seam |
| [`pkg/ratelimit`](pkg/ratelimit) | Bounded concurrent in-process token bucket |
| [`db/migrations`](db/migrations) | Versioned PostgreSQL schema changes, including the idempotency-key retention table |
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

Migration `0000002_create_item_idempotency_keys` adds the hashed-key replay
table. It stores only a SHA-256 key hash, a SHA-256 canonical-request hash, the
referenced Item UUID, and timestamps. Expired records are cleaned in bounded
batches during idempotent creates; the 24-hour retention window starts only
after the Item and replay record commit together. Keep the migration applied
before enabling keyed creates against PostgreSQL.

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
- [HTTP observability guide](docs/observability.md)
- [HTTP security controls](docs/security.md)
- [Downstream template smoke test](docs/template-smoke.md)
- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Support](SUPPORT.md)
- [Changelog](CHANGELOG.md)
- [Recommended GitHub repository settings](docs/repository-settings.md)

## License

Distributed under the [MIT License](LICENSE).
