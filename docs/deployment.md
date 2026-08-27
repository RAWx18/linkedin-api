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

The Bicep is split so an automated first deploy never deadlocks.
[deploy/bicep/infra.bicep](../deploy/bicep/infra.bicep) provisions the Azure
Container Registry, a user-assigned identity granted `AcrPull` on the registry, a
Container Apps environment, a Log Analytics workspace, and Application Insights.
[deploy/bicep/app.bicep](../deploy/bicep/app.bicep) then deploys the Container App
against an image that already exists in the registry. The app pulls with the
user-assigned identity, which holds `AcrPull` before the app is created, so it
never waits on a role assignment that depends on itself. Ingress is external on
port 8080 with liveness and readiness probes wired to `/healthz` and `/readyz`.
Secrets are passed at deploy time and stored as Container App secrets, never in
the repository.

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
- `deploy.yml` logs in to Azure with OIDC, provisions `infra.bicep`, builds the
  image inside ACR with `az acr build`, deploys `app.bicep` against that image,
  and smoke-tests the public `/healthz` endpoint.

Set these repository secrets for deployment: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
`AZURE_SUBSCRIPTION_ID`, `LINKEDIN_LI_AT`, `LINKEDIN_JSESSIONID`,
`LINKEDIN_USER_AGENT`, and `API_KEYS`.
Then run the Deploy workflow with a resource group name.

## Manual deploy

Deploy the infrastructure, build the image into the new registry, then deploy the
app against that image:

```bash
az group create -n linkedin-api-rg -l eastus

# 1. Infrastructure: registry, identity + AcrPull, environment, logs, storage.
acr=$(az deployment group create -g linkedin-api-rg \
  -f deploy/bicep/infra.bicep \
  --query properties.outputs.acrName.value -o tsv)

# 2. Build the image inside the registry.
az acr build --registry "$acr" --image linkedin-api:v1 -f deploy/Dockerfile .

# 3. Application, using the freshly built image.
az deployment group create -g linkedin-api-rg \
  -f deploy/bicep/app.bicep \
  -p containerImage="$acr.azurecr.io/linkedin-api:v1" \
     linkedInLiAt="$LINKEDIN_LI_AT" \
     linkedInJSessionID="$LINKEDIN_JSESSIONID" \
     linkedInUserAgent="$LINKEDIN_USER_AGENT" \
     apiKeys="$API_KEYS"
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
