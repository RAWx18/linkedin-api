# linkedin-api

A Go service that reads a public LinkedIn profile and returns it as normalized
JSON. It calls LinkedIn's internal Voyager API with your own signed-in session,
normalizes the response into a stable model, and serves it over a small versioned
HTTP API with a minimal web UI for testing.

Access is authenticated with your own account only. The service does not bypass
authentication, solve challenges, or evade rate limits, and it stays within the
limits LinkedIn returns.

## Stack

Go 1.25 and the standard library HTTP stack, Prometheus metrics, a small Vite and
TypeScript UI embedded in the binary, a distroless container image, and Azure
Container Apps for deployment.

## Prerequisites

- Go 1.25 or newer
- Node 22 or newer, only to rebuild the UI
- A LinkedIn account, for the session cookies the upstream calls use

## Quick start

```bash
cp .env.example .env
# set LINKEDIN_LI_AT, LINKEDIN_JSESSIONID, and LINKEDIN_USER_AGENT in .env
set -a && source .env && set +a
make run
```

Call it, or open the UI at http://localhost:8080/:

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates"
```

The three session values come from a browser signed in to LinkedIn; the steps are
in [docs/session-cookies.md](docs/session-cookies.md).

## API

The service exposes one profile endpoint:

```
GET /v1/profile?url=<linkedin profile url>
```

It returns `{ "data": <profile>, "meta": <metadata> }`.

Two credential types are involved and never mixed:

- `X-API-Key` authorizes the caller to this service. It is required for
  programmatic access when `API_KEYS` is set. The browser UI is same-origin and
  exempt, so the key never reaches the browser.
- `X-LinkedIn-Li-At` and `X-LinkedIn-JSESSIONID`, with optional
  `X-LinkedIn-User-Agent`, let a caller supply its own LinkedIn session for a
  single request. Without them the server session is used. They are
  request-scoped, isolated per caller, and never stored.

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates" \
  -H "X-API-Key: your-key"
```

Liveness and readiness are at `/healthz` and `/readyz`, and Prometheus metrics at
`/metrics`. Every request is recorded to a privacy-safe audit store, and a
protected `GET /admin/usage` returns usage aggregates when `AUDIT_ADMIN_KEYS` is
set. The full request and response contract is in [docs/api.md](docs/api.md) and
[api/openapi.yaml](api/openapi.yaml).

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
- [Session cookies](docs/session-cookies.md)
- [Development](docs/development.md)
- [Deployment](docs/deployment.md)
- [Observability](docs/observability.md)
- [Auditing and usage](docs/auditing.md)
- [Security](docs/security.md)
- [Limitations](docs/limitations.md)
