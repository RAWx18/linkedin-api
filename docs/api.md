# API

The base URL is the deployed host, or `http://localhost:8080` locally. All
responses are JSON.

## Authentication

When `API_KEYS` is set, `GET /v1/*` requires a key. Send it as `X-API-Key: <key>`
or `Authorization: Bearer <key>`. With no keys configured the API is open, which
is meant for local development. See [configuration.md](configuration.md).

## GET /v1/profile

Fetches and normalizes a profile.

Query parameters:

- `url` (required): a profile URL of the form
  `https://www.linkedin.com/in/{identifier}`. Locale and mobile subdomains, a
  trailing slash, and query strings are accepted and normalized. Hosts outside
  `linkedin.com` are rejected.

Example:

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates" \
  -H "X-API-Key: your-key"
```

Response:

```json
{
  "data": {
    "public_identifier": "williamhgates",
    "profile_url": "https://www.linkedin.com/in/williamhgates",
    "first_name": "Bill",
    "last_name": "Gates",
    "full_name": "Bill Gates",
    "headline": "Chair, Gates Foundation and Founder, Breakthrough Energy",
    "summary": "Chair of the Gates Foundation. Founder of Breakthrough Energy.",
    "location": { "country_code": "US" },
    "profile_picture": { "url": "https://media.licdn.com/dms/image/v2/example/800_800/pic.jpg" },
    "background_image": { "url": "https://media.licdn.com/dms/image/v2/example/cover.jpg" },
    "websites": [ { "url": "https://gatesnot.es/sourcecode-li", "category": "BLOG" } ],
    "verified": true,
    "influencer": true,
    "premium": true
  },
  "meta": {
    "retrieved_at": "2026-08-27T09:30:00Z",
    "schema_version": "2.0",
    "source": "linkedin",
    "cached": false
  }
}
```

### Status codes

| Status | Code | Meaning |
| --- | --- | --- |
| 200 | | Profile returned |
| 400 | `invalid_request` | Missing or invalid URL |
| 401 | `unauthorized` | Missing or invalid API key |
| 404 | `profile_not_found` | Profile not found upstream |
| 429 | `rate_limited`, `upstream_rate_limited` | Local or upstream rate limit, see `Retry-After` |
| 502 | `upstream_auth_failed`, `upstream_parse_error` | Session rejected or response unreadable |
| 503 | `upstream_unavailable` | Transient upstream failure |
| 504 | `upstream_timeout` | Upstream timed out |
| 500 | `internal_error` | Unexpected failure |

Errors share one envelope:

```json
{ "error": { "code": "invalid_request", "message": "...", "request_id": "..." } }
```

## Response schema

The top level is `{ "data": <profile>, "meta": <metadata> }`. The complete schema
is in [openapi.yaml](../api/openapi.yaml). Notes:

- `public_identifier` and `profile_url` are always present. Other scalars are
  omitted when absent, and `websites` is omitted when empty.
- `location` exposes `country_code`, and `text` when a full place name is
  available.
- `verified`, `influencer`, and `premium` appear only when true.
- `meta.cached` is true when the result came from the cache. `meta.source` is
  `linkedin`, and `meta.schema_version` tracks the response contract.

## Health and metrics

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Liveness. Returns `200 {"status":"ok"}` and never calls LinkedIn. |
| `GET /readyz` | Readiness. `200` when serving, `503` while draining during shutdown. |
| `GET /metrics` | Prometheus metrics when `METRICS_ENABLED` is true. See [observability.md](observability.md). |
| `GET /` | The testing UI. |
