# LinkedIn integration

LinkedIn's web client talks to an internal JSON API under
`https://www.linkedin.com/voyager/api/`. The service authenticates with a
signed-in session and reads the profile from the identity DASH collection.

## Endpoint

| Purpose | Endpoint |
| --- | --- |
| Base profile by public identifier | `identity/dash/profiles?q=memberIdentity&memberIdentity={publicId}` |

The finder returns a collection whose first element is the member entity. An
empty collection means the profile does not exist.

## Authentication

The API expects a signed-in browser session. Two cookie values from a normal
login provide it:

- `LINKEDIN_LI_AT`, the `li_at` session cookie.
- `LINKEDIN_JSESSIONID`, the `JSESSIONID` value, which looks like `ajax:1234...`.

Each request sends both as cookies and the unquoted `JSESSIONID` as the
`csrf-token` header, matching what the web client does. The session object is
built once and never mutated, so concurrent requests cannot corrupt shared auth
state. Setting these values is covered in
[configuration.md](configuration.md#session-cookies).

## Session compatibility

A LinkedIn session is bound to the browser that created it. The strongest signal
LinkedIn uses to decide whether a request belongs to that session is the
`User-Agent`. If the server presents a different `User-Agent` than the browser
where the `li_at` and `JSESSIONID` cookies were obtained, LinkedIn can accept the
first request and then challenge and invalidate the session, so the next request
fails with `upstream_auth_failed`. This is the "works once, then fails" symptom.

The fix is not evasion but correctness: present the same browser context. The
service requires `LINKEDIN_USER_AGENT` to be set to the exact browser `User-Agent`
whenever a session is configured, and refuses to start otherwise. The request
header set is fixed and deterministic, so identical inputs always produce the same
request:

| Header | Value |
| --- | --- |
| `User-Agent` | `LINKEDIN_USER_AGENT`, or a caller's `X-LinkedIn-User-Agent` |
| `Accept` | `application/json` |
| `Accept-Language` | `LINKEDIN_ACCEPT_LANGUAGE` (default `en-US,en;q=0.9`) |
| `X-RestLi-Protocol-Version` | `2.0.0` |
| `X-Li-Lang` | `en_US` |
| `Cookie` | `li_at=...; JSESSIONID="ajax:..."` |
| `Csrf-Token` | the unquoted `JSESSIONID` |

`Accept` deliberately requests the non-normalized collection shape the parser
reads; it is not changed to the web client's normalized media type.

### Manual validation procedure

To confirm the Go request matches your browser without ever exposing secrets:

1. In the browser signed in to LinkedIn, open a profile, then devtools, Network.
   Filter for `voyager/api/identity/dash/profiles` and open the request.
2. Copy the exact `user-agent` request header and set `LINKEDIN_USER_AGENT` to it.
3. Start the service with `LOG_LEVEL=debug`. Each upstream call logs a
   `linkedin upstream request` line with non-sensitive request metadata
   (`user_agent`, `accept`, `accept_language`, cookie names, and whether a CSRF
   token is present). Cookie values, `li_at`, `JSESSIONID`, and the CSRF token are
   never logged.
4. Compare the logged `user_agent` and header names with the browser request. Two
   lookups produce identical request lines, confirming the deterministic model.

### Caller sessions

A caller supplying its own session can also supply the matching browser
`User-Agent` through the optional `X-LinkedIn-User-Agent` header, so its session
stays consistent. It applies only to that request and never changes the server
`User-Agent`.

## Response shape and normalization

The member entity is the identity top-card. The parser reads every stable,
legitimately available field and ignores the rest of the large payload:

- Name (`firstName`, `lastName`), `headline`, `summary`, and the primary locale
  (`primaryLocale` becomes `profile_language`, such as `en_US`).
- Profile and background pictures, each a `displayImage.vectorImage` with a root
  URL and sized artifacts. Every artifact becomes an image variant (width,
  height, url) ordered smallest to largest, and the largest is also the primary
  `url`.
- `location.countryCode`, published `websites`, the creator's featured website
  (`creatorInfo.creatorWebsite`), and the creator's associated topics
  (`creatorInfo.associatedHashtagUrns`, decoded to hashtag names).
- Status flags: `verified`, `influencer`, `premium`, `creator`, `student`,
  `memorialized`, and `top_voice` (from the presence of `topVoiceBadge`).

An empty collection maps to a not-found result. A body that cannot be decoded
maps to a parse error. Optional nested sections use pointers so a missing, null,
or partial section is skipped rather than failing the profile. Optional fields
are omitted when absent; the parser never invents values. Identity fields, the
public identifier and canonical URL, come from the validated request, not the
response body.

### Profile sections

The `q=memberIdentity` finder returns the top-card only. Detailed sections are
retrieved separately, after the core profile, from LinkedIn's DASH REST API keyed
by the profile URN (`entityUrn`) the top-card already returns:

    GET /voyager/api/identity/dash/profile{Section}?q=viewee&profileUrn={urn}

sent with `Accept: application/vnd.linkedin.normalized+json+2.1`, which returns a
normalized `data`/`included` document. The `data["*elements"]` array gives the
ordered entity URNs; each is resolved from `included`, so LinkedIn's ordering is
preserved. Supported sections and their resources:

| Section | Resource |
| --- | --- |
| `experience` | `profilePositions` |
| `education` | `profileEducations` |
| `skills` | `profileSkills` |
| `certifications` | `profileCertifications` |
| `languages` | `profileLanguages` |
| `volunteer` | `profileVolunteerExperiences` |
| `projects` | `profileProjects` |
| `test_scores` | `profileTestScores` |

These calls are deterministic, one request per section, and use the same
authenticated session and headers as the core call: no browser automation, no
server-driven UI, and no ephemeral page tokens. Experience and education are
fetched by default; the rest are enabled with `PROFILE_SECTIONS`. Enrichment is
optional and failure-isolated: each section runs through the shared upstream gate
(rate limit, breaker, concurrency), bounded by a global semaphore
(`ENRICHMENT_CONCURRENCY`) and the overall `PROFILE_TIMEOUT`. Any error, timeout,
or empty result is recorded in `meta.sections` (`ok`, `empty`, or `unavailable`)
without affecting the core profile. The parser never fabricates values, and
sections that would require server-driven UI or unstable tokens are not used.

## Timeouts and retries

Each upstream call runs under `HTTP_REQUEST_TIMEOUT`, which spans its attempts,
and the whole lookup runs under `PROFILE_TIMEOUT`. Retries apply only to timeouts,
connection failures, and 5xx responses, up to `HTTP_MAX_RETRIES`, with exponential
backoff and jitter. Authentication failures, login redirects, 404s, and 429s are
never retried. A 429 is surfaced with its `Retry-After` value.

## Session health

Using one signed-in session from a server is inherently more fragile than a
browser: LinkedIn can challenge or invalidate it. The client treats authentication
and login-redirect responses as a likely session invalidation rather than a
routine error. After a small number of them the circuit breaker marks the session
unhealthy, stops issuing upstream requests, and stays open for a long cooldown that
doubles on each failed probe, so a dead session is not hammered into deeper
trouble. While the session is unhealthy, callers receive a controlled `503`.
`upstream_session_healthy` exposes the state, and it clears automatically once a
probe succeeds after the cookies are refreshed. The service never tries to bypass a
challenge or login wall.

Matching the browser `User-Agent` and session context (above) removes the most
common cause of this invalidation. It cannot guarantee a session is never
challenged: a server also differs from a browser in its network origin, and
LinkedIn may still challenge a session used from a datacenter address. The service
does not attempt to work around that. When it happens the session-health breaker
detects it, stops further traffic, and surfaces the state, so the failure stays
contained and is recoverable by refreshing the cookies.

## Volatility

These endpoints are internal and undocumented, so their paths and shapes can
change without notice. The parser is defensive, but an upstream change can still
reduce the fields returned until the mapping is updated. That risk is confined to
the `linkedin` and `linkedin/parse` packages.
