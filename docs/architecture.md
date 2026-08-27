# Architecture

The service is split into layers with one-directional dependencies. Each layer
knows only about the ones beneath it, so changes stay local: a shift in
LinkedIn's response format stays inside the parser, an HTTP concern stays inside
the API layer, and the domain model does not move because of either.

```
HTTP request
   │
   ▼
 api/            validation, auth, rate limiting, error mapping, JSON I/O
   │
   ▼
 service/        orchestration: cache lookup, fetch, parse, cache store
   │
   ├───► cache/  bounded in-memory TTL cache
   │
   ▼
 linkedin/       Voyager HTTP client: auth headers, timeouts, retries
   │
   ▼
 linkedin/parse/ raw Voyager JSON to domain model
   │
   ▼
 domain/         stable models and typed errors
```

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/server` | Entrypoint: load config, build dependencies, run the server, handle graceful shutdown. |
| `internal/api` | Routing, middleware (request ID, logging, metrics, recovery, rate limit, API key, audit), error to HTTP mapping, JSON responses. |
| `internal/service` | Coordinates a lookup: cache, coalesce, gate, fetch, parse, cache. Declares the interfaces it depends on. |
| `internal/upstream` | The gate in front of LinkedIn: aggregate rate limit, concurrency ceiling, and circuit breaker. |
| `internal/audit` | Durable, privacy-safe request history: async batched writer, SQLite store, retention, and usage queries. |
| `internal/linkedin` | Voyager HTTP client, session, endpoint builders, upstream error classification, bounded retries. |
| `internal/linkedin/parse` | Turns raw Voyager JSON into the domain model. Defensive against missing and malformed fields. |
| `internal/domain` | Profile models and the structured error type. No transport or HTTP concerns. |
| `internal/cache` | Concurrency-safe TTL cache with a size cap. |
| `internal/config` | Environment configuration, validation, and secret-safe logging. |
| `internal/observability` | slog logger and Prometheus metrics. |
| `internal/urlx` | LinkedIn URL validation, normalization, and the host allowlist. |
| `web` | Minimal Vite and TypeScript UI, embedded into the binary. |

## Request lifecycle

A call to `GET /v1/profile` moves through these steps:

1. Middleware assigns a request ID and records timing and outcome. On `/v1/*` the
   request also passes rate limiting and API key checks.
2. The handler validates the `url` parameter and extracts the public identifier.
   The raw URL is never fetched.
3. The service checks the cache. A hit returns immediately with `cached` set, and
   a profile known to be missing is refused without any upstream call.
4. On a miss it coalesces concurrent identical lookups, then passes the retrieval
   through the upstream gate (aggregate rate limit, concurrency ceiling, circuit
   breaker). If the gate rejects, no upstream call is made.
5. Under a lookup deadline it fetches the top-card from the identity DASH finder.
6. The parser normalizes the response into a `domain.Profile`; an empty
   collection is treated as not found.
7. Any configured optional sections are fetched concurrently under the same
   deadline, merged into the profile, and each recorded in `meta.sections`. A
   failed or empty section never fails the core profile.
8. The result is cached and serialized as `{ "data": ..., "meta": ... }`.

The outermost `/v1/*` layer records one privacy-safe audit row per request,
including rejected ones, without delaying the response. See
[auditing.md](auditing.md).

Failures become a structured `domain.Error` that the API layer maps to an HTTP
status. The status table is in [api.md](api.md). The upstream protections are
described in [security.md](security.md).

## Why this shape

The boundaries let each concern change on its own. The service depends on
interfaces rather than concrete types, so tests drive it with a mock client and a
real or empty cache, without a network or a LinkedIn account. The LinkedIn client
returns `domain.Error` values directly, so the API layer maps from a single error
taxonomy. Parsing is isolated from transport, which keeps the brittle part of the
system, the mapping of undocumented JSON, in one place. That mapping is described
in [reverse-engineering.md](reverse-engineering.md).

## Repository layout

```
cmd/server/            entrypoint and graceful shutdown
internal/
  api/                 router, handlers, middleware, error mapping
  audit/               durable request history and usage queries
  cache/               in-memory TTL cache
  config/              configuration and validation
  domain/              profile models and errors
  linkedin/            Voyager client, session, endpoints
  linkedin/parse/      normalization and test fixtures
  observability/       logging and metrics
  service/             orchestration
  upstream/            rate limit, concurrency cap, circuit breaker
  urlx/                URL validation and allowlist
web/                   Vite and TypeScript UI (embedded)
deploy/                Dockerfile, docker-compose, Bicep
api/openapi.yaml       OpenAPI 3.0 contract
docs/                  detailed documentation
```
