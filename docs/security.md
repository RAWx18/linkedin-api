# Security

## Secrets

Credentials are read only from the environment or the deployment secret store.
They are never committed, logged, or included in responses or errors. The config
type implements a `LogValue` that reports only non-sensitive fields, so logging
the config cannot leak a cookie or a key.

## SSRF

The supplied `url` is never fetched. The URL layer validates it against a strict
`linkedin.com` host allowlist, rejects anything else, and extracts the public
identifier. Every upstream request is then built against the fixed
`LINKEDIN_BASE_URL`, so a caller cannot steer the service at an arbitrary host.

## Input and response bounds

The input URL and the extracted identifier are bounded by length and character
set. Upstream responses are read through a size cap, so a large or hostile
response cannot exhaust memory.

## Public API

`GET /v1/profile` is protected by API-key auth and two layers of rate limiting:
per client IP and per API key. Keys are checked with a constant-time comparison.
The per-IP limiter keys on the connection's remote address; behind a proxy that
terminates client connections, preserve the client address if you need accurate
per-client limits.

The API key authorizes access to this service and is distinct from the LinkedIn
session credentials. The server's API key is never shipped to the browser: the
co-hosted UI is served from the same origin and its requests are admitted via
`Sec-Fetch-Site: same-origin`, which browsers set from the real origin and page
scripts cannot forge, so a cross-site page cannot bypass the key. A non-browser
client can send that header directly, but it stays bound by the per-IP rate limit,
the aggregate upstream rate and concurrency caps, and the circuit breaker, so it
gains no secret and no extra upstream capacity; the key remains the access-control
and per-key attribution mechanism for programmatic callers. The admin endpoint is
never exempt. `/metrics` is unauthenticated for scraping and holds no secrets;
restrict it at the network layer if it should not be publicly reachable.

## Protecting the upstream session

The most important protection is keeping abusive traffic off LinkedIn. Every
retrieval passes through a single gate before any upstream call:

- Concurrent identical lookups are coalesced, so a burst for one profile makes one
  upstream request, not many.
- An aggregate rate limit and a concurrency ceiling bound how much traffic reaches
  LinkedIn at once, regardless of how many distinct URLs an attacker rotates
  through. Overflow is rejected in microseconds with no upstream call.
- A circuit breaker opens after repeated upstream failures (auth rejection, 429,
  timeout, or unavailability) and stays open for a cooldown, during which every
  request is rejected without touching LinkedIn. A 429's Retry-After extends the
  cooldown so LinkedIn's own backoff is honored. After the cooldown a single probe
  decides whether to close.
- Confirmed-missing profiles are negatively cached for a short window so the same
  bad URL is not retried upstream.
- Authentication and login-redirect responses are treated as a likely session
  invalidation: the breaker trips after fewer of them and stays open for a long,
  doubling cooldown, so a challenged session is not hammered. The state is exposed
  as `upstream_session_healthy` and clears on a successful probe once the cookies
  are refreshed.

These limits live in each instance's memory, so the deployment runs a single
replica by default (see [deployment.md](deployment.md)); scaling out would
multiply the limits against one shared session. Running more than one replica
stays safe, each instance enforces its own limits and writes its own audit file,
but the caps become per instance, so raise the replica count only after lowering
the per-instance limits to keep total upstream pressure within the one session's
tolerance. When credentials expire or the source is blocked, the breaker turns the
failure into fast, controlled 502 and 503 responses, and the service recovers on
its own once access returns.

## Optional caller-supplied sessions

A caller may supply its own LinkedIn session for a single request through the
`X-LinkedIn-Li-At` and `X-LinkedIn-JSESSIONID` headers. The feature is optional,
controlled by `LINKEDIN_ALLOW_CALLER_SESSION`, and is a convenience for a caller
with its own authorized session, never a way to bypass LinkedIn.

The credentials are treated as request-scoped secrets and are strictly isolated:

- They are used only for that one request and never fall back to, mix with, or
  substitute the server session. A caller-session failure never tries the server
  session.
- The raw values live only in request memory. They are never written to the
  cache, audit store, logs, metrics, traces, or any persistent store, and are
  never used as a cache key, metric label, log field, or request id.
- Caller-session requests bypass the shared cache and request coalescing, so a
  profile fetched with one caller's session can never be served to another caller.
- For operational tracking a non-reversible keyed fingerprint (`cs_...`) is
  derived from the credentials. It cannot be reversed to the cookies and is the
  only caller-session identifier that ever appears in metadata.
- When LinkedIn rejects a caller session (401, 403, or a login redirect), only
  that session is marked unhealthy for a short, bounded window. It is fast-failed
  with `caller_session_invalid` during that window, with no retries and no
  upstream traffic. A caller's bad session never opens the shared server-session
  circuit, and a dead server session never invalidates caller sessions. A rate
  limit or broad restriction still engages the global upstream protection for all
  traffic.

All existing protections (API-key auth, per-IP and per-key limits, the aggregate
upstream rate and concurrency caps, URL validation, timeouts, and response-size
limits) apply identically regardless of which session a request uses, so caller
credentials cannot be used to bypass any limit.

## Request auditing

Every API request is recorded to a durable store for abuse investigation and
usage analysis. The store holds no secrets: API keys are reduced to a one-way
fingerprint, the full URL is never kept (only the normalized public identifier),
and no cookies or authorization headers are stored. Client IPs are retained for
investigation and bounded by a retention window. The `/admin/usage` query
endpoint is disabled unless `AUDIT_ADMIN_KEYS` is set and is protected by those
admin keys, which are separate from the public `API_KEYS`. Auditing is best
effort and isolated from the request path: a full buffer drops records and a
failing store is counted, never surfaced to a caller. Details are in
[auditing.md](auditing.md).

## Runtime

The container image is distroless and runs as a non-root user.

## What the service does not do

It does not try to bypass authentication, CAPTCHAs, or rate limits, and it does
not solve challenges. It uses one authenticated session and respects upstream
limits.
