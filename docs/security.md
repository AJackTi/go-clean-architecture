# HTTP security controls

The template ships safe seams, not a universal identity policy. Both controls
below are disabled by default so a freshly generated service remains compatible
with local probes and existing clients.

## Bearer authentication

Set `AUTH_ENABLED=true` and configure `AUTH_BEARER_TOKEN_SHA256` with the
64-character SHA-256 digest of a high-entropy service secret. For example:

```sh
secret='replace-with-a-secret-manager-value'
printf %s "$secret" | sha256sum | awk '{print $1}'
```

Keep the secret itself in a secret manager and send it as
`Authorization: Bearer <secret>`. The parser accepts exactly one strict Bearer
header, rejects duplicate/malformed values, and the verifier compares fixed
SHA-256 digests in constant time. A missing or invalid credential receives:

```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="api"
Cache-Control: no-store
```

The response body uses the stable `unauthorized` error code and never reveals
which part of a credential failed. The configured digest is not a reversible
secret; rotation still requires the normal deployment/configuration rollout.

This is one shared machine credential, not user authentication or
authorization. Replace the `auth.Authenticator` option with an OIDC/JWT/JWKS
adapter for issuer/audience validation, key rotation, scopes, and resource
ownership.

## Rate limiting

Set `RATE_LIMIT_ENABLED=true` and tune:

| Variable | Meaning |
| --- | --- |
| `RATE_LIMIT_REQUESTS_PER_SECOND` | Token refill rate. |
| `RATE_LIMIT_BURST` | Maximum burst tokens per key. |
| `RATE_LIMIT_MAX_CLIENTS` | Maximum resident buckets. |
| `RATE_LIMIT_IDLE_TTL` | Duration before an idle bucket is reclaimed. |

Authenticated principals use an opaque hash-derived key. Invalid/missing
credentials share one `anonymous` bucket, which prevents attackers from
allocating a bucket for every guessed token. With auth disabled, the limiter
uses `RemoteAddr` after parsing the direct peer IP; `X-Forwarded-For` is never
trusted automatically. Exhausted requests receive `429 rate_limited` and a
bounded `Retry-After` value.

The limiter is in-process and each replica has independent state. Put a shared
limit at a trusted gateway or implement a reviewed distributed limiter when
horizontal scaling requires a global quota. Do not log bearer values, limiter
keys, or forwarded client headers.

## Idempotent creates

`POST /api/v1/items` supports idempotency only when the request includes one
valid `Idempotency-Key` header. The value is an ASCII HTTP token between 1 and
255 bytes; duplicate, empty, whitespace, control, non-ASCII, and oversized
values are rejected before domain or persistence work. Treat the token as a
credential-like value: generate it with enough entropy for the client
operation, do not put sensitive data in it, and do not reuse it for a different
payload during its retention window.

The router scopes the token to the authenticated principal when Bearer auth is
enabled. With auth disabled, it uses the direct peer address. The application
does not trust `X-Forwarded-For` (or other client-supplied forwarding headers)
for this decision; deployments behind a proxy should establish a trusted
identity at the proxy and pass that identity through an authenticated adapter.

The service canonicalizes the create fields by trimming their edges and stores
only SHA-256 digests of the scoped key and canonical request fingerprint. Raw
tokens, request bodies, fingerprints, and scopes are not logged, persisted, or
included in error responses. PostgreSQL stores the digest record in
`item_idempotency_keys`; memory deployments enforce a resident-entry cap.

The Item insert and replay record are one atomic operation. A first success is
`201`; a matching retry within the fixed 24-hour post-completion retention
window is `200` with `Idempotency-Replayed: true`; a changed payload is `409
idempotency_conflict`; and a same-key operation currently in flight is `409
idempotency_in_progress` with `Retry-After: 1`. A requested key without an
atomic-capable store fails closed with `503 idempotency_unavailable`, rather
than silently degrading to an ordinary create. Apply migration `0000002` to
PostgreSQL before sending keyed requests.

Idempotency is transport-level retry protection, not authorization, payment
deduplication, or a permanent business operation identifier. The 24-hour
retention window is intentionally finite; use a domain-owned durable operation
ID when a longer guarantee is required.
