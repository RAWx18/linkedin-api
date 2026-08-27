// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Prefix used to name resources.')
param namePrefix string = 'linkedinapi'

var acrName = toLower('${namePrefix}acr${uniqueString(resourceGroup().id)}')
var logsName = '${namePrefix}-logs'
var envName = '${namePrefix}-env'
var identityName = '${namePrefix}-id'
var storageName = take(toLower('${namePrefix}st${uniqueString(resourceGroup().id)}'), 24)
var auditShareName = 'audit'
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

// A user-assigned identity that pulls the image. Granting it AcrPull here, before
// the app exists, is what makes an automated first deploy possible: a
// system-assigned identity cannot pull its own image until a role assignment that
// itself depends on the app has been created, which deadlocks the initial rollout.
resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: identityName
  location: location
}

resource acrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(acr.id, identity.id, acrPullRoleId)
  scope: acr
  properties: {
    principalId: identity.properties.principalId
    roleDefinitionId: acrPullRoleId
    principalType: 'ServicePrincipal'
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

output acrName string = acr.name
output acrLoginServer string = acr.properties.loginServer
output identityId string = identity.id
