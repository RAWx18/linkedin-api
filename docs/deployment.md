# Deployment

The target is Azure Container Apps with managed HTTPS ingress. Everything ships in
one container image that includes the embedded UI.

## Docker

```bash
docker build -f deploy/Dockerfile -t linkedin-api .
docker run -p 8080:8080 --env-file .env linkedin-api
```

Or with Compose:

```bash
docker compose -f deploy/docker-compose.yml up --build
```

The image is a multi-stage build. Node builds the UI, Go builds a static binary
with the UI embedded, and the result ships on a distroless non-root base.

## Azure infrastructure

[deploy/bicep/main.bicep](../deploy/bicep/main.bicep) provisions a Container Apps
environment and app, an Azure Container Registry, a Log Analytics workspace, and
Application Insights. The app runs with a system-assigned identity that has
`AcrPull` on the registry, external ingress on port 8080, and liveness and
readiness probes wired to `/healthz` and `/readyz`. Secrets are passed at deploy
time and stored as Container App secrets, never in the repository.

A storage account with an Azure Files share is provisioned for the durable audit
store and mounted at `/data`, so request history survives restarts and redeploys.
The `/admin/usage` query endpoint stays disabled unless the `auditAdminKeys`
parameter is supplied. Auditing is covered in [auditing.md](auditing.md).

The app scales to zero but is capped at one replica by default. The rate limits,
concurrency ceiling, and circuit breaker live in each instance's memory, so a
single replica keeps them authoritative for the one shared LinkedIn session.
Raising `maxReplicas` multiplies those limits and increases pressure on that
session, so only do it alongside a shared coordination store and reduced
per-instance limits.

## GitHub Actions

Two workflows live in [.github/workflows](../.github/workflows):

- `ci.yml` runs formatting checks, `go vet`, the race test suite,
  `golangci-lint`, the UI build, and a Docker build on every push and pull
  request.
- `deploy.yml` logs in to Azure with OIDC, deploys the Bicep template, builds and
  pushes the image to ACR, and rolls out the Container App.

Set these repository secrets for deployment: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
`AZURE_SUBSCRIPTION_ID`, `LINKEDIN_LI_AT`, `LINKEDIN_JSESSIONID`, and `API_KEYS`.
Then run the Deploy workflow with a resource group name.

## Manual deploy

```bash
az group create -n linkedin-api-rg -l eastus
az deployment group create \
  -g linkedin-api-rg \
  -f deploy/bicep/main.bicep \
  -p linkedInLiAt="$LINKEDIN_LI_AT" \
     linkedInJSessionID="$LINKEDIN_JSESSIONID" \
     apiKeys="$API_KEYS"
# build and push the image to the new ACR, then point the app at it
az containerapp update -g linkedin-api-rg -n linkedinapi-app --image <acr>/linkedin-api:<tag>
```

Verify the deployment:

```bash
curl https://<app-fqdn>/healthz
```

## Recovering an expired or challenged session

When LinkedIn challenges or invalidates the server session, the service protects
itself automatically and surfaces the state for an operator:

1. Detection. Authentication failures and login redirects trip the session
   circuit. `upstream_session_healthy` drops to `0`, `upstream_circuit_open` goes
   to `1`, a structured `warn` log records that the session is unhealthy along
   with the cooldown, and profile requests return controlled `502` and `503`
   responses. No credential value is ever logged.
2. Containment. While unhealthy the breaker stops issuing server-session traffic
   and admits only one probe per cooldown, which doubles on repeated failures, so
   a dead session is not hammered.
3. Replace the credentials. Obtain a fresh `li_at` and `JSESSIONID` from a
   signed-in browser (see [configuration.md](configuration.md#session-cookies)),
   update the Container App secrets, and roll a new revision so they are picked up:

   ```bash
   az containerapp secret set -g linkedin-api-rg -n linkedinapi-app \
     --secrets linkedin-li-at="$LINKEDIN_LI_AT" linkedin-jsessionid="$LINKEDIN_JSESSIONID"
   az containerapp update -g linkedin-api-rg -n linkedinapi-app
   ```

4. Recovery. After the restart the first successful probe closes the circuit,
   `upstream_session_healthy` returns to `1`, and an `info` log records the
   recovery. No manual reset is needed.

Callers that supply their own session (`LINKEDIN_ALLOW_CALLER_SESSION`, on by
default) are unaffected by a server-session outage: their requests keep working as
long as their own session is valid. A caller whose session expires gets a
`401 caller_session_invalid` and must supply fresh credentials, which never
requires an operator action or a restart.
