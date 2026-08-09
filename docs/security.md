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
