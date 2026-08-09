---
status: accepted
date: 2026-08-10
---

# Keep starter authentication and rate limiting opt-in

The template provides transport seams for a small service-to-service
deployment without claiming to solve application authorization. When enabled,
`AUTH_ENABLED` protects only `/api/v1` with one Bearer credential whose
configured value is a SHA-256 digest. The verifier parses duplicate and
malformed headers strictly, compares fixed-size digests in constant time, and
never stores or logs the raw secret. Health probes remain unauthenticated.

`RATE_LIMIT_ENABLED` adds a mutex-protected token bucket per authenticated
principal. Failed credentials share one bounded anonymous bucket; when auth is
disabled the key is the direct peer IP and never an untrusted forwarded header.
The bucket map has a configured client cap and idle expiry, and returns a
sanitized `429` response with `Retry-After` when exhausted. The limiter is
explicitly per-process; multi-replica deployments should enforce a shared
policy at an API gateway or with a reviewed distributed store.

The built-in credential is a migration-friendly starter for machine clients,
not an end-user identity system. Downstream services needing issuer,
audience, algorithm, key-rotation, scope, or resource-ownership checks must
replace the `auth.Authenticator` seam with an OIDC/JWT/JWKS implementation.

## Consequences

- A default checkout keeps existing clients and health probes working because
  both controls are disabled.
- Enabling auth does not silently introduce ownership or authorization rules
  into the generic Item example.
- Digest configuration reduces accidental secret retention in config dumps;
  deployments should still generate high-entropy credentials and rotate them
  through their secret manager.
- Bounded, local limiting provides a useful baseline defense while making its
  multi-process limitation explicit.
