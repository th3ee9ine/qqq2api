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
import dataManagementAPI from './dataManagement'
import apiKeysAPI from './apiKeys'
import scheduledTestsAPI from './scheduledTests'
import backupAPI from './backup'
import tlsFingerprintProfileAPI from './tlsFingerprintProfile'
import adminPaymentAPI from './payment'
import affiliatesAPI from './affiliates'
import riskControlAPI from './riskControl'
import adminComplianceAPI from './compliance'
import auditAPI from './audit'

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
  dataManagement: dataManagementAPI,
  apiKeys: apiKeysAPI,
  scheduledTests: scheduledTestsAPI,
  backup: backupAPI,
  tlsFingerprintProfiles: tlsFingerprintProfileAPI,
  payment: adminPaymentAPI,
  affiliates: affiliatesAPI,
  riskControl: riskControlAPI,
  compliance: adminComplianceAPI,
  audit: auditAPI
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
  dataManagementAPI,
  apiKeysAPI,
  scheduledTestsAPI,
  backupAPI,
  tlsFingerprintProfileAPI,
  adminPaymentAPI,
  affiliatesAPI,
  riskControlAPI,
  adminComplianceAPI,
  auditAPI
}

export default adminAPI

// Re-export types used by components
export type { AuditLog, AuditLogQuery, AuditLogListResponse } from './audit'
export type { ErrorPassthroughRule, CreateRuleRequest, UpdateRuleRequest } from './errorPassthrough'
export type { BackupAgentHealth, DataManagementConfig } from './dataManagement'
export type { TLSFingerprintProfile, CreateProfileRequest, UpdateProfileRequest } from './tlsFingerprintProfile'
export type { ContentModerationConfig, ContentModerationLog, ModerationMode } from './riskControl'
