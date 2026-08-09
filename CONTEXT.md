# Go clean architecture context

This repository is a production-oriented Go HTTP template. It contains one
complete **Item** vertical slice so a team can copy the shape, policies, tests,
and delivery workflow when adding a real domain feature.

## Language

**Item**: An addressable resource with a server-generated identity, a name, a description, and a creation time.

**Name**: A required edge-trimmed Unicode label containing 1–120 characters.

**Description**: An optional edge-trimmed Unicode text value containing at most 2,000 characters.

**Item identity**: An RFC 4122 UUID version 4 assigned by the application when an Item is created.

**Creation time**: A server-assigned UTC instant that is immutable for the lifetime of an Item.

**Item page**: A deterministic newest-first collection containing Items, a
normalized limit, optional legacy offset metadata, an optional signed cursor,
and a `has_more` flag.

**Cursor position**: The exclusive `(created_at,id)` boundary in the Item
ordering. Cursor pages use this immutable keyset rather than an offset.

**Idempotency key**: A client-supplied HTTP token used to retry one Item create
without producing a second Item. The transport accepts 1–255 ASCII HTTP-token
bytes and rejects duplicate header values.

**Canonical create fingerprint**: A SHA-256 digest of the validated,
edge-trimmed `name` and `description` fields. Equivalent padding therefore
replays, while a changed canonical payload conflicts.

**Idempotency scope**: A bounded caller identity combined with the route and
hashed with the key. Authenticated requests use the principal; auth-disabled
requests use the direct peer address. `X-Forwarded-For` is never trusted.

**Idempotency record**: A bounded-retention mapping from an opaque key hash and
fingerprint to the committed Item. The default replay window is 24 hours after
successful completion.

## Architecture language

**Item module**: The inward-facing module that owns Item policy and exposes use cases plus the `Store` Interface.

**Store**: The persistence Interface at the Item module's seam; memory and PostgreSQL are its current adapters.

**HTTP adapter**: The transport adapter that translates JSON/URL concerns into Item use-case calls and stable responses.

**Composition root**: `internal/app`, the one place that chooses concrete adapters and assembles the running process.

## Relationships

- An **Item** has exactly one **Item identity**, one **Name**, one **Description**, and one **Creation time**.
- The Item module canonicalizes create input, assigns the **Item identity** and **Creation time**, and then writes the Item through its Store seam.
- An **Item page** orders Items by `created_at DESC, id DESC`; the second key makes ties deterministic.
- A cursor page applies a strict keyset predicate after its returned boundary;
  the first offset-compatible response may advertise `meta.next_cursor` as a
  migration path.
- The HTTP adapter translates a request into the Item module's input and translates stable domain errors into transport responses; it does not own Item policy.
- An idempotent create is one atomic persistence operation: the Item row and
  its replay record commit together, or neither commits. A replay returns the
  original Item with HTTP 200; the first successful create returns HTTP 201.

## Operational contracts

- **Liveness** is the process-level signal at `/api/health`; it is independent of PostgreSQL.
- **Readiness** is the dependency-level signal at `/api/healthz`; it performs a PostgreSQL ping with a two-second timeout.
- Liveness can succeed while Readiness fails, allowing an orchestrator to keep the process alive while withholding traffic during a dependency outage.
- Every HTTP response carries an `X-Request-ID`; one valid upstream token is preserved and all other values are replaced with a UUIDv4.
- The HTTP composition module emits one structured access event after each request through the process logger. It records request ID, method, route pattern, status, response bytes, and duration, but never query strings, bodies, or raw unmatched paths.
- Handler panics return the stable internal-error envelope without panic values or request metadata; automatic trailing-slash and fixed-path redirects are disabled so route-shape errors remain observable.
- Prometheus HTTP metrics are opt-in and use only bounded method, matched-route, and status labels; the disabled-by-default `/metrics` endpoint never exposes raw URLs or process/runtime collectors.
- HTTP server spans extract W3C trace context and export over optional OTLP/HTTP. Empty exporter configuration performs no collector I/O, production requires TLS, and span attributes follow the same no-query/body/header/raw-path privacy boundary as access events.
- Authentication and rate limiting are opt-in transport policies for `/api/v1`: the starter Bearer verifier compares a configured SHA-256 digest without retaining the secret, and the in-process token bucket has bounded keys and memory. These controls do not imply end-user authorization or trust forwarded client headers.
- Cursor pagination is an optional capability on the same Item Store. Tokens
  are versioned, HMAC-authenticated, URL-safe, bounded to 512 bytes, and expire
  after 24 hours; `CURSOR_SIGNING_KEY` is supplied by the composition root and
  must be shared by replicas.
- Idempotency is opt-in per request through `Idempotency-Key`. Memory and
  PostgreSQL adapters implement the cohesive atomic seam; a service never
  combines an ordinary Item store with a separate idempotency backend. Raw
  keys, scopes, and fingerprints are hashed before adapter storage and are not
  logged. A retained payload mismatch maps to `409 idempotency_conflict`, an
  in-flight key maps to `409 idempotency_in_progress` with `Retry-After`, and a
  missing atomic capability maps to sanitized `503 idempotency_unavailable`.
- The checked-in OpenAPI 3.1 document describes the same route set and response contract; `go test ./docs` fails when the document and mounted routes drift.

## Invariants and policies

- Callers cannot supply an Item identity or creation time in a create request.
- Name and description are trimmed at the edges, while internal whitespace and valid Unicode are preserved.
- A list request defaults to 20 Items and is capped at 100 Items. The Item module asks the Store for one look-ahead row to calculate `has_more`.
- `cursor` and `offset` query parameters are mutually exclusive. Cursor pages
  are not snapshots: newer inserts are excluded after a boundary and deletes
  can shorten a later page.
- Stores return deterministic newest-first ordering and honor cancellation from the supplied context.
- Domain errors are matched with `errors.Is`/`errors.As`; HTTP clients receive stable error codes and sanitized messages rather than provider details.
- Invalid create input or a failed atomic persistence attempt does not retain an
  idempotency record, so a client can safely correct and retry. Once an Item is
  committed, its replay record remains even if the original request context is
  cancelled after commit.
- PostgreSQL cleans expired idempotency records in bounded batches during keyed
  operations; the replay table stores fixed-size hashes rather than raw client
  tokens or request bodies. The migration must be applied before keyed creates
  are enabled.
- The `internal/item` package has no dependency on Gin, pgx, PostgreSQL, or filesystem configuration. Adapters point inward to its interfaces and types.
- There is currently no update or delete use case. Adding either requires an explicit policy for mutability, authorization, and pagination effects.

## Module map

| Module | Location | Responsibility |
| --- | --- | --- |
| Composition root | `internal/app` | Validate configuration, open PostgreSQL, wire the Item module and HTTP adapter, and own shutdown. |
| Item module | `internal/item` | Define Item language, validation, use cases, pagination, and stable errors. |
| Memory adapter | `internal/item/memory` | Provide a race-safe, deterministic Store for tests and lightweight local runs. |
| PostgreSQL adapter | `internal/item/postgres` | Implement Store with pgx, explicit parameterized SQL, and atomic idempotent creates. |
| HTTP adapter | `internal/item/httpapi` | Decode strict JSON, parse paths/queries, enforce body limits, and map errors/statuses. |
| HTTP composition | `internal/controller/http` | Mount health endpoints and versioned routes, assign request IDs, and emit structured access events. |
| Runtime modules | `pkg/httpserver`, `pkg/logger` | Provide explicit HTTP lifecycle and process-wide structured logging seams. |
| Telemetry modules | `pkg/metrics`, `pkg/telemetry` | Own an isolated Prometheus registry and an optional OTLP/HTTP tracing provider with bounded lifecycle. |
| Security modules | `pkg/auth`, `pkg/ratelimit` | Provide an opt-in Bearer verification seam and a bounded in-process limiter; they do not implement resource authorization. |
| Commands | `cmd/app`, `cmd/bootstrap`, `cmd/migrate`, `cmd/healthcheck` | Customize a clean checkout, start the app, apply migrations, and probe readiness in the minimal runtime image. |
| Schema | `db/migrations` | Versioned PostgreSQL schema changes with paired up/down files, including the idempotency replay table. |
| Contract documentation | `docs/openapi.yaml`, `docs/openapi_test.go` | Publish and verify the machine-readable HTTP interface. |

The intended dependency direction is:

```text
cmd/app
└── internal/app
    ├── internal/controller/http
    │   └── internal/item/httpapi ──→ internal/item
    └── internal/item/postgres ─────→ internal/item
```

The arrows describe compile-time dependencies, not request flow. A new
feature should follow the same vertical-slice shape; shared code belongs in
`pkg/` only when it has a genuinely reusable interface and at least two real
callers.

## Change guide

When adding a domain feature:

1. Define its language, invariants, and use cases in a new `internal/<feature>` module.
2. Put the persistence seam beside those use cases; keep adapters in feature-owned subpackages.
3. Add a memory or fake adapter for fast contract tests, then add the production adapter and its integration tests.
4. Add an explicit migration pair under `db/migrations` when the schema changes; never make application startup mutate the schema.
5. Mount transport routes under the current version, preserve response envelopes, and add a new version for breaking changes.
6. Wire the feature once in `internal/app`, update `CONTEXT.md`, and record decisions that are costly or surprising to reverse in `docs/adr/`.

The interface is the test surface: tests should cross the same seams that
callers use. Keep domain tests independent of Docker, and keep PostgreSQL and
HTTP behaviour covered by adapter/integration tests.

## Example dialogue

> **Developer:** "Can a caller choose an Item identity or sort an Item page by name?"
>
> **Domain expert:** "No. The Item module assigns a UUIDv4 identity, and every Item page is newest-first by creation time with the UUID as a deterministic tie-breaker."
>
> **Developer:** "What should happen when PostgreSQL is unavailable?"
>
> **Domain expert:** "Liveness remains `ok`, Readiness becomes `unavailable`, and the application never exposes the database error to an HTTP client."

## Flagged ambiguities

- **Item** is intentionally generic template language, not a claim about the eventual product domain. Rename it consistently when bootstrapping a project.
- **Store** means the Item persistence interface, not necessarily a PostgreSQL database; the memory and PostgreSQL adapters are interchangeable implementations of that interface.
- **Offset** pagination remains deterministic but is not snapshot-based: inserts
  between requests can move an Item between pages. Cursor pagination is the
  preferred deep-page contract, while preserving offset for compatibility.
- **Creation time** is assigned once because no update use case exists today; future mutability must not silently change ordering semantics.
- The code type is named `item.Service`, but in domain discussions it means the Item use-case module, not a generic catch-all service layer.
