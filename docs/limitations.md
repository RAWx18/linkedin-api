# Limitations

- The internal profile API is undocumented. Endpoint shapes can change and reduce
  the fields returned until the parser is updated. See
  [reverse-engineering.md](reverse-engineering.md).
- The response covers the member's identity and top-card fields, plus optional
  experience, education, and other sections, and reflects what the configured
  session can see. See [Profile section coverage](#profile-section-coverage) for a
  section-by-section audit.
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
- Rate limiting keys on the connection's remote address by default. Behind a
  reverse proxy, set `TRUSTED_PROXY_DEPTH` to the number of trusted hops so the
  client IP is read from `X-Forwarded-For`; with the default of `0` per-IP
  limiting sees the proxy address.
- The rate, concurrency, and circuit-breaker limits and the audit store are per
  instance. The deployment runs a single replica by default so they stay
  authoritative for the one shared LinkedIn session; scaling out multiplies
  upstream pressure and splits the audit trail across replicas.

## Profile section coverage

Each lookup issues one core request,
`GET /voyager/api/identity/dash/profiles?q=memberIdentity` (the top-card), then
optional per-section DASH requests (see
[reverse-engineering.md](reverse-engineering.md)). The table maps each section to
how it is retrieved.

| Section | Status | Source |
| --- | --- | --- |
| Identity: name, headline, summary, public id, canonical url | Retrieved | top-card |
| Profile and background images, with sized variants | Retrieved | top-card |
| Profile language, supported locales, country code, created time | Retrieved | top-card |
| Websites, creator website, creator topics | Retrieved | top-card |
| Flags: verified, influencer, premium, creator, top_voice, student, memorialized | Retrieved | top-card |
| Experience, Education | Retrieved (default) | DASH `profilePositions`, `profileEducations` |
| Skills, Certifications, Languages, Volunteer, Projects, Test scores | Retrieved (opt-in) | DASH section endpoints, enabled with `PROFILE_SECTIONS` |
| Recommendations, Honors, Publications, Courses, Organizations, Patents, Interests, Causes | Not modeled | DASH endpoints exist but were not verified with populated responses, so they are not parsed |
| Contact info: email, phone, twitter | Not exposed | needs the contact-info request; not implemented |
| Follower and connection counts | Not exposed | the top-card carries only a `showFollowerCount` flag, no count value |
| Industry name, human-readable location | Not resolvable | the top-card carries only opaque urns with no names |

Optional sections are failure-isolated: `meta.sections` reports each attempted
section as `ok`, `empty`, or `unavailable`, and any failure leaves the core
profile intact. The parser never fabricates values.
