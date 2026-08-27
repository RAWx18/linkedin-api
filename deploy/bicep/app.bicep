// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Prefix used to name resources. Must match the infra deployment.')
param namePrefix string = 'linkedinapi'

@description('Container image (registry/repository:tag) to deploy.')
param containerImage string

@description('Minimum replica count (0 enables scale-to-zero).')
param minReplicas int = 0

@description('Maximum replica count. Kept at 1 by default because the rate limit, concurrency cap, and circuit breaker are per-instance; scaling out multiplies pressure on the single shared LinkedIn session, so raise this only with matching upstream limits.')
param maxReplicas int = 1

@description('LinkedIn li_at session cookie.')
@secure()
param linkedInLiAt string

@description('LinkedIn JSESSIONID cookie value.')
@secure()
param linkedInJSessionID string

@description('Browser User-Agent of the session used to obtain the LinkedIn cookies. Required so the session context matches and is not invalidated.')
param linkedInUserAgent string

@description('Comma-separated public API keys.')
@secure()
param apiKeys string

@description('Comma-separated admin keys for the /admin/usage endpoint. Leave empty to keep the endpoint disabled.')
@secure()
param auditAdminKeys string = ''

var acrName = toLower('${namePrefix}acr${uniqueString(resourceGroup().id)}')
var envName = '${namePrefix}-env'
var appName = '${namePrefix}-app'
var identityName = '${namePrefix}-id'
var auditVolumeName = 'audit-data'
var auditEnvStorageName = 'audit-storage'

resource env 'Microsoft.App/managedEnvironments@2024-03-01' existing = {
  name: envName
}

resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' existing = {
  name: acrName
}

resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' existing = {
  name: identityName
}

resource app 'Microsoft.App/containerApps@2024-03-01' = {
  name: appName
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: env.id
    configuration: {
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        allowInsecure: false
      }
      registries: [
        {
          server: acr.properties.loginServer
          identity: identity.id
        }
      ]
      secrets: concat([
        {
          name: 'linkedin-li-at'
          value: linkedInLiAt
        }
        {
          name: 'linkedin-jsessionid'
          value: linkedInJSessionID
        }
        {
          name: 'api-keys'
          value: apiKeys
        }
      ], empty(auditAdminKeys) ? [] : [
        {
          name: 'audit-admin-keys'
          value: auditAdminKeys
        }
      ])
    }
    template: {
      containers: [
        {
          name: 'linkedin-api'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: concat([
            {
              name: 'ENV'
              value: 'production'
            }
            {
              name: 'SERVER_PORT'
              value: '8080'
            }
            {
              name: 'LOG_FORMAT'
              value: 'json'
            }
            {
              name: 'METRICS_ENABLED'
              value: 'true'
            }
            {
              name: 'PROFILE_TIMEOUT'
              value: '15s'
            }
            {
              name: 'LINKEDIN_LI_AT'
              secretRef: 'linkedin-li-at'
            }
            {
              name: 'LINKEDIN_JSESSIONID'
              secretRef: 'linkedin-jsessionid'
            }
            {
              name: 'LINKEDIN_USER_AGENT'
              value: linkedInUserAgent
            }
            {
              name: 'API_KEYS'
              secretRef: 'api-keys'
            }
            {
              name: 'AUDIT_ENABLED'
              value: 'true'
            }
            {
              name: 'AUDIT_DB_PATH'
              value: '/data/audit.db'
            }
          ], empty(auditAdminKeys) ? [] : [
            {
              name: 'AUDIT_ADMIN_KEYS'
              secretRef: 'audit-admin-keys'
            }
          ])
          volumeMounts: [
            {
              volumeName: auditVolumeName
              mountPath: '/data'
            }
          ]
          probes: [
            {
              type: 'Liveness'
              httpGet: {
                path: '/healthz'
                port: 8080
              }
              initialDelaySeconds: 5
              periodSeconds: 15
            }
            {
              type: 'Readiness'
              httpGet: {
                path: '/readyz'
                port: 8080
              }
              initialDelaySeconds: 3
              periodSeconds: 10
            }
          ]
        }
      ]
      volumes: [
        {
          name: auditVolumeName
          storageType: 'AzureFile'
          storageName: auditEnvStorageName
        }
      ]
      scale: {
        minReplicas: minReplicas
        maxReplicas: maxReplicas
        rules: [
          {
            name: 'http-scale'
            http: {
              metadata: {
                concurrentRequests: '50'
              }
            }
          }
        ]
      }
    }
  }
}

output appName string = app.name
output appUrl string = 'https://${app.properties.configuration.ingress.fqdn}'
