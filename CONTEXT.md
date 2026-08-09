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

**Item page**: A deterministic newest-first collection containing Items, a normalized limit and offset, and a `has_more` flag.

## Architecture language

**Item module**: The inward-facing module that owns Item policy and exposes use cases plus the `Store` Interface.

**Store**: The persistence Interface at the Item module's seam; memory and PostgreSQL are its current adapters.

**HTTP adapter**: The transport adapter that translates JSON/URL concerns into Item use-case calls and stable responses.

**Composition root**: `internal/app`, the one place that chooses concrete adapters and assembles the running process.

## Relationships

- An **Item** has exactly one **Item identity**, one **Name**, one **Description**, and one **Creation time**.
- The Item module canonicalizes create input, assigns the **Item identity** and **Creation time**, and then writes the Item through its Store seam.
- An **Item page** orders Items by `created_at DESC, id DESC`; the second key makes ties deterministic.
- The HTTP adapter translates a request into the Item module's input and translates stable domain errors into transport responses; it does not own Item policy.

## Operational contracts

- **Liveness** is the process-level signal at `/api/health`; it is independent of PostgreSQL.
- **Readiness** is the dependency-level signal at `/api/healthz`; it performs a PostgreSQL ping with a two-second timeout.
- Liveness can succeed while Readiness fails, allowing an orchestrator to keep the process alive while withholding traffic during a dependency outage.
- Every HTTP response carries an `X-Request-ID`; one valid upstream token is preserved and all other values are replaced with a UUIDv4.
- The HTTP composition module emits one structured access event after each request through the process logger. It records request ID, method, route pattern, status, response bytes, and duration, but never query strings, bodies, or raw unmatched paths.
- Handler panics return the stable internal-error envelope without panic values or request metadata; automatic trailing-slash and fixed-path redirects are disabled so route-shape errors remain observable.
- Prometheus HTTP metrics are opt-in and use only bounded method, matched-route, and status labels; the disabled-by-default `/metrics` endpoint never exposes raw URLs or process/runtime collectors.
- HTTP server spans extract W3C trace context and export over optional OTLP/HTTP. Empty exporter configuration performs no collector I/O, production requires TLS, and span attributes follow the same no-query/body/header/raw-path privacy boundary as access events.
- The checked-in OpenAPI 3.1 document describes the same route set and response contract; `go test ./docs` fails when the document and mounted routes drift.

## Invariants and policies

- Callers cannot supply an Item identity or creation time in a create request.
- Name and description are trimmed at the edges, while internal whitespace and valid Unicode are preserved.
- A list request defaults to 20 Items and is capped at 100 Items. The Item module asks the Store for one look-ahead row to calculate `has_more`.
- Stores return deterministic newest-first ordering and honor cancellation from the supplied context.
- Domain errors are matched with `errors.Is`/`errors.As`; HTTP clients receive stable error codes and sanitized messages rather than provider details.
- The `internal/item` package has no dependency on Gin, pgx, PostgreSQL, or filesystem configuration. Adapters point inward to its interfaces and types.
- There is currently no update or delete use case. Adding either requires an explicit policy for mutability, authorization, and pagination effects.

## Module map

| Module | Location | Responsibility |
| --- | --- | --- |
| Composition root | `internal/app` | Validate configuration, open PostgreSQL, wire the Item module and HTTP adapter, and own shutdown. |
| Item module | `internal/item` | Define Item language, validation, use cases, pagination, and stable errors. |
| Memory adapter | `internal/item/memory` | Provide a race-safe, deterministic Store for tests and lightweight local runs. |
| PostgreSQL adapter | `internal/item/postgres` | Implement Store with pgx and explicit parameterized SQL. |
| HTTP adapter | `internal/item/httpapi` | Decode strict JSON, parse paths/queries, enforce body limits, and map errors/statuses. |
| HTTP composition | `internal/controller/http` | Mount health endpoints and versioned routes, assign request IDs, and emit structured access events. |
| Runtime modules | `pkg/httpserver`, `pkg/logger` | Provide explicit HTTP lifecycle and process-wide structured logging seams. |
| Telemetry modules | `pkg/metrics`, `pkg/telemetry` | Own an isolated Prometheus registry and an optional OTLP/HTTP tracing provider with bounded lifecycle. |
| Commands | `cmd/app`, `cmd/bootstrap`, `cmd/migrate`, `cmd/healthcheck` | Customize a clean checkout, start the app, apply migrations, and probe readiness in the minimal runtime image. |
| Schema | `db/migrations` | Versioned PostgreSQL schema changes with paired up/down files. |
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
- **Offset** pagination is deterministic but not snapshot-based: inserts between requests can move an Item between pages. A cursor contract must be designed explicitly before replacing it.
- **Creation time** is assigned once because no update use case exists today; future mutability must not silently change ordering semantics.
- The code type is named `item.Service`, but in domain discussions it means the Item use-case module, not a generic catch-all service layer.
