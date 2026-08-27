# Configuration

Configuration comes entirely from the environment and is validated at startup.
When `ENV=production`, the service refuses to start if a required secret is
missing, and it reports every problem at once instead of one per restart. A safe
template is in [.env.example](../.env.example).

## Variables

| Variable | Default | Required | Description |
| --- | --- | --- | --- |
| `ENV` | `development` | no | `development` or `production` |
| `SERVER_PORT` | `8080` | no | HTTP listen port |
| `SERVER_READ_TIMEOUT` | `10s` | no | Request read timeout |
| `SERVER_WRITE_TIMEOUT` | `20s` | no | Response write timeout |
| `SERVER_IDLE_TIMEOUT` | `90s` | no | Keep-alive idle timeout |
| `SHUTDOWN_TIMEOUT` | `15s` | no | Graceful shutdown drain time |
| `LINKEDIN_LI_AT` | | production | `li_at` session cookie (secret) |
| `LINKEDIN_JSESSIONID` | | production | `JSESSIONID` value (secret) |
| `LINKEDIN_BASE_URL` | `https://www.linkedin.com` | no | Upstream base, must be a linkedin.com host |
| `LINKEDIN_USER_AGENT` | | when a session is set | Exact browser User-Agent of the session that created the cookies; required when `LINKEDIN_LI_AT` is set |
| `LINKEDIN_ACCEPT_LANGUAGE` | `en-US,en;q=0.9` | no | Accept-Language sent upstream to match the browser |
| `HTTP_REQUEST_TIMEOUT` | `10s` | no | Overall deadline per upstream call |
| `HTTP_MAX_RETRIES` | `2` | no | Retries for transient upstream failures |
| `HTTP_RETRY_BACKOFF` | `300ms` | no | Base backoff, grows with jitter |
| `PROFILE_TIMEOUT` | `15s` | no | Deadline for a whole profile lookup across its upstream calls |
| `LINKEDIN_ALLOW_CALLER_SESSION` | `true` | no | Allow callers to supply their own session via the `X-LinkedIn-Li-At` and `X-LinkedIn-JSESSIONID` headers |
| `CACHE_ENABLED` | `true` | no | Enable the in-memory cache |
| `CACHE_TTL` | `10m` | no | Cache entry lifetime |
| `CACHE_MAX_ENTRIES` | `1000` | no | Cache size cap |
| `RATE_LIMIT_ENABLED` | `true` | no | Enable per-IP rate limiting |
| `RATE_LIMIT_RPS` | `5` | no | Sustained requests per second per IP |
| `RATE_LIMIT_BURST` | `10` | no | Burst allowance per IP |
| `RATE_LIMIT_KEY_RPS` | `10` | no | Sustained requests per second per API key |
| `RATE_LIMIT_KEY_BURST` | `20` | no | Burst allowance per API key |
| `UPSTREAM_MAX_CONCURRENCY` | `4` | no | Max concurrent retrievals sent to LinkedIn |
| `UPSTREAM_RATE_RPS` | `5` | no | Aggregate retrievals per second to LinkedIn |
| `UPSTREAM_RATE_BURST` | `10` | no | Aggregate burst to LinkedIn |
| `UPSTREAM_BREAKER_THRESHOLD` | `5` | no | Consecutive upstream failures before the circuit opens |
| `UPSTREAM_BREAKER_COOLDOWN` | `30s` | no | How long the circuit stays open before a probe |
| `UPSTREAM_SESSION_THRESHOLD` | `2` | no | Auth or challenge responses before the session is treated as unhealthy |
| `UPSTREAM_SESSION_COOLDOWN` | `5m` | no | Base cooldown for an unhealthy session, doubling on repeated failed probes |
| `CALLER_SESSION_UNHEALTHY_TTL` | `5m` | no | How long a rejected caller session is fast-failed before it may be tried again |
| `UPSTREAM_NEG_CACHE_TTL` | `1m` | no | How long a confirmed-missing profile is remembered |
| `API_KEYS` | | production | Comma-separated keys; empty disables auth in dev |
| `LOG_LEVEL` | `info` | no | `debug`, `info`, `warn`, or `error` |
| `LOG_FORMAT` | `json` | no | `json` or `text` |
| `METRICS_ENABLED` | `true` | no | Expose `/metrics` |
| `AUDIT_ENABLED` | `true` | no | Record privacy-safe request history to the durable store |
| `AUDIT_DB_PATH` | `audit.db` | no | SQLite file path; put it on a persistent volume in production |
| `AUDIT_RETENTION` | `720h` | no | How long request history is kept before automatic purge |
| `AUDIT_BUFFER_SIZE` | `4096` | no | Bounded in-memory write buffer; records drop when full |
| `AUDIT_BATCH_SIZE` | `128` | no | Records per batched insert |
| `AUDIT_FLUSH_INTERVAL` | `1s` | no | Maximum time a record waits before it is written |
| `AUDIT_ADMIN_KEYS` | | no | Comma-separated keys for `/admin/usage`; empty disables the endpoint |
| `APPLICATIONINSIGHTS_CONNECTION_STRING` | | no | Passed through for Application Insights, exporter not wired yet |

Production requires `LINKEDIN_LI_AT`, `LINKEDIN_JSESSIONID`, and `API_KEYS`.

## Session cookies

`LINKEDIN_LI_AT` and `LINKEDIN_JSESSIONID` come from a browser session:

1. Sign in to LinkedIn in a browser.
2. Open developer tools, then Application, Cookies, `https://www.linkedin.com`.
3. Copy the `li_at` and `JSESSIONID` values.

Treat both as passwords. Keep them in `.env` locally or in your deployment's
secret store, and never commit them. They expire, so refresh them when upstream
authentication starts returning `upstream_auth_failed`. How they are used is in
[reverse-engineering.md](reverse-engineering.md#authentication).

## Caller-supplied sessions

When `LINKEDIN_ALLOW_CALLER_SESSION` is true, a caller may send its own `li_at`
and `JSESSIONID` through request headers to use its own authorized session for a
single request. The values are request-scoped, never stored, and never replace
the server session. See [api.md](api.md) for usage and [security.md](security.md)
for the isolation guarantees. Set it to `false` to reject caller sessions.
