# Limitations

- The internal profile API is undocumented. Endpoint shapes can change and reduce
  the fields returned until the parser is updated. See
  [reverse-engineering.md](reverse-engineering.md).
- The response covers the member's identity and top-card fields and reflects what
  the configured session can see.
- LinkedIn rate limiting is respected, not worked around. A `429` is surfaced with
  `Retry-After` and is not retried.
- Session cookies expire. Refresh `LINKEDIN_LI_AT` and `LINKEDIN_JSESSIONID` when
  requests start failing with `upstream_auth_failed`.
- A session is bound to the browser that created it. `LINKEDIN_USER_AGENT` must be
  the exact browser User-Agent of that session, or LinkedIn can invalidate it after
  the first request. Matching it removes the common cause, but a session used from a
  datacenter address may still be challenged; the service detects and contains this
  rather than evading it. See [reverse-engineering.md](reverse-engineering.md#session-compatibility).
- Only profile retrieval is implemented. There are no company, job, or search
  endpoints.
- Rate limiting keys on the connection's remote address. Behind a proxy that
  terminates client connections, per-IP limiting sees the proxy address unless the
  proxy preserves the client IP.
- The rate, concurrency, and circuit-breaker limits and the audit store are per
  instance. The deployment runs a single replica by default so they stay
  authoritative for the one shared LinkedIn session; scaling out multiplies
  upstream pressure and splits the audit trail across replicas.
- `APPLICATIONINSIGHTS_CONNECTION_STRING` is accepted and provisioned, but the app
  does not export to Application Insights yet. Azure telemetry today is the
  structured logs in Log Analytics.
