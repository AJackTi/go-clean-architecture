---
status: accepted
date: 2026-08-09
---

# Keep the Item feature as a vertical slice

The template keeps Item language, invariants, use cases, stable errors, and the
`Store` interface together in `internal/item`; HTTP, memory, and PostgreSQL
code are feature-owned adapters that point inward. The composition root wires
concrete adapters once, so callers depend on a small seam rather than a set of
generic entity/use-case/repository layers.

## Consequences

- A new feature can be discovered and tested locally by following one vertical
  slice (`internal/<feature>` plus its adapters).
- The Item module has no Gin or pgx dependency, which keeps domain tests fast
  and preserves locality when transport or storage changes.
- Some duplication between feature slices is preferable to a premature shared
  abstraction; promote code to `pkg/` only after it has multiple real callers.
- Adding a second adapter is an intentional seam decision, and its contract
  must include ordering, cancellation, error mapping, and configuration—not
  only method signatures.

Revisit this decision if the repository grows multiple bounded contexts with
independent ownership or if a shared module demonstrably earns more leverage
than locality.
