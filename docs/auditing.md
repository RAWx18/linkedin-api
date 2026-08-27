# Request auditing and usage tracking

The service records privacy-safe metadata about every public API request in a
durable store. This is separate from application logs and from Prometheus
metrics: logs are ephemeral and unstructured for querying, metrics answer "how is
the system behaving now", and the audit store answers "what exactly happened" so
operators can investigate abuse, measure LinkedIn upstream load, and study usage.

## What is recorded

One row is written per request to `/v1/profile`, including requests rejected by
rate limiting or authentication. Each row holds:

| Column | Meaning |
| --- | --- |
| `ts` | Request time, Unix milliseconds UTC |
| `request_id` | Correlates the row with logs and the client's `X-Request-ID` |
| `client_ip` | Remote address, from the connection not a spoofable header |
| `key_id` | Non-reversible fingerprint of the API key, or `anonymous` |
| `profile_id` | Normalized LinkedIn public identifier, empty if never parsed |
| `credential_mode` | `server_session` or `caller_session`, empty if not evaluated |
| `cred_fp` | Non-reversible fingerprint of a caller session (`cs_...`), empty for the server session |
| `status` | Final HTTP status |
| `rate_decision` | `allowed`, `ip_limited`, or `key_limited` |
| `cached` | Whether the response was served from cache |
| `upstream_called` | Whether a LinkedIn request was actually made |
| `upstream_outcome` | `ok`, `profile_not_found`, or an `upstream_*` failure code |
| `retries` | Upstream retry attempts for the request |
| `latency_ms` | End-to-end handling time |
| `error_class` | Final client-facing error code, empty on success |

## Privacy and credentials

The store is designed to hold no secrets and the minimum identifying data needed
to investigate abuse:

- No API keys, `li_at`, `JSESSIONID`, authorization headers, or cookies are ever
  written. The API key is reduced to a one-way SHA-256 fingerprint (`key_id`) so
  traffic can be grouped by caller without the key being recoverable.
- Caller-supplied `li_at` and `JSESSIONID` are never written either. When a caller
  uses its own session, only the `credential_mode` and a non-reversible keyed
  fingerprint (`cred_fp`, `cs_...`) are stored, so caller activity can be grouped
  without the cookies being recoverable.
- The full request URL is not stored. Only the normalized public identifier is
  kept, which is the part already present in a LinkedIn profile URL and the only
  part needed to answer "which profiles are requested most". Query strings and
  tracking parameters are discarded during URL validation, before auditing.
- `client_ip` is retained because investigating and blocking abuse requires it.
  Retention bounds how long it lives (see below).

## Persistence choice

The backend is SQLite through the pure-Go `modernc.org/sqlite` driver, so it needs
no cgo and keeps the `CGO_ENABLED=0` distroless build. It was chosen over a
managed database because it is simple, cheap, secure, and a good fit for the
single-replica deployment: there is no separate server to run or secure, no
network dependency, and no new remote failure point in front of profile lookups.

A single background goroutine performs every write, so the store is never
contended. In Azure the database file lives on a mounted Azure Files volume so it
survives restarts and redeploys; the single-replica design (see
[deployment.md](deployment.md)) guarantees a single writer, which is what keeps
SQLite safe on a network-mounted volume. Scaling out would require moving the
store to a shared database.

Operational counters stay in Prometheus. The database is deliberately not used as
a metrics system: it stores durable history and serves occasional aggregate
queries, not high-frequency counters.

## Performance and failure isolation

Auditing must never slow down or break a profile lookup:

- Recording is a non-blocking enqueue onto a bounded buffer. If a burst fills the
  buffer, records are dropped and counted in `audit_events_dropped_total` rather
  than blocking the request. This also means auditing cannot be abused to exhaust
  memory.
- Writes are batched and flushed by size or on a short interval from the single
  writer goroutine.
- If the store is unavailable, the failure is logged and counted in
  `audit_write_errors_total`; the request path is unaffected. If the store cannot
  be opened at startup the service still runs, simply without auditing.
- Buffered records are flushed on graceful shutdown.

## Retention

`AUDIT_RETENTION` (default 30 days) bounds how long history, including client IP
data, is kept. A background purge runs at startup and hourly, deleting rows older
than the window with an indexed range delete so the store cannot grow without
limit.

## Schema and indexes

The table is created on open. Indexes keep every usage query bounded to a scan of
the requested time window:

- `idx_audit_ts` on `ts` for windowed scans and retention deletes.
- `idx_audit_profile` on `(profile_id, ts)`.
- `idx_audit_client` on `(client_ip, ts)`.
- `idx_audit_key` on `(key_id, ts)`.

## Usage endpoint

`GET /admin/usage` returns aggregated usage for a time window. It is exposed only
when `AUDIT_ADMIN_KEYS` is set and is protected by those keys, separate from the
public `API_KEYS`. The window and result limit are clamped (30 days and 100 rows)
so the endpoint stays cheap and cannot be turned into an expensive scan.

Query parameters: `window` (a duration such as `24h`, default `24h`) and `limit`
(top-N size, default `10`).

```
curl -H "X-API-Key: $ADMIN_KEY" "https://host/admin/usage?window=24h&limit=10"
```

The response answers the operational questions directly:

- `summary.total_requests`, `rate_limited`, `auth_failures`, `not_found`,
  `server_errors`: traffic and rejection counts.
- `summary.cache_hits` and `cache_misses`: cache effectiveness.
- `summary.upstream_requests` and `upstream_outcomes`: how much traffic reached
  LinkedIn and the breakdown of 429, auth, and unavailable outcomes.
- `summary.avg_latency_ms` and `p95_latency_ms`: latency trend.
- `top_profiles`: most frequently requested profiles.
- `top_clients`: client IPs generating the most traffic.
- `top_upstream_clients`: client IPs driving the most LinkedIn requests, the
  signal for a caller attempting to generate excessive upstream traffic.
