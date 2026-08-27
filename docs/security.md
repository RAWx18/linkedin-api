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

`GET /v1/*` is protected by API key auth and two layers of rate limiting: per
client IP and per API key. Keys are checked with a constant-time comparison. The
per-IP limiter keys on the connection's remote address; behind a proxy that
terminates client connections, preserve the client address if you need accurate
per-client limits.

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
