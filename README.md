# linkedin-api

Go service that fetches a public LinkedIn profile and returns it as normalized
JSON. It calls LinkedIn's internal Voyager API with your own authenticated
session and exposes a small versioned HTTP API plus a minimal web UI for testing.

It uses only legitimate authenticated access with your own account. It does not
bypass authentication, CAPTCHAs, or rate limits, and it respects upstream limits.

## Stack

Go 1.25 with the standard library HTTP stack, Prometheus for metrics, a small Vite
and TypeScript UI embedded in the binary, Docker, and Azure Container Apps for
deployment.

## Architecture

Requests pass through thin layers. The API layer validates input and maps errors,
the service coordinates the lookup and the cache, the LinkedIn client makes the
authenticated HTTP calls, and the parser turns raw Voyager JSON into a stable
domain model. Details are in [docs/architecture.md](docs/architecture.md).

## Prerequisites

- Go 1.25 or newer
- Node 22 or newer, only to rebuild the UI
- A LinkedIn account, for the session cookies the upstream calls need

## Quick start

```bash
cp .env.example .env
# set LINKEDIN_LI_AT and LINKEDIN_JSESSIONID in .env
set -a && source .env && set +a
make run
```

Then call it:

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates"
```

The UI is at http://localhost:8080/. Getting the two cookie values is described in
[docs/configuration.md](docs/configuration.md#session-cookies).

## API

One main endpoint:

```
GET /v1/profile?url=<linkedin profile url>
```

It returns `{ "data": <profile>, "meta": <metadata> }`. When `API_KEYS` is set,
pass a key with the `X-API-Key` header. Health is at `/healthz` and `/readyz`, and
metrics at `/metrics`. Every request is recorded to a privacy-safe audit store; a
protected `GET /admin/usage` exposes usage aggregates when `AUDIT_ADMIN_KEYS` is
set. Request and response details, the schema, and error codes are in
[docs/api.md](docs/api.md).

## Build and test

```bash
make test        # run the tests
make test-race   # tests with the race detector
make build       # static binary into bin/
make docker-build
```

More targets are in [docs/development.md](docs/development.md).

## Documentation

- [Architecture](docs/architecture.md)
- [LinkedIn integration](docs/reverse-engineering.md)
- [API reference](docs/api.md)
- [Configuration](docs/configuration.md)
- [Development](docs/development.md)
- [Deployment](docs/deployment.md)
- [Observability](docs/observability.md)
- [Auditing and usage](docs/auditing.md)
- [Security](docs/security.md)
- [Limitations](docs/limitations.md)

## Known limitations

- **Upstream volatility:** the internal API is undocumented; endpoint shapes can
  change and temporarily reduce returned fields until the parser is updated.
- **Profile coverage:** the response covers the member's identity and top-card
  fields (name, headline, summary, location, pictures, websites, and status
  badges), and reflects what the configured session can see.
- **Rate limits:** LinkedIn rate limiting is respected, not circumvented; `429`
  responses are surfaced with `Retry-After` and are not retried.
- **Session lifetime:** `li_at`/`JSESSIONID` cookies expire; refresh them when
  upstream authentication starts failing (`upstream_auth_failed`).
- **Single profile scope:** only profile retrieval is implemented (no company,
  job, or search endpoints), by design.
