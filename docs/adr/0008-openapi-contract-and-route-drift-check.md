---
status: accepted
date: 2026-08-10
---

# Keep a checked-in OpenAPI contract beside the HTTP adapter

The template publishes one OpenAPI 3.1 document at `docs/openapi.yaml`. It
describes operational probes, versioned Item routes, strict request decoding,
stable response envelopes, pagination limits, and sanitized error codes. A Go
test parses the document and compares its operations with the routes mounted
by `internal/controller/http`.

The document is descriptive rather than a runtime router generator. Gin stays
the HTTP adapter implementation, while the OpenAPI file is the client-facing
Interface used for examples, review, and code generation.

## Consequences

- A route can be reviewed and generated from a stable machine-readable
  contract without introducing a second runtime router.
- Route drift fails in the same quality gate as code tests; schema details and
  examples still require human review for semantic compatibility.
- Breaking changes require a new route version or an explicitly reviewed
  compatible schema change, consistent with ADR-0003.
- Generator-specific annotations remain out of the Item module, preserving
  dependency direction and locality.
