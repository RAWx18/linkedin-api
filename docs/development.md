# Development

## Prerequisites

- Go 1.25 or newer.
- Node 22 or newer, only to rebuild the UI.
- Docker, optional, for container builds.

## Setup and run

```bash
cp .env.example .env
# set LINKEDIN_LI_AT, LINKEDIN_JSESSIONID, and LINKEDIN_USER_AGENT in .env
set -a && source .env && set +a
make run
```

The server listens on `http://localhost:8080`, the UI is at `/`, and a request
looks like:

```bash
curl "http://localhost:8080/v1/profile?url=https://www.linkedin.com/in/williamhgates"
```

With no `API_KEYS` set, the API is open, which is fine for local work. Getting the
session cookies is covered in [session-cookies.md](session-cookies.md).

## Make targets

| Target | Action |
| --- | --- |
| `make run` | Run the server with `go run`. |
| `make build` | Build a static binary into `bin/`. |
| `make ui` | Install UI dependencies and build into `web/dist`. |
| `make test` | Run the test suite. |
| `make test-race` | Run tests with the race detector. |
| `make cover` | Print a coverage summary. |
| `make bench` | Run the parser benchmark. |
| `make lint` | Check formatting, run `go vet`, and `golangci-lint` if installed. |
| `make fmt` | Format the code. |
| `make tidy` | Tidy modules. |
| `make docker-build` | Build the Docker image. |

## Tests

The suite is deterministic and needs no network or LinkedIn account. It covers URL
validation, the parser against a sanitized fixture in
[../internal/linkedin/parse/testdata](../internal/linkedin/parse/testdata), the
client against an `httptest` server (success, auth failure, login redirect, not
found, rate limit, retried 5xx, timeout, an oversized-body cap, and deterministic
request headers), the service with a mock client including cache, negative cache,
coalescing, gate rejection, and caller-session isolation, the upstream guard
including circuit breaking and server and caller session health, the API layer
for routing, validation, auth, rate limiting, caller credential headers, and
error mapping, and the audit, cache, and config packages. The parser also has a
benchmark.

## UI

The UI is a small Vite and TypeScript app in `web/`. `make ui` builds it into
`web/dist`, which is embedded into the Go binary with `go:embed` and served at
`/`. The committed `web/dist` lets `go build` work without Node, and the Docker
build rebuilds it for freshness.
