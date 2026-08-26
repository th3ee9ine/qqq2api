/**
 * Admin API barrel export
 * Centralized exports for all admin API modules
 */

import dashboardAPI from './dashboard'
import groupsAPI from './groups'
import accountsAPI from './accounts'
import proxiesAPI from './proxies'
import settingsAPI from './settings'
import systemAPI from './system'
import usageAPI from './usage'
import opsAPI from './ops'
import errorPassthroughAPI from './errorPassthrough'
import apiKeysAPI from './apiKeys'
import scheduledTestsAPI from './scheduledTests'
import tlsFingerprintProfileAPI from './tlsFingerprintProfile'
import adminPaymentAPI from './payment'
import affiliatesAPI from './affiliates'
import riskControlAPI from './riskControl'
import auditAPI from './audit'
import imageStorageAPI from './imageStorage'
import pluginsAPI from './plugins'

/**
 * Unified admin API object for convenient access
 */
export const adminAPI = {
  dashboard: dashboardAPI,
  groups: groupsAPI,
  accounts: accountsAPI,
  proxies: proxiesAPI,
  settings: settingsAPI,
  system: systemAPI,
  usage: usageAPI,
  ops: opsAPI,
  errorPassthrough: errorPassthroughAPI,
  apiKeys: apiKeysAPI,
  scheduledTests: scheduledTestsAPI,
  tlsFingerprintProfiles: tlsFingerprintProfileAPI,
  payment: adminPaymentAPI,
  affiliates: affiliatesAPI,
  riskControl: riskControlAPI,
  audit: auditAPI,
  imageStorage: imageStorageAPI,
  plugins: pluginsAPI
}

export {
  dashboardAPI,
  groupsAPI,
  accountsAPI,
  proxiesAPI,
  settingsAPI,
  systemAPI,
  usageAPI,
  opsAPI,
  errorPassthroughAPI,
  apiKeysAPI,
  scheduledTestsAPI,
  tlsFingerprintProfileAPI,
  adminPaymentAPI,
  affiliatesAPI,
  riskControlAPI,
  auditAPI,
  imageStorageAPI,
  pluginsAPI
}

export default adminAPI

// Re-export types used by components
export type { AuditLog, AuditLogQuery, AuditLogListResponse } from './audit'
export type { ErrorPassthroughRule, CreateRuleRequest, UpdateRuleRequest } from './errorPassthrough'
export type { TLSFingerprintProfile, CreateProfileRequest, UpdateProfileRequest } from './tlsFingerprintProfile'
export type { ContentModerationConfig, ContentModerationLog, ModerationMode } from './riskControl'
export type {
  ImageStorageConfig,
  ImageStorageConfigResponse,
  ImageStorageTestResponse,
} from './imageStorage'
export type {
  PluginInstallation,
  PluginCompatibility,
  PluginUISession,
  PluginTestResult
} from './plugins'
