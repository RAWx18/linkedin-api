# LinkedIn integration

LinkedIn's web client talks to an internal JSON API under
`https://www.linkedin.com/voyager/api/`. This service authenticates with a
signed-in session and reads a member's profile from the identity DASH resources:
one finder for the top-card, and one resource per optional section.

## Core endpoint

| Purpose | Endpoint |
| --- | --- |
| Top-card by public identifier | `identity/dash/profiles?q=memberIdentity&memberIdentity={publicId}` |

The finder returns a collection whose first element is the member entity. An empty
collection means the profile does not exist and maps to a not-found result.

## Authentication

The API expects a signed-in browser session, provided by two cookie values from a
normal login:

- `LINKEDIN_LI_AT`, the `li_at` session cookie.
- `LINKEDIN_JSESSIONID`, the `JSESSIONID` value, which looks like `ajax:1234...`.

Each request sends both as cookies and the unquoted `JSESSIONID` as the
`Csrf-Token` header, matching the web client. The session is built once and never
mutated, so concurrent requests cannot corrupt shared auth state. Obtaining the
values is covered in [session-cookies.md](session-cookies.md).

## Session compatibility

A LinkedIn session is bound to the browser that created it, and the strongest
signal LinkedIn uses to decide whether a request belongs to that session is the
`User-Agent`. If the service presents a different `User-Agent` than the browser
where the cookies were obtained, LinkedIn can accept the first request and then
challenge and invalidate the session, so the next request fails with
`upstream_auth_failed`. That is the "works once, then fails" symptom.

The remedy is to present the same browser context, not to evade detection. The
service requires `LINKEDIN_USER_AGENT` to equal the exact browser `User-Agent`
whenever a session is configured, and refuses to start otherwise. The header set
is fixed, so identical inputs always produce the same request:

| Header | Value |
| --- | --- |
| `User-Agent` | `LINKEDIN_USER_AGENT`, or a caller's `X-LinkedIn-User-Agent` |
| `Accept` | `application/json` for the top-card, `application/vnd.linkedin.normalized+json+2.1` for sections |
| `Accept-Language` | `LINKEDIN_ACCEPT_LANGUAGE` (default `en-US,en;q=0.9`) |
| `X-RestLi-Protocol-Version` | `2.0.0` |
| `X-Li-Lang` | `en_US` |
| `Cookie` | `li_at=...; JSESSIONID="ajax:..."` |
| `Csrf-Token` | the unquoted `JSESSIONID` |

### Verifying the request matches your browser

The request can be confirmed against a real browser without exposing any secret:

1. In the browser signed in to LinkedIn, open a profile, then devtools, Network.
   Filter for `voyager/api/identity/dash/profiles` and open the request.
2. Copy the exact `user-agent` request header into `LINKEDIN_USER_AGENT`.
3. Start the service with `LOG_LEVEL=debug`. Each upstream call logs a
   `linkedin upstream request` line with non-sensitive metadata: `user_agent`,
   `accept`, `accept_language`, the cookie names, and whether a CSRF token is
   present. Cookie values, `li_at`, `JSESSIONID`, and the CSRF token are never
   logged.
4. Compare the logged `user_agent` and header names against the browser. Two
   lookups produce identical request lines.

A caller supplying its own session can also send the matching `X-LinkedIn-User-Agent`
so its session context stays consistent for that one request; it never changes the
server `User-Agent`.

## Top-card normalization

The member entity is the identity top-card. The parser reads the stable,
legitimately available fields and ignores the rest of the payload:

- Name (`firstName`, `lastName`), `headline`, `summary`, and the primary locale
  (`primaryLocale` becomes `profile_language`, such as `en_US`), plus every
  `supportedLocale`.
- Profile and background pictures, each a `displayImage.vectorImage` with a root
  URL and sized artifacts. Every artifact becomes a variant (width, height, url)
  ordered smallest to largest, and the largest is also the primary `url`.
- `location.countryCode`, published `websites`, the creator's featured website
  (`creatorInfo.creatorWebsite`), and creator topics
  (`creatorInfo.associatedHashtagUrns`, decoded to hashtag names).
- Status flags: `verified`, `influencer`, `premium`, `creator`, `student`,
  `memorialized`, and `top_voice` (from the presence of `topVoiceBadge`).
- `created`, the profile creation time, when present.

Text fields are HTML-unescaped, because LinkedIn encodes entities such as `&amp;`
in the raw values. Optional fields are omitted when absent; the parser never
invents values. The public identifier and canonical URL come from the validated
request, never from the response body, so the response can never redirect the
service to another identity.

## Profile sections

The `q=memberIdentity` finder returns the top-card only. Detailed sections are
retrieved separately, after the core profile, from DASH resources keyed by the
profile URN (`entityUrn`) the top-card already returns:

    GET /voyager/api/identity/dash/{resource}?q=viewee&profileUrn={urn}

with `Accept: application/vnd.linkedin.normalized+json+2.1`, which returns a
normalized `data`/`included` document. The `data["*elements"]` array holds the
ordered entity URNs, each resolved from `included`, so LinkedIn's ordering is
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

Each call is one deterministic request using the same session and headers as the
core call. Company and school URLs are derived from the company and school URNs on
each entity. Experience and education are fetched by default; the rest are enabled
through `PROFILE_SECTIONS`. Enrichment is optional and failure-isolated: each
section runs through the shared upstream gate (rate limit, breaker, concurrency),
bounded by a global semaphore (`ENRICHMENT_CONCURRENCY`) and the overall
`PROFILE_TIMEOUT`. Any error, timeout, or empty result is recorded in
`meta.sections` as `ok`, `empty`, or `unavailable`, and never affects the core
profile that was already assembled.

## Timeouts and retries

Each upstream call runs under `HTTP_REQUEST_TIMEOUT`, which spans its attempts,
and the whole lookup runs under `PROFILE_TIMEOUT`. Retries apply only to timeouts,
connection failures, and 5xx responses, up to `HTTP_MAX_RETRIES`, with exponential
backoff and jitter. Authentication failures, login redirects, 404s, and 429s are
never retried. A 429 is surfaced with its `Retry-After` value.

## Session health

Using one signed-in session from a server is inherently more fragile than a
browser, because LinkedIn can challenge or invalidate it. The client treats
authentication and login-redirect responses as a likely session invalidation
rather than a routine error. After a small number of them a session breaker marks
the session unhealthy, stops issuing upstream requests, and stays open for a
cooldown that doubles on each failed probe, so a dead session is not hammered.
While the session is unhealthy, callers receive a controlled `502` or `503`.
`upstream_session_healthy` exposes the state, and it clears automatically once a
probe succeeds after the cookies are refreshed.

Matching the browser `User-Agent` removes the most common cause of invalidation.
It cannot guarantee a session is never challenged: a server also differs from a
browser in its network origin, and LinkedIn may still challenge a session used
from a datacenter address. The service does not work around that. When it happens
the session breaker detects it, stops further traffic, and surfaces the state, so
the failure stays contained and is recoverable by refreshing the cookies.

## Volatility

These endpoints are internal and undocumented, so their paths and shapes can
change without notice. The parser is defensive, but an upstream change can still
reduce the fields returned until the mapping is updated. That risk is confined to
the `linkedin` and `linkedin/parse` packages.
