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
`csrf-token` header, matching what the web client does. The client also sets a
browser `User-Agent`, `x-restli-protocol-version: 2.0.0`, and `x-li-lang`. The
session object is built once and never mutated, so concurrent requests cannot
corrupt shared auth state. Setting these values is covered in
[configuration.md](configuration.md#session-cookies).

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

## Volatility

These endpoints are internal and undocumented, so their paths and shapes can
change without notice. The parser is defensive, but an upstream change can still
reduce the fields returned until the mapping is updated. That risk is confined to
the `linkedin` and `linkedin/parse` packages.
