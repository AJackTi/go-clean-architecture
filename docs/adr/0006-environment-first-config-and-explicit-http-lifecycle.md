---
status: accepted
date: 2026-08-09
---

# Use environment-first configuration and an explicit HTTP lifecycle

Runtime configuration is a flat, validated environment contract (`APP_ENV`,
`HTTP_PORT`, `LOG_LEVEL`, and `DATABASE_URL`) with development defaults and an
explicit production database requirement. The composition root constructs an
`httpserver.Server`, calls `Start`, observes its terminal error through
`Notify`, and performs bounded graceful shutdown on context cancellation or
SIGTERM; the server carries read, write, idle, header, and shutdown timeouts.

## Considered options

- **Working-directory-relative YAML configuration:** rejected because it made
  startup depend on where the binary was launched and duplicated deployment
  settings.
- **A server that starts itself in `New`:** rejected because hidden goroutines
  obscure bind failures and make startup ordering difficult to test.

## Consequences

- The same binary can run locally, in Compose, or in a deployment platform
  without rewriting configuration files.
- Startup and shutdown ordering are visible in `internal/app` and are
  deterministic in tests; terminal serve errors are not silently swallowed.
- Every deployment must provide a valid database URL in production and must
  allow enough shutdown time for in-flight requests to finish.
- The application entrypoint initializes logging once and flushes it on exit;
  modules use the logger seam rather than creating independent global loggers.
