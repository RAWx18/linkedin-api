# Observability

## Logging

Logs are structured JSON written to stdout through `log/slog`. Each request logs a
`request_id`, method, route, status, duration, and client IP. Credentials,
cookies, tokens, and profile data are never logged, and the configuration is
logged through a `LogValue` that reports only non-sensitive fields such as whether
a session is present and how many API keys are set. Use `LOG_FORMAT=text` for
readable local output and `LOG_LEVEL=debug` for per-request upstream detail.

## Metrics

Prometheus metrics are served at `/metrics` when `METRICS_ENABLED` is true, on a
private registry that also includes Go runtime and process collectors.

| Metric | Type | Labels |
| --- | --- | --- |
| `http_requests_total` | counter | `route`, `method`, `status` |
| `http_request_duration_seconds` | histogram | `route` |
| `upstream_requests_total` | counter | `endpoint`, `status` |
| `upstream_request_duration_seconds` | histogram | `endpoint` |
| `upstream_retries_total` | counter | |
| `upstream_timeouts_total` | counter | |
| `upstream_rate_limited_total` | counter | |
| `upstream_auth_failures_total` | counter | |
| `parse_failures_total` | counter | |
| `profile_cache_total` | counter | `result` = hit, miss, negative |
| `profiles_retrieved_total` | counter | `result` = success, failure |
| `upstream_rejected_total` | counter | `reason` = circuit_open, rate, concurrency |
| `upstream_circuit_open` | gauge | |
| `upstream_circuit_trips_total` | counter | |
| `upstream_session_healthy` | gauge | |
| `caller_session_invalid_total` | counter | |
| `caller_sessions_unhealthy` | gauge | |
| `upstream_requests_coalesced_total` | counter | |
| `audit_events_written_total` | counter | |
| `audit_events_dropped_total` | counter | |
| `audit_write_errors_total` | counter | |

The `upstream_rejected_total` and `upstream_circuit_open` series are the ones to
alert on: a rising rejection rate or a circuit that stays open signals abuse or
that LinkedIn is restricting the session. `upstream_session_healthy` dropping to 0
is the strongest signal that the LinkedIn session has been challenged or
invalidated and needs new cookies; while it is 0 the service stops issuing
upstream requests and returns controlled `503`s. `upstream_session_healthy`
tracks the server-configured session only. Caller-supplied sessions are tracked
separately: `caller_session_invalid_total` counts caller sessions LinkedIn
rejected or that were fast-failed as already expired, and `caller_sessions_unhealthy`
is how many caller sessions are currently being fast-failed. A caller's bad
session never moves `upstream_session_healthy` or opens the shared circuit. A
non-zero `audit_events_dropped_total` means request volume is outrunning the audit
buffer, and `audit_write_errors_total` means the store is unhealthy; neither
affects profile lookups.

## Request history

Operational metrics answer "how is the system behaving right now" and live in
Prometheus. Durable, queryable request history answers "what exactly happened"
and lives in a separate SQLite audit store, not in the metrics system. Every
finished request is also emitted as a structured `audit` log event with the same
privacy-safe fields, so the platform log pipeline holds a complete history even
if the store is unavailable. It powers
the protected `/admin/usage` endpoint and incident investigation. Each row also
records the request's `credential_mode` (`server_session` or `caller_session`)
and, for caller sessions, a non-reversible `cred_fp` fingerprint, so an operator
can tell server-session from caller-session activity without ever seeing a
credential. The schema, privacy rules, retention, and example queries are in
[auditing.md](auditing.md).

## Azure

Container stdout is collected into a Log Analytics workspace by the Container Apps
environment, so the structured JSON logs are queryable there. Metrics are scraped
from `/metrics` in the standard Prometheus format by whatever collector the
platform provides.
