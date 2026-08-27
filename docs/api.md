# API

The production base URL is
`https://linkedinapi-app.nicegrass-014c577e.eastus.azurecontainerapps.io`, or
`http://localhost:8080` locally. All responses are JSON.

## Authentication

The profile API is public and requires no API key. `X-LinkedIn-Li-At`,
`X-LinkedIn-JSESSIONID`, and optional `X-LinkedIn-User-Agent` may provide a
request-scoped LinkedIn session as described below.

See [configuration.md](configuration.md) and [security.md](security.md).

## GET /v1/profile

Fetches and normalizes a profile.

Query parameters:

- `url` (required): a profile URL of the form
  `https://www.linkedin.com/in/{identifier}`. Locale and mobile subdomains, a
  trailing slash, and query strings are accepted and normalized. Hosts outside
  `linkedin.com` are rejected.

Example:

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates"
```

### Optional caller-supplied session

By default the request uses the server-configured LinkedIn session. A caller may
instead supply its own session for a single request through two headers:

- `X-LinkedIn-Li-At`: the `li_at` cookie value
- `X-LinkedIn-JSESSIONID`: the `JSESSIONID` cookie value
- `X-LinkedIn-User-Agent` (optional): the browser User-Agent of that session, so the request matches the browser that created the cookies

Both cookie headers are required together; supplying only one is a `400`. When both are present
the request uses only the caller session and never falls back to the server
session. Caller sessions are not served from or written to the shared cache, are
never stored or logged, and are used for that one request only. The feature can be
turned off with `LINKEDIN_ALLOW_CALLER_SESSION=false`.

Load the cookies from environment variables rather than typing them into shell
history:

```bash
export LI_AT="your_li_at_cookie"
export JSESSIONID="ajax:your_jsessionid"
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates" \
  -H "X-LinkedIn-Li-At: $LI_AT" \
  -H "X-LinkedIn-JSESSIONID: $JSESSIONID" \
  -H "X-LinkedIn-User-Agent: $LINKEDIN_UA"
```

If the supplied session is rejected or expired, the response is `401` with code
`caller_session_invalid`. That session is then fast-failed for a short window with
no retries and no fallback; supply a fresh session to try again.

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
    "profile_language": "en_US",
    "supported_locales": ["en_US"],
    "location": { "country_code": "US" },
    "profile_picture": {
      "url": "https://media.licdn.com/dms/image/v2/example/800_800/pic.jpg",
      "variants": [
        { "width": 100, "height": 100, "url": "https://media.licdn.com/dms/image/v2/example/100_100/pic.jpg" },
        { "width": 400, "height": 400, "url": "https://media.licdn.com/dms/image/v2/example/400_400/pic.jpg" },
        { "width": 800, "height": 800, "url": "https://media.licdn.com/dms/image/v2/example/800_800/pic.jpg" }
      ]
    },
    "background_image": { "url": "https://media.licdn.com/dms/image/v2/example/1400_350/cover.jpg" },
    "websites": [ { "url": "https://gatesnot.es/sourcecode-li", "category": "BLOG" } ],
    "creator_website": "https://gatesnot.es/AI",
    "topics": ["books", "climatechange", "healthcare", "innovation", "sustainability"],
    "verified": true,
    "influencer": true,
    "premium": true,
    "creator": true,
    "top_voice": true,
    "created_at": "2013-05-02T19:09:40Z",
    "experience": [
      {
        "title": "Co-chair",
        "company": "Gates Foundation",
        "company_url": "https://www.linkedin.com/company/8736/",
        "date_range": { "start": { "year": 2000 } }
      }
    ],
    "education": [
      {
        "school": "Harvard University",
        "school_url": "https://www.linkedin.com/school/18483/",
        "date_range": { "start": { "year": 1973 }, "end": { "year": 1975 } }
      }
    ]
  },
  "meta": {
    "retrieved_at": "2026-08-27T09:30:00Z",
    "schema_version": "2.2",
    "source": "linkedin",
    "cached": false,
    "sections": { "experience": "ok", "education": "ok" }
  }
}
```

### Status codes

| Status | Code | Meaning |
| --- | --- | --- |
| 200 | | Profile returned |
| 400 | `invalid_request` | Missing or invalid URL, or an incomplete caller session |
| 401 | `unauthorized`, `caller_session_invalid` | Missing or invalid API key, or a supplied caller session was rejected or expired |
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
  omitted when absent, and arrays are omitted when empty.
- `profile_language` is the profile's primary locale, such as `en_US`.
- `supported_locales` lists every locale the profile publishes content in.
- `location` exposes `country_code`, and `text` when a full place name is
  available.
- `profile_picture` and `background_image` expose `url` (the largest rendition)
  and `variants`, every sized rendition ordered smallest to largest.
- `websites` are member-published links; `creator_website` and `topics` come from
  the member's creator profile when present.
- `verified`, `influencer`, `premium`, `creator`, `top_voice`, `student`, and
  `memorialized` appear only when true.
- `created_at` is when LinkedIn created the profile, when the source provides it.
- `experience`, `education`, `skills`, `certifications`, `languages`,
  `volunteer_experience`, `projects`, and `test_scores` are optional sections.
  Experience and education are returned by default; the rest are enabled with
  `PROFILE_SECTIONS`. Each entry carries only the fields LinkedIn provides, such
  as titles, organizations with `company_url`/`school_url`, a typed `date_range`,
  and descriptions.
- `meta.sections` reports the retrieval status of each attempted section as `ok`,
  `empty`, or `unavailable`.
- `meta.cached` is true when the result came from the cache. `meta.source` is
  `linkedin`, and `meta.schema_version` tracks the response contract.

Detailed sections are fetched from LinkedIn's DASH section endpoints after the
core profile, bounded and failure-isolated, and enabled through `PROFILE_SECTIONS`.
The rationale and endpoint details are in
[reverse-engineering.md](reverse-engineering.md).

## GET /v1/image

Proxies a profile image so the browser UI can render it; LinkedIn media URLs are
not directly loadable from another origin. The endpoint sits behind the same
authentication and rate limiting as `/v1/profile`.

Query parameters:

- `url` (required): an `https://media.licdn.com/...` image URL, exactly as
  returned in `profile_picture` or `background_image`. Any other host or scheme
  is rejected with `400`.

The response is the image bytes with the upstream content type, capped at 8 MiB,
with `Cache-Control: private, max-age=3600`.

## Health and metrics

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Liveness. Returns `200 {"status":"ok"}` and never calls LinkedIn. |
| `GET /readyz` | Readiness. `200` when serving, `503` while draining during shutdown. |
| `GET /metrics` | Prometheus metrics when `METRICS_ENABLED` is true. See [observability.md](observability.md). |
| `GET /` | The testing UI. |
