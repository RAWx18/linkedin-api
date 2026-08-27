# Limitations

- The internal profile API is undocumented. Endpoint shapes can change and reduce
  the fields returned until the parser is updated. See
  [reverse-engineering.md](reverse-engineering.md).
- The response covers the member's identity and top-card fields and reflects what
  the configured session can see. See [Profile section coverage](#profile-section-coverage)
  for a section-by-section audit.
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

## Profile section coverage

Each lookup issues one upstream request,
`GET /voyager/api/identity/dash/profiles?q=memberIdentity`, which returns the
profile top-card. The table maps each profile section to that response.

| Section | Status | Source |
| --- | --- | --- |
| Identity: name, headline, summary, public id, canonical url | Retrieved | top-card |
| Profile and background images, with sized variants | Retrieved | top-card |
| Profile language, country code | Retrieved | top-card (`primaryLocale`, `location`) |
| Websites, creator website, creator topics | Retrieved | top-card |
| Flags: verified, influencer, premium, creator, top_voice, student, memorialized | Retrieved | top-card |
| Experience | Unavailable | separate card request (`experienceCardUrn`) |
| Education | Unavailable | separate card request (`educationCardUrn`) |
| Skills, certifications, languages, projects, publications, honors and awards, volunteer, courses, organizations, test scores, causes, interests | Unavailable | separate profile-cards request (other card types) |
| Recommendations | Unavailable | separate recommendations request |
| Contact info: email, phone, twitter | Unavailable | `twitterHandles` exists on the top-card but is empty for the audited profile; the rest needs the contact-info request |
| Follower and connection counts | Not exposed | the top-card carries only a `showFollowerCount` flag, no count value |
| Industry name, human-readable location | Not resolvable | the top-card carries only opaque urns (`industryUrn`, `geoLocation.geoUrn`) with no names |

Sections marked unavailable are not present in the top-card response. Retrieving
them requires additional Voyager card requests keyed by the card urns, which use
versioned internal decoration and GraphQL query identifiers. The service does not
issue those requests or fabricate the sections. See
[reverse-engineering.md](reverse-engineering.md) for the bounded, concurrent,
failure-tolerant enrichment design that would add them once real sanitized
fixtures for each card are captured.
