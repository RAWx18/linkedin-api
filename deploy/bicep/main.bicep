// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Prefix used to name resources.')
param namePrefix string = 'linkedinapi'

@description('Container image (registry/repository:tag). Defaults to a public placeholder for first provisioning.')
param containerImage string = 'mcr.microsoft.com/k8se/quickstart:latest'

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
var logsName = '${namePrefix}-logs'
var appInsightsName = '${namePrefix}-ai'
var envName = '${namePrefix}-env'
var appName = '${namePrefix}-app'
var storageName = take(toLower('${namePrefix}st${uniqueString(resourceGroup().id)}'), 24)
var auditShareName = 'audit'
var auditVolumeName = 'audit-data'
var auditEnvStorageName = 'audit-storage'
var acrPullRoleId = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '7f951dda-4ed3-4680-a7ca-43fe172d538d')

resource logs 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: logsName
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: 30
  }
}

resource appInsights 'Microsoft.Insights/components@2020-02-02' = {
  name: appInsightsName
  location: location
  kind: 'web'
  properties: {
    Application_Type: 'web'
    WorkspaceResourceId: logs.id
  }
}

resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' = {
  name: acrName
  location: location
  sku: {
    name: 'Basic'
  }
  properties: {
    adminUserEnabled: false
  }
}

resource env 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: envName
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logs.properties.customerId
        sharedKey: logs.listKeys().primarySharedKey
      }
    }
  }
}

// Durable audit history lives on an Azure Files share so it survives restarts and
// redeploys. The single-replica app is the only writer, which keeps SQLite safe
// on the network mount.
resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: storageName
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
  properties: {
    minimumTlsVersion: 'TLS1_2'
    allowBlobPublicAccess: false
  }
}

resource fileService 'Microsoft.Storage/storageAccounts/fileServices@2023-05-01' = {
  parent: storage
  name: 'default'
}

resource auditShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  parent: fileService
  name: auditShareName
  properties: {
    shareQuota: 5
    enabledProtocols: 'SMB'
  }
}

resource auditStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  parent: env
  name: auditEnvStorageName
  properties: {
    azureFile: {
      accountName: storage.name
      accountKey: storage.listKeys().keys[0].value
      shareName: auditShareName
      accessMode: 'ReadWrite'
    }
  }
  dependsOn: [
    auditShare
  ]
}

resource app 'Microsoft.App/containerApps@2024-03-01' = {
  name: appName
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  dependsOn: [
    auditStorage
  ]
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
          identity: 'system'
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
        {
          name: 'appinsights-connection'
          value: appInsights.properties.ConnectionString
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
              name: 'APPLICATIONINSIGHTS_CONNECTION_STRING'
              secretRef: 'appinsights-connection'
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

resource acrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(acr.id, app.id, acrPullRoleId)
  scope: acr
  properties: {
    principalId: app.identity.principalId
    roleDefinitionId: acrPullRoleId
    principalType: 'ServicePrincipal'
  }
}

output acrName string = acr.name
output acrLoginServer string = acr.properties.loginServer
output appName string = app.name
output appUrl string = 'https://${app.properties.configuration.ingress.fqdn}'
