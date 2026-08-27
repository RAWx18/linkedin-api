# LinkedIn integration

LinkedIn's web client talks to an internal JSON API under
`https://www.linkedin.com/voyager/api/`. The service authenticates with a
signed-in session and reads the profile from the identity DASH collection.

## Endpoint

| Purpose | Endpoint |
| --- | --- |
| Base profile by public identifier | `identity/dash/profiles?q=memberIdentity&memberIdentity={publicId}` |

The finder returns a collection whose first element is the member entity. An
empty collection means the profile does not exist.

## Authentication

The API expects a signed-in browser session. Two cookie values from a normal
login provide it:

- `LINKEDIN_LI_AT`, the `li_at` session cookie.
- `LINKEDIN_JSESSIONID`, the `JSESSIONID` value, which looks like `ajax:1234...`.

Each request sends both as cookies and the unquoted `JSESSIONID` as the
`csrf-token` header, matching what the web client does. The session object is
built once and never mutated, so concurrent requests cannot corrupt shared auth
state. Setting these values is covered in
[configuration.md](configuration.md#session-cookies).

## Session compatibility

A LinkedIn session is bound to the browser that created it. The strongest signal
LinkedIn uses to decide whether a request belongs to that session is the
`User-Agent`. If the server presents a different `User-Agent` than the browser
where the `li_at` and `JSESSIONID` cookies were obtained, LinkedIn can accept the
first request and then challenge and invalidate the session, so the next request
fails with `upstream_auth_failed`. This is the "works once, then fails" symptom.

The fix is not evasion but correctness: present the same browser context. The
service requires `LINKEDIN_USER_AGENT` to be set to the exact browser `User-Agent`
whenever a session is configured, and refuses to start otherwise. The request
header set is fixed and deterministic, so identical inputs always produce the same
request:

| Header | Value |
| --- | --- |
| `User-Agent` | `LINKEDIN_USER_AGENT`, or a caller's `X-LinkedIn-User-Agent` |
| `Accept` | `application/json` |
| `Accept-Language` | `LINKEDIN_ACCEPT_LANGUAGE` (default `en-US,en;q=0.9`) |
| `X-RestLi-Protocol-Version` | `2.0.0` |
| `X-Li-Lang` | `en_US` |
| `Cookie` | `li_at=...; JSESSIONID="ajax:..."` |
| `Csrf-Token` | the unquoted `JSESSIONID` |

`Accept` deliberately requests the non-normalized collection shape the parser
reads; it is not changed to the web client's normalized media type.

### Manual validation procedure

To confirm the Go request matches your browser without ever exposing secrets:

1. In the browser signed in to LinkedIn, open a profile, then devtools, Network.
   Filter for `voyager/api/identity/dash/profiles` and open the request.
2. Copy the exact `user-agent` request header and set `LINKEDIN_USER_AGENT` to it.
3. Start the service with `LOG_LEVEL=debug`. Each upstream call logs a
   `linkedin upstream request` line with non-sensitive request metadata
   (`user_agent`, `accept`, `accept_language`, cookie names, and whether a CSRF
   token is present). Cookie values, `li_at`, `JSESSIONID`, and the CSRF token are
   never logged.
4. Compare the logged `user_agent` and header names with the browser request. Two
   lookups produce identical request lines, confirming the deterministic model.

### Caller sessions

A caller supplying its own session can also supply the matching browser
`User-Agent` through the optional `X-LinkedIn-User-Agent` header, so its session
stays consistent. It applies only to that request and never changes the server
`User-Agent`.

## Response shape and normalization

The member entity carries the identity and top-card fields. The parser reads only
what the domain model needs and ignores the rest of the large payload:

- Name (`firstName`, `lastName`), `headline`, and `summary`.
- Profile and background pictures, each a `displayImage.vectorImage` with a root
  URL and sized artifacts; the highest-resolution artifact is used.
- `location.countryCode`, published websites, and the verified, influencer, and
  premium flags.

An empty collection maps to a not-found result. A body that cannot be decoded
maps to a parse error. Optional fields are omitted when absent; the parser never
invents values. Identity fields, the public identifier and canonical URL, come
from the validated request, not the response body.

## Timeouts and retries

Each upstream call runs under `HTTP_REQUEST_TIMEOUT`, which spans its attempts,
and the whole lookup runs under `PROFILE_TIMEOUT`. Retries apply only to timeouts,
connection failures, and 5xx responses, up to `HTTP_MAX_RETRIES`, with exponential
backoff and jitter. Authentication failures, login redirects, 404s, and 429s are
never retried. A 429 is surfaced with its `Retry-After` value.

## Session health

Using one signed-in session from a server is inherently more fragile than a
browser: LinkedIn can challenge or invalidate it. The client treats authentication
and login-redirect responses as a likely session invalidation rather than a
routine error. After a small number of them the circuit breaker marks the session
unhealthy, stops issuing upstream requests, and stays open for a long cooldown that
doubles on each failed probe, so a dead session is not hammered into deeper
trouble. While the session is unhealthy, callers receive a controlled `503`.
`upstream_session_healthy` exposes the state, and it clears automatically once a
probe succeeds after the cookies are refreshed. The service never tries to bypass a
challenge or login wall.

Matching the browser `User-Agent` and session context (above) removes the most
common cause of this invalidation. It cannot guarantee a session is never
challenged: a server also differs from a browser in its network origin, and
LinkedIn may still challenge a session used from a datacenter address. The service
does not attempt to work around that. When it happens the session-health breaker
detects it, stops further traffic, and surfaces the state, so the failure stays
contained and is recoverable by refreshing the cookies.

## Volatility

These endpoints are internal and undocumented, so their paths and shapes can
change without notice. The parser is defensive, but an upstream change can still
reduce the fields returned until the mapping is updated. That risk is confined to
the `linkedin` and `linkedin/parse` packages.
