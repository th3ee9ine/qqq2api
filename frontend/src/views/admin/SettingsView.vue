<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <div
          class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"
        ></div>
      </div>

      <!-- Settings Form -->
      <form v-else @submit.prevent="saveSettings" class="space-y-6" novalidate>
        <!-- Tab Navigation -->
        <div class="settings-tabs-shell">
          <nav
            class="settings-tabs-scroll"
            role="tablist"
            :aria-label="t('admin.settings.title')"
          >
            <div class="settings-tabs">
              <button
                v-for="tab in settingsTabs"
                :key="tab.key"
                :id="`settings-tab-${tab.key}`"
                type="button"
                role="tab"
                :aria-selected="activeTab === tab.key"
                :tabindex="activeTab === tab.key ? 0 : -1"
                :class="[
                  'settings-tab',
                  activeTab === tab.key && 'settings-tab-active',
                ]"
                @click="selectSettingsTab(tab.key)"
                @keydown="handleSettingsTabKeydown($event, tab.key)"
              >
                <span class="settings-tab-icon">
                  <Icon :name="tab.icon" size="sm" />
                </span>
                <span class="settings-tab-label">{{
                  t(`admin.settings.tabs.${tab.key}`)
                }}</span>
              </button>
            </div>
          </nav>
        </div>

        <!-- Tab: Security — Admin API Key -->
        <div v-show="activeTab === 'security'" class="space-y-6">
          <!-- Admin API Key Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.adminApiKey.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.adminApiKey.description") }}
              </p>
            </div>
            <div class="space-y-4 p-6">
              <!-- Security Warning -->
              <div
                class="rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-900/20"
              >
                <div class="flex items-start">
                  <Icon
                    name="exclamationTriangle"
                    size="md"
                    class="mt-0.5 flex-shrink-0 text-amber-500"
                  />
                  <p class="ml-3 text-sm text-amber-700 dark:text-amber-300">
                    {{ t("admin.settings.adminApiKey.securityWarning") }}
                  </p>
                </div>
              </div>

              <!-- Loading State -->
              <div
                v-if="adminApiKeyLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <!-- No Key Configured -->
              <div
                v-else-if="!adminApiKeyExists"
                class="flex items-center justify-between"
              >
                <span class="text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.adminApiKey.notConfigured") }}
                </span>
                <button
                  type="button"
                  @click="createAdminApiKey"
                  :disabled="adminApiKeyOperating"
                  class="btn btn-primary btn-sm"
                >
                  <svg
                    v-if="adminApiKeyOperating"
                    class="mr-1 h-4 w-4 animate-spin"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <circle
                      class="opacity-25"
                      cx="12"
                      cy="12"
                      r="10"
                      stroke="currentColor"
                      stroke-width="4"
                    ></circle>
                    <path
                      class="opacity-75"
                      fill="currentColor"
                      d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                    ></path>
                  </svg>
                  {{
                    adminApiKeyOperating
                      ? t("admin.settings.adminApiKey.creating")
                      : t("admin.settings.adminApiKey.create")
                  }}
                </button>
              </div>

              <!-- Key Exists -->
              <div v-else class="space-y-4">
                <div class="flex items-center justify-between">
                  <div>
                    <label
                      class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.adminApiKey.currentKey") }}
                    </label>
                    <code
                      class="rounded bg-gray-100 px-2 py-1 font-mono text-sm text-gray-900 dark:bg-dark-700 dark:text-gray-100"
                    >
                      {{ adminApiKeyMasked }}
                    </code>
                  </div>
                  <div class="flex gap-2">
                    <button
                      type="button"
                      @click="regenerateAdminApiKey"
                      :disabled="adminApiKeyOperating"
                      class="btn btn-secondary btn-sm"
                    >
                      {{
                        adminApiKeyOperating
                          ? t("admin.settings.adminApiKey.regenerating")
                          : t("admin.settings.adminApiKey.regenerate")
                      }}
                    </button>
                    <button
                      type="button"
                      @click="deleteAdminApiKey"
                      :disabled="adminApiKeyOperating"
                      class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
                    >
                      {{ t("admin.settings.adminApiKey.delete") }}
                    </button>
                  </div>
                </div>

                <!-- Newly Generated Key Display -->
                <div
                  v-if="newAdminApiKey"
                  class="space-y-3 rounded-lg border border-green-200 bg-green-50 p-4 dark:border-green-800 dark:bg-green-900/20"
                >
                  <p
                    class="text-sm font-medium text-green-700 dark:text-green-300"
                  >
                    {{ t("admin.settings.adminApiKey.keyWarning") }}
                  </p>
                  <div class="flex items-center gap-2">
                    <code
                      class="flex-1 select-all break-all rounded border border-green-300 bg-white px-3 py-2 font-mono text-sm dark:border-green-700 dark:bg-dark-800"
                    >
                      {{ newAdminApiKey }}
                    </code>
                    <button
                      type="button"
                      @click="copyNewKey"
                      class="btn btn-primary btn-sm flex-shrink-0"
                    >
                      {{ t("admin.settings.adminApiKey.copyKey") }}
                    </button>
                  </div>
                  <p class="text-xs text-green-600 dark:text-green-400">
                    {{ t("admin.settings.adminApiKey.usage") }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
        <!-- /Tab: Security — Admin API Key -->

        <!-- Tab: Gateway -->
        <div v-show="activeTab === 'gateway'" class="space-y-6">
          <!-- Overload Cooldown (529) Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.overloadCooldown.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.overloadCooldown.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="overloadCooldownLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.overloadCooldown.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.overloadCooldown.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="overloadCooldownForm.enabled" />
                </div>

                <div
                  v-if="overloadCooldownForm.enabled"
                  class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.overloadCooldown.cooldownMinutes") }}
                    </label>
                    <input
                      v-model.number="overloadCooldownForm.cooldown_minutes"
                      type="number"
                      min="1"
                      max="120"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t("admin.settings.overloadCooldown.cooldownMinutesHint")
                      }}
                    </p>
                  </div>
                </div>

                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveOverloadCooldownSettings"
                    :disabled="overloadCooldownSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="overloadCooldownSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      overloadCooldownSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Rate Limit Cooldown (429) Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.rateLimit429Cooldown.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.rateLimit429Cooldown.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="rateLimit429CooldownLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.rateLimit429Cooldown.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.rateLimit429Cooldown.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="rateLimit429CooldownForm.enabled" />
                </div>

                <div
                  v-if="rateLimit429CooldownForm.enabled"
                  class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t(
                          "admin.settings.rateLimit429Cooldown.cooldownSeconds",
                        )
                      }}
                    </label>
                    <input
                      v-model.number="rateLimit429CooldownForm.cooldown_seconds"
                      type="number"
                      min="1"
                      max="7200"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.rateLimit429Cooldown.cooldownSecondsHint",
                        )
                      }}
                    </p>
                  </div>
                </div>

                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveRateLimit429CooldownSettings"
                    :disabled="rateLimit429CooldownSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="rateLimit429CooldownSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      rateLimit429CooldownSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Stream Timeout Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.streamTimeout.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.streamTimeout.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Loading State -->
              <div
                v-if="streamTimeoutLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <!-- Enable Stream Timeout -->
                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.streamTimeout.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.streamTimeout.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="streamTimeoutForm.enabled" />
                </div>

                <!-- Settings - Only show when enabled -->
                <div
                  v-if="streamTimeoutForm.enabled"
                  class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <!-- Action -->
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.streamTimeout.action") }}
                    </label>
                    <select
                      v-model="streamTimeoutForm.action"
                      class="input w-64"
                    >
                      <option value="temp_unsched">
                        {{
                          t("admin.settings.streamTimeout.actionTempUnsched")
                        }}
                      </option>
                      <option value="error">
                        {{ t("admin.settings.streamTimeout.actionError") }}
                      </option>
                      <option value="none">
                        {{ t("admin.settings.streamTimeout.actionNone") }}
                      </option>
                    </select>
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.streamTimeout.actionHint") }}
                    </p>
                  </div>

                  <!-- Temp Unsched Minutes (only show when action is temp_unsched) -->
                  <div v-if="streamTimeoutForm.action === 'temp_unsched'">
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.streamTimeout.tempUnschedMinutes") }}
                    </label>
                    <input
                      v-model.number="streamTimeoutForm.temp_unsched_minutes"
                      type="number"
                      min="1"
                      max="60"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t("admin.settings.streamTimeout.tempUnschedMinutesHint")
                      }}
                    </p>
                  </div>

                  <!-- Threshold Count -->
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.streamTimeout.thresholdCount") }}
                    </label>
                    <input
                      v-model.number="streamTimeoutForm.threshold_count"
                      type="number"
                      min="1"
                      max="10"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.streamTimeout.thresholdCountHint") }}
                    </p>
                  </div>

                  <!-- Threshold Window Minutes -->
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t("admin.settings.streamTimeout.thresholdWindowMinutes")
                      }}
                    </label>
                    <input
                      v-model.number="
                        streamTimeoutForm.threshold_window_minutes
                      "
                      type="number"
                      min="1"
                      max="60"
                      class="input w-32"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.streamTimeout.thresholdWindowMinutesHint",
                        )
                      }}
                    </p>
                  </div>
                </div>

                <!-- Save Button -->
                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveStreamTimeoutSettings"
                    :disabled="streamTimeoutSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="streamTimeoutSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      streamTimeoutSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

        </div>
        <!-- /Tab: Gateway -->




        <!-- Tab: Security — API Key ACL and panel protection -->
        <div v-show="activeTab === 'security'" class="space-y-6">
          <!-- API Key IP ACL Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.apiKeyAcl.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.apiKeyAcl.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">
                    {{ t("admin.settings.apiKeyAcl.trustForwardedIp") }}
                  </label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.apiKeyAcl.trustForwardedIpHint") }}
                  </p>
                </div>
                <Toggle v-model="form.api_key_acl_trust_forwarded_ip" />
              </div>

              <div
                v-if="form.api_key_acl_trust_forwarded_ip"
                class="border-t border-gray-100 pt-4 dark:border-dark-700"
              >
                <label
                  for="forwarded-client-ip-headers"
                  class="font-medium text-gray-900 dark:text-white"
                >
                  {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeaders") }}
                </label>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeadersHint") }}
                </p>
                <div
                  class="mt-3 rounded-lg border border-gray-300 bg-white p-2 dark:border-dark-500 dark:bg-dark-700"
                >
                  <div class="flex flex-wrap items-center gap-2">
                    <span
                      v-for="header in form.forwarded_client_ip_headers"
                      :key="header"
                      data-testid="forwarded-client-ip-header-tag"
                      class="inline-flex items-center gap-1 rounded bg-gray-100 px-2 py-1 text-xs font-mono text-gray-700 dark:bg-dark-600 dark:text-gray-200"
                    >
                      <span>{{ header }}</span>
                      <button
                        type="button"
                        class="rounded-full text-gray-500 hover:bg-gray-200 hover:text-gray-700 dark:text-gray-300 dark:hover:bg-dark-500 dark:hover:text-white"
                        :aria-label="t('admin.settings.apiKeyAcl.removeForwardedClientIpHeader', { header })"
                        @click="removeForwardedClientIpHeader(header)"
                      >
                        <Icon
                          name="x"
                          size="xs"
                          class="h-3.5 w-3.5"
                          :stroke-width="2"
                        />
                      </button>
                    </span>
                    <div
                      class="flex min-w-[220px] flex-1 items-center gap-1 rounded border border-transparent px-2 py-1 focus-within:border-primary-300 dark:focus-within:border-primary-700"
                    >
                      <input
                        id="forwarded-client-ip-headers"
                        v-model="forwardedClientIpHeaderDraft"
                        data-testid="forwarded-client-ip-headers-input"
                        type="text"
                        class="w-full bg-transparent text-sm font-mono text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
                        :placeholder="t('admin.settings.apiKeyAcl.forwardedClientIpHeadersPlaceholder')"
                        @keydown="handleForwardedClientIpHeaderKeydown"
                        @blur="commitForwardedClientIpHeaderDraft"
                        @paste="handleForwardedClientIpHeaderPaste"
                      />
                    </div>
                  </div>
                </div>
                <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.apiKeyAcl.forwardedClientIpHeadersRiskHint") }}
                </p>
              </div>
            </div>
          </div>

          <!-- Panel API Rate Limit Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <div class="flex items-center gap-2">
                <Icon
                  name="shield"
                  size="md"
                  class="text-primary-500"
                />
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                  {{ t("admin.settings.panelRateLimit.title") }}
                </h2>
              </div>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.panelRateLimit.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="panelRateLimitLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <!-- 计数维度说明：按账号计数，反代部署无误伤 -->
                <div
                  class="rounded-lg border border-sky-200 bg-sky-50 p-4 dark:border-sky-800 dark:bg-sky-900/20"
                >
                  <div class="flex items-start">
                    <Icon
                      name="infoCircle"
                      size="md"
                      class="mt-0.5 flex-shrink-0 text-sky-500"
                    />
                    <p class="ml-3 text-sm text-sky-700 dark:text-sky-300">
                      {{ t("admin.settings.panelRateLimit.proxySafeNote") }}
                    </p>
                  </div>
                </div>

                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.panelRateLimit.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.panelRateLimit.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="panelRateLimitForm.enabled" />
                </div>

                <div
                  v-if="panelRateLimitForm.enabled"
                  class="space-y-5 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <div class="grid grid-cols-1 gap-6 sm:grid-cols-2">
                    <div>
                      <label
                        class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                      >
                        {{ t("admin.settings.panelRateLimit.userRpm") }}
                      </label>
                      <div class="flex items-center gap-2">
                        <input
                          v-model.number="panelRateLimitForm.user_rpm"
                          data-testid="panel-rate-limit-user-rpm"
                          type="number"
                          min="0"
                          max="100000"
                          class="input w-32"
                        />
                        <span class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.panelRateLimit.perMinute") }}
                        </span>
                      </div>
                      <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.panelRateLimit.userRpmHint") }}
                      </p>
                    </div>

                    <div>
                      <label
                        class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                      >
                        {{ t("admin.settings.panelRateLimit.heavyRpm") }}
                      </label>
                      <div class="flex items-center gap-2">
                        <input
                          v-model.number="panelRateLimitForm.heavy_rpm"
                          type="number"
                          min="0"
                          max="100000"
                          class="input w-32"
                        />
                        <span class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.panelRateLimit.perMinute") }}
                        </span>
                      </div>
                      <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.panelRateLimit.heavyRpmHint") }}
                      </p>
                    </div>

                    <div>
                      <label
                        class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                      >
                        {{ t("admin.settings.panelRateLimit.publicIpRpm") }}
                      </label>
                      <div class="flex items-center gap-2">
                        <input
                          v-model.number="panelRateLimitForm.public_ip_rpm"
                          type="number"
                          min="0"
                          max="100000"
                          class="input w-32"
                        />
                        <span class="text-sm text-gray-500 dark:text-gray-400">
                          {{ t("admin.settings.panelRateLimit.perMinute") }}
                        </span>
                      </div>
                      <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.panelRateLimit.publicIpRpmHint") }}
                      </p>
                    </div>
                  </div>

                  <div
                    class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700"
                  >
                    <div>
                      <label class="font-medium text-gray-900 dark:text-white">{{
                        t("admin.settings.panelRateLimit.exemptAdmin")
                      }}</label>
                      <p class="text-sm text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.panelRateLimit.exemptAdminHint") }}
                      </p>
                    </div>
                    <Toggle v-model="panelRateLimitForm.exempt_admin" />
                  </div>
                </div>

                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    data-testid="panel-rate-limit-save"
                    @click="savePanelRateLimitSettings"
                    :disabled="panelRateLimitSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="panelRateLimitSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      panelRateLimitSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

        </div>

        <!-- Retired user-login controls remain in a non-rendered compatibility block. -->


        <!-- Retained only as a non-rendered compatibility block for old saved settings. -->

        <!-- /Tab: Users -->

        <!-- Tab: Gateway — Scheduling and Services -->
        <div v-show="activeTab === 'gateway'" class="space-y-6">
          <div class="card" data-testid="grok-settings">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Grok</h2>
            </div>
            <div class="space-y-5 p-6">
              <div class="grid gap-5 border-b border-gray-100 pb-5 dark:border-dark-700 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                <div>
                  <label
                    for="grok-default-text-model"
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.grokDefaultTextModel") }}
                  </label>
                  <input
                    id="grok-default-text-model"
                    v-model.trim="form.grok_default_text_model"
                    type="text"
                    class="input mt-2 w-full"
                    list="grok-default-text-model-options"
                    data-testid="grok-default-text-model"
                    placeholder="grok-4.6"
                  />
                  <datalist id="grok-default-text-model-options">
                    <option value="grok-4.7" />
                    <option value="grok-4.6" />
                    <option value="grok-4.5" />
                    <option value="grok-4.1-fast" />
                    <option value="grok-4" />
                  </datalist>
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.grokDefaultTextModelHint") }}
                  </p>
                </div>
                <div class="flex items-center justify-between gap-5 md:min-w-72">
                  <div>
                    <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t("admin.settings.gatewayForwarding.grokCrossClientMap") }}
                    </label>
                    <p class="mt-0.5 max-w-sm text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.gatewayForwarding.grokCrossClientMapHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="form.grok_cross_client_model_map_enabled"
                    data-testid="grok-cross-client-model-map-toggle"
                  />
                </div>
              </div>
              <div>
                <label
                  for="grok-default-base-url-mode"
                  class="text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.gatewayForwarding.grokDefaultBaseURLMode") }}
                </label>
                <select
                  id="grok-default-base-url-mode"
                  v-model="form.grok_default_base_url_mode"
                  class="input mt-2 w-full"
                  data-testid="grok-default-base-url-mode"
                >
                  <option value="cli">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeCLI") }}</option>
                  <option value="api">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeAPI") }}</option>
                  <option value="us-east-1">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeUSEast1") }}</option>
                  <option value="us-west-2">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeUSWest2") }}</option>
                  <option value="eu-west-1">{{ t("admin.settings.gatewayForwarding.grokBaseURLModeEUWest1") }}</option>
                </select>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.gatewayForwarding.grokDefaultBaseURLModeHint") }}
                </p>
              </div>
            </div>
          </div>

          <!-- Upstream Billing Probe Settings -->
          <div class="card" data-testid="upstream-billing-probe-settings">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.upstreamBillingProbe.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.upstreamBillingProbe.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="upstreamBillingProbeLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <div class="flex items-center justify-between gap-4">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">
                      {{ t("admin.settings.upstreamBillingProbe.enabled") }}
                    </label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.upstreamBillingProbe.enabledHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="upstreamBillingProbeForm.enabled"
                    :aria-label="t('admin.settings.upstreamBillingProbe.enabled')"
                    data-testid="upstream-billing-probe-enabled"
                  />
                </div>

                <div
                  v-if="upstreamBillingProbeForm.enabled"
                  class="border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    for="upstream-billing-probe-interval"
                  >
                    {{ t("admin.settings.upstreamBillingProbe.intervalMinutes") }}
                  </label>
                  <input
                    id="upstream-billing-probe-interval"
                    v-model.number="upstreamBillingProbeForm.interval_minutes"
                    type="number"
                    min="5"
                    max="1440"
                    class="input w-32"
                    data-testid="upstream-billing-probe-interval"
                    @keydown.enter.prevent="saveUpstreamBillingProbeSettings"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.upstreamBillingProbe.intervalHint") }}
                  </p>
                </div>

                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    class="btn btn-primary btn-sm"
                    :disabled="upstreamBillingProbeSaving"
                    data-testid="upstream-billing-probe-save"
                    @click="saveUpstreamBillingProbeSettings"
                  >
                    {{
                      upstreamBillingProbeSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Gateway Scheduling Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.scheduling.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.scheduling.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.scheduling.allowUngroupedKey") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.scheduling.allowUngroupedKeyHint") }}
                  </p>
                </div>
                <Toggle v-model="form.allow_ungrouped_key_scheduling" />
              </div>

              <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                <div class="mb-3">
                  <label class="font-medium text-gray-900 dark:text-white">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsTitle",
                      )
                    }}
                  </label>
                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsDescription",
                      )
                    }}
                  </p>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsGlobalHint",
                      )
                    }}
                  </p>
                  <p class="mt-0.5 text-xs text-amber-600 dark:text-amber-400">
                    {{
                      t(
                        "admin.settings.scheduling.accountSchedulingThresholdsDisabledHint",
                      )
                    }}
                  </p>
                </div>
                <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
                  <div
                    v-for="platform in schedulingThresholdPlatforms"
                    :key="platform"
                    class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
                  >
                    <div class="flex items-start justify-between gap-3">
                      <div>
                        <label
                          class="font-mono text-sm font-medium text-gray-900 dark:text-white"
                        >
                          {{ platform }}
                        </label>
                        <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                          {{
                            t(
                              "admin.settings.scheduling.accountSchedulingThresholdsRangeHint",
                            )
                          }}
                        </p>
                      </div>
                      <span
                        class="rounded bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                      >
                        %
                      </span>
                    </div>
                    <input
                      v-model.number="form.account_scheduling_thresholds[platform]"
                      type="number"
                      min="1"
                      max="100"
                      step="1"
                      class="input mt-3"
                      :data-testid="`account-scheduling-threshold-${platform}`"
                      placeholder="100"
                    />
                  </div>
                </div>
              </div>

            </div>
          </div>

          <!-- Async image task object storage (independent from data backup). -->
          <div class="card" data-testid="image-storage-settings">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.imageStorage.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.imageStorage.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="imageStorageLoading"
                class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>
              <div
                v-else-if="imageStorageLoadError"
                class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300"
              >
                {{ imageStorageLoadError }}
              </div>
              <template v-else>
                <div class="flex items-center justify-between gap-4">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">
                      {{ t("admin.settings.imageStorage.enabled") }}
                    </label>
                    <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.imageStorage.enabledHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="imageStorageForm.enabled"
                    data-testid="image-storage-enabled"
                  />
                </div>

                <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <div>
                    <label class="input-label" for="image-storage-endpoint">
                      {{ t("admin.settings.imageStorage.endpoint") }}
                    </label>
                    <input
                      id="image-storage-endpoint"
                      v-model="imageStorageForm.endpoint"
                      class="input w-full font-mono text-sm"
                      placeholder="https://<account_id>.r2.cloudflarestorage.com"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-region">
                      {{ t("admin.settings.imageStorage.region") }}
                    </label>
                    <input
                      id="image-storage-region"
                      v-model="imageStorageForm.region"
                      class="input w-full font-mono text-sm"
                      placeholder="auto"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-bucket">
                      {{ t("admin.settings.imageStorage.bucket") }}
                    </label>
                    <input
                      id="image-storage-bucket"
                      v-model="imageStorageForm.bucket"
                      class="input w-full font-mono text-sm"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-prefix">
                      {{ t("admin.settings.imageStorage.prefix") }}
                    </label>
                    <input
                      id="image-storage-prefix"
                      v-model="imageStorageForm.prefix"
                      class="input w-full font-mono text-sm"
                      placeholder="images/"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-access-key">
                      {{ t("admin.settings.imageStorage.accessKeyId") }}
                    </label>
                    <input
                      id="image-storage-access-key"
                      v-model="imageStorageForm.access_key_id"
                      class="input w-full font-mono text-sm"
                      autocomplete="off"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-secret-key">
                      {{ t("admin.settings.imageStorage.secretAccessKey") }}
                    </label>
                    <input
                      id="image-storage-secret-key"
                      v-model="imageStorageForm.secret_access_key"
                      type="password"
                      class="input w-full font-mono text-sm"
                      autocomplete="new-password"
                      :placeholder="
                        imageStorageSecretConfigured
                          ? t('admin.settings.imageStorage.secretConfigured')
                          : ''
                      "
                    />
                  </div>
                  <div class="md:col-span-2">
                    <label class="input-label" for="image-storage-public-url">
                      {{ t("admin.settings.imageStorage.publicBaseUrl") }}
                    </label>
                    <input
                      id="image-storage-public-url"
                      v-model="imageStorageForm.public_base_url"
                      type="url"
                      class="input w-full font-mono text-sm"
                      :placeholder="
                        t('admin.settings.imageStorage.publicBaseUrlPlaceholder')
                      "
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-presign-expiry">
                      {{ t("admin.settings.imageStorage.presignExpiryHours") }}
                    </label>
                    <input
                      id="image-storage-presign-expiry"
                      v-model.number="imageStorageForm.presign_expiry_hours"
                      type="number"
                      min="1"
                      step="1"
                      class="input w-full"
                    />
                  </div>
                  <div>
                    <label class="input-label" for="image-storage-max-download">
                      {{ t("admin.settings.imageStorage.maxDownloadBytes") }}
                    </label>
                    <input
                      id="image-storage-max-download"
                      v-model.number="imageStorageForm.max_download_bytes"
                      type="number"
                      min="1"
                      step="1"
                      class="input w-full"
                    />
                    <p class="input-hint">
                      {{ t("admin.settings.imageStorage.maxDownloadBytesHint") }}
                    </p>
                  </div>
                </div>

                <div
                  class="flex items-center justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">
                      {{ t("admin.settings.imageStorage.forcePathStyle") }}
                    </label>
                    <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.imageStorage.forcePathStyleHint") }}
                    </p>
                  </div>
                  <Toggle
                    v-model="imageStorageForm.force_path_style"
                    data-testid="image-storage-force-path-style"
                  />
                </div>

                <div class="flex flex-wrap justify-end gap-2 border-t border-gray-100 pt-4 dark:border-dark-700">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="imageStorageTesting || imageStorageSaving"
                    data-testid="image-storage-test"
                    @click="testImageStorageConnection"
                  >
                    <svg
                      v-if="imageStorageTesting"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      imageStorageTesting
                        ? t("admin.settings.imageStorage.testing")
                        : t("admin.settings.imageStorage.testConnection")
                    }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-primary btn-sm"
                    :disabled="imageStorageSaving || imageStorageTesting"
                    data-testid="image-storage-save"
                    @click="saveImageStorageConfig"
                  >
                    <svg
                      v-if="imageStorageSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      imageStorageSaving
                        ? t("common.saving")
                        : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Legacy per-user usage visibility setting is no longer exposed. -->

        </div>
        <!-- /Tab: Gateway — Scheduling and Services -->

        <!-- Tab: GPT -->
        <div v-show="activeTab === 'gpt'" class="space-y-6" data-testid="gpt-settings">
          <!-- Codex Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.gatewayForwarding.codexHardeningTitle") }}
              </h2>
            </div>
            <div class="p-6 space-y-4">
                <div>
                  <h3 class="text-base font-semibold text-gray-900 dark:text-white">
                    {{ t("admin.settings.gatewayForwarding.codexClientRestrictionTitle") }}
                  </h3>
                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.codexHardeningDesc") }}
                  </p>
                </div>
                <div class="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.gatewayForwarding.minCodexVersion") }}
                    </label>
                    <input
                      v-model="form.min_codex_version"
                      type="text"
                      class="input w-full font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.minCodexVersionPlaceholder',
                        )
                      "
                    />
                  </div>
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.gatewayForwarding.maxCodexVersion") }}
                    </label>
                    <input
                      v-model="form.max_codex_version"
                      type="text"
                      class="input w-full font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.maxCodexVersionPlaceholder',
                        )
                      "
                    />
                  </div>
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.gatewayForwarding.codexVersionHint") }}
                </p>

                <div>
                  <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
                    {{ t("admin.settings.gatewayForwarding.codexFingerprintSignals") }}
                  </label>
                  <p class="mb-2 mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.codexFingerprintSignalsDesc") }}
                  </p>
                  <div
                    v-for="(row, i) in codexFingerprintRows"
                    :key="`codex-fp-${i}`"
                    class="mb-2 flex items-center gap-2"
                  >
                    <select v-model="row.type" class="input w-32 text-sm">
                      <option value="header_exact">{{ t("admin.settings.gatewayForwarding.codexFpTypeHeaderExact") }}</option>
                      <option value="header_prefix">{{ t("admin.settings.gatewayForwarding.codexFpTypeHeaderPrefix") }}</option>
                      <option value="body_path">{{ t("admin.settings.gatewayForwarding.codexFpTypeBodyPath") }}</option>
                    </select>
                    <input
                      v-model="row.match"
                      type="text"
                      class="input flex-1 font-mono text-sm"
                      :placeholder="t('admin.settings.gatewayForwarding.codexFpMatchPlaceholder')"
                    />
                    <label class="flex shrink-0 items-center gap-1 text-xs text-gray-600 dark:text-gray-400">
                      <input v-model="row.required" type="checkbox" />
                      {{ t("admin.settings.gatewayForwarding.codexFpRequired") }}
                    </label>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm shrink-0 text-red-600 hover:text-red-700 dark:text-red-400"
                      @click="removeCodexFingerprintRow(i)"
                    >
                      {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
                    </button>
                  </div>
                  <button type="button" class="btn btn-secondary btn-sm" @click="addCodexFingerprintRow">
                    {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
                  </button>
                  <p
                    v-if="codexFingerprintNoRequired"
                    class="mt-2 text-xs text-amber-600 dark:text-amber-500"
                  >
                    {{ t("admin.settings.gatewayForwarding.codexFingerprintNoRequiredWarn") }}
                  </p>
                </div>

                <div class="flex items-center justify-between">
                  <div class="pr-4">
                    <label
                      class="block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t("admin.settings.gatewayForwarding.codexAllowAppServer")
                      }}
                    </label>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.gatewayForwarding.codexAllowAppServerDesc",
                        )
                      }}
                    </p>
                  </div>
                  <Toggle
                    v-model="form.codex_cli_only_allow_app_server_clients"
                  />
                </div>

                <div>
                  <label
                    class="block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.codexBlacklist") }}
                  </label>
                  <p class="mb-2 mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.codexBlacklistDesc") }}
                  </p>
                  <div
                    v-for="(row, i) in codexBlacklistRows"
                    :key="`codex-bl-${i}`"
                    class="mb-2 flex gap-2"
                  >
                    <input
                      v-model="row.originator"
                      type="text"
                      class="input w-1/3 font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.codexOriginatorPlaceholder',
                        )
                      "
                    />
                    <input
                      v-model="row.uaContains"
                      type="text"
                      class="input flex-1 font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.codexUaContainsPlaceholder',
                        )
                      "
                    />
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm shrink-0 text-red-600 hover:text-red-700 dark:text-red-400"
                      @click="removeCodexBlacklistRow(i)"
                    >
                      {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
                    </button>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="addCodexBlacklistRow"
                  >
                    {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
                  </button>
                </div>

                <div>
                  <label
                    class="block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.codexWhitelist") }}
                  </label>
                  <p class="mb-2 mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.codexWhitelistDesc") }}
                  </p>
                  <div
                    v-for="(row, i) in codexWhitelistRows"
                    :key="`codex-wl-${i}`"
                    class="mb-2 flex gap-2"
                  >
                    <input
                      v-model="row.originator"
                      type="text"
                      class="input w-1/3 font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.codexOriginatorPlaceholder',
                        )
                      "
                    />
                    <input
                      v-model="row.uaContains"
                      type="text"
                      class="input flex-1 font-mono text-sm"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.codexUaContainsPlaceholder',
                        )
                      "
                    />
                    <label
                      class="flex shrink-0 items-center gap-1 text-xs text-gray-600 dark:text-gray-400"
                      :title="
                        t(
                          'admin.settings.gatewayForwarding.codexWhitelistSkipFingerprintTooltip',
                        )
                      "
                    >
                      <input
                        v-model="row.skipEngineFingerprint"
                        type="checkbox"
                      />
                      {{
                        t(
                          'admin.settings.gatewayForwarding.codexWhitelistSkipFingerprint',
                        )
                      }}
                    </label>
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm shrink-0 text-red-600 hover:text-red-700 dark:text-red-400"
                      @click="removeCodexWhitelistRow(i)"
                    >
                      {{ t("admin.settings.gatewayForwarding.codexRemoveRow") }}
                    </button>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="addCodexWhitelistRow"
                  >
                    {{ t("admin.settings.gatewayForwarding.codexAddRow") }}
                  </button>
                </div>
            </div>
          </div>

          <!-- GPT Scheduling Settings -->
          <div class="card">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.scheduling.gptTitle") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.scheduling.gptDescription") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <div
                v-if="!form.openai_advanced_scheduler_enabled"
                class="flex items-center justify-between"
              >
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.lowRatePriorityTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t("admin.settings.openaiExperimentalScheduler.lowRatePriorityDescription")
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.openai_low_upstream_rate_priority_enabled"
                  data-testid="openai-low-rate-priority-toggle"
                />
              </div>

              <div
                v-if="!form.openai_advanced_scheduler_enabled && form.openai_low_upstream_rate_priority_enabled"
                class="flex flex-col items-stretch gap-3 border-t border-gray-100 pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6 dark:border-dark-700"
              >
                <div class="min-w-0">
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                    for="openai-oauth-scheduling-rate-multiplier"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.openaiExperimentalScheduler.oauthRatePriorityDescription") }}
                  </p>
                </div>
                <div class="relative w-full shrink-0 sm:w-32">
                  <input
                    id="openai-oauth-scheduling-rate-multiplier"
                    v-model.number="form.openai_oauth_scheduling_rate_multiplier"
                    class="input pr-8"
                    data-testid="openai-oauth-scheduling-rate-multiplier"
                    min="0"
                    required
                    step="0.01"
                    type="number"
                  />
                  <span
                    class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400"
                  >x</span>
                </div>
              </div>

              <div class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.title") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t("admin.settings.openaiExperimentalScheduler.description")
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.openai_advanced_scheduler_enabled"
                  data-testid="openai-advanced-scheduler-toggle"
                />
              </div>

              <div
                v-if="form.openai_advanced_scheduler_enabled"
                class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
              >
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.stickyWeightedTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t("admin.settings.openaiExperimentalScheduler.stickyWeightedDescription")
                    }}
                  </p>
                </div>
                <Toggle v-model="form.openai_advanced_scheduler_sticky_weighted_enabled" />
              </div>

              <div
                v-if="form.openai_advanced_scheduler_enabled"
                class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
              >
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.subscriptionPriorityTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t("admin.settings.openaiExperimentalScheduler.subscriptionPriorityDescription")
                    }}
                  </p>
                </div>
                <Toggle v-model="form.openai_advanced_scheduler_subscription_priority_enabled" />
              </div>

              <div
                v-if="form.openai_advanced_scheduler_enabled"
                class="flex flex-col items-stretch gap-3 border-t border-gray-100 pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6 dark:border-dark-700"
              >
                <div class="min-w-0">
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                    for="openai-oauth-scheduling-rate-multiplier"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.openaiExperimentalScheduler.oauthRateWeightedDescription") }}
                  </p>
                </div>
                <div class="relative w-full shrink-0 sm:w-32">
                  <input
                    id="openai-oauth-scheduling-rate-multiplier"
                    v-model.number="form.openai_oauth_scheduling_rate_multiplier"
                    class="input pr-8"
                    data-testid="openai-oauth-scheduling-rate-multiplier"
                    min="0"
                    required
                    step="0.01"
                    type="number"
                  />
                  <span
                    class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400"
                  >x</span>
                </div>
              </div>

              <div
                v-if="form.openai_advanced_scheduler_enabled"
                class="border-t border-gray-100 pt-5 dark:border-dark-700"
              >
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.openaiExperimentalScheduler.weightsTitle") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t("admin.settings.openaiExperimentalScheduler.weightsDescription")
                    }}
                  </p>
                </div>

                <div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
                  <label
                    v-for="field in openAIAdvancedSchedulerWeightFields"
                    :key="field.key"
                    class="block"
                  >
                    <span class="text-xs font-medium text-gray-600 dark:text-gray-400">
                      {{ field.label }}
                    </span>
                    <input
                      v-model="form[field.key]"
                      class="input mt-1"
                      inputmode="decimal"
                      :placeholder="field.placeholder"
                      type="text"
                    />
                  </label>
                </div>
              </div>
            </div>
          </div>

          <!-- GPT Forwarding Behavior -->
          <div class="card">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.gatewayForwarding.gptTitle") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.gatewayForwarding.gptDescription") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- OpenAI Responses 首 token 统计 -->
              <div class="border-b border-gray-100 pb-5 dark:border-dark-700 md:col-span-2">
                <label
                  for="openai-ttft-mode"
                  class="text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.gatewayForwarding.openaiTTFTMode") }}
                </label>
                <select
                  id="openai-ttft-mode"
                  v-model="form.openai_ttft_mode"
                  class="input mt-2 w-full"
                  data-testid="openai-ttft-mode"
                >
                  <option value="semantic">
                    {{ t("admin.settings.gatewayForwarding.openaiTTFTModeSemantic") }}
                  </option>
                  <option value="visible">
                    {{ t("admin.settings.gatewayForwarding.openaiTTFTModeVisible") }}
                  </option>
                </select>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.gatewayForwarding.openaiTTFTModeHint") }}
                </p>
              </div>

              <!-- OpenAI Codex account-local device session identity -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.accountLocalDeviceIdentity",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.accountLocalDeviceIdentityHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_openai_account_local_device_identity"
                  data-testid="account-local-device-identity-toggle"
                />
              </div>

              <!-- GPT/Codex upstream identity headers -->
              <div
                class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60"
                data-testid="openai-codex-upstream-identity-settings"
              >
                <div>
                  <h3
                    class="text-sm font-semibold text-gray-900 dark:text-white"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexIdentityTitle",
                      )
                    }}
                  </h3>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexIdentityHint",
                      )
                    }}
                  </p>
                </div>

                <div class="mt-4 space-y-4">
                  <!-- Originator -->
                  <div>
                    <label
                      for="openai-codex-originator"
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexOriginator",
                        )
                      }}
                    </label>
                    <Select
                      :model-value="form.openai_codex_originator"
                      :options="codexOriginatorOptions"
                      searchable
                      class="mb-2"
                      data-testid="openai-codex-originator-presets"
                      :aria-label="t('admin.settings.gatewayForwarding.openaiCodexOriginatorPresets')"
                      :placeholder="t('admin.settings.gatewayForwarding.openaiCodexOriginatorPresets')"
                      @update:model-value="applyCodexOriginatorPreset"
                    />
                    <input
                      id="openai-codex-originator"
                      v-model="form.openai_codex_originator"
                      type="text"
                      class="input w-full font-mono text-sm"
                      data-testid="openai-codex-originator"
                      maxlength="64"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.openaiCodexOriginatorPlaceholder',
                        )
                      "
                    />
                    <p
                      v-if="form.openai_codex_originator_default"
                      class="mt-1.5 break-all font-mono text-xs text-gray-500 dark:text-gray-400"
                      data-testid="openai-codex-originator-default"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexDefaultValue",
                          {
                            value: form.openai_codex_originator_default,
                          },
                        )
                      }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexOriginatorHint",
                        )
                      }}
                    </p>
                  </div>

                  <!-- User-Agent -->
                  <div>
                    <label
                      for="openai-codex-user-agent"
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexUserAgent",
                        )
                      }}
                    </label>
                    <Select
                      :model-value="form.openai_codex_user_agent"
                      :options="codexUserAgentOptions"
                      searchable
                      class="mb-2"
                      data-testid="openai-codex-user-agent-presets"
                      :aria-label="t('admin.settings.gatewayForwarding.openaiCodexUserAgentPresets')"
                      :placeholder="t('admin.settings.gatewayForwarding.openaiCodexUserAgentPresets')"
                      @update:model-value="applyCodexUserAgentPreset"
                    />
                    <input
                      id="openai-codex-user-agent"
                      v-model="form.openai_codex_user_agent"
                      type="text"
                      class="input w-full font-mono text-sm"
                      data-testid="openai-codex-user-agent"
                      maxlength="512"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.openaiCodexUserAgentPlaceholder',
                        )
                      "
                    />
                    <p
                      v-if="form.openai_codex_user_agent_default"
                      class="mt-1.5 break-all font-mono text-xs text-gray-500 dark:text-gray-400"
                      data-testid="openai-codex-user-agent-default"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexDefaultValue",
                          {
                            value: form.openai_codex_user_agent_default,
                          },
                        )
                      }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexUserAgentHint",
                        )
                      }}
                    </p>
                  </div>

                  <!-- Version -->
                  <div>
                    <label
                      for="openai-codex-version"
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexClientVersion",
                        )
                      }}
                    </label>
                    <div class="mb-2 grid gap-2 sm:grid-cols-2">
                      <Select
                        :model-value="form.openai_codex_client_version_mode"
                        :options="codexVersionModeOptions"
                        data-testid="openai-codex-version-mode"
                        :aria-label="t('admin.settings.gatewayForwarding.openaiCodexVersionMode')"
                        @update:model-value="setCodexVersionMode"
                      />
                      <Select
                        :model-value="form.openai_codex_client_version"
                        :options="codexVersionOptions"
                        :loading="codexVersionsLoading"
                        searchable
                        data-testid="openai-codex-version-history"
                        :aria-label="t('admin.settings.gatewayForwarding.openaiCodexVersionHistory')"
                        :placeholder="t('admin.settings.gatewayForwarding.openaiCodexVersionHistory')"
                        @update:model-value="selectCodexVersion"
                      />
                    </div>
                    <input
                      id="openai-codex-version"
                      v-model="form.openai_codex_client_version"
                      @input="onCodexVersionInput"
                      type="text"
                      class="input w-full font-mono text-sm"
                      data-testid="openai-codex-version"
                      maxlength="64"
                      :placeholder="
                        t(
                          'admin.settings.gatewayForwarding.openaiCodexClientVersionPlaceholder',
                        )
                      "
                    />
                    <div class="mt-2 flex flex-wrap gap-2">
                      <button
                        type="button"
                        class="btn btn-secondary text-xs"
                        :disabled="codexSyncLoading || saving"
                        data-testid="openai-codex-sync"
                        @click="syncCodexVersion"
                      >
                        {{ t(codexSyncLoading ? 'admin.settings.gatewayForwarding.openaiCodexVersionSyncing' : 'admin.settings.gatewayForwarding.openaiCodexVersionSyncNow') }}
                      </button>
                      <button
                        type="button"
                        class="btn btn-secondary text-xs"
                        :disabled="codexVersionsLoading || codexSyncLoading"
                        data-testid="openai-codex-versions-refresh"
                        @click="loadCodexVersions()"
                      >
                        {{ t(codexVersionsLoading ? 'common.loading' : 'admin.settings.gatewayForwarding.openaiCodexVersionRefresh') }}
                      </button>
                      <button
                        v-if="codexVersionsNextPage !== null"
                        type="button"
                        class="btn btn-secondary text-xs"
                        :disabled="codexVersionsLoading || codexSyncLoading"
                        data-testid="openai-codex-versions-more"
                        @click="loadCodexVersions(codexVersionsNextPage)"
                      >
                        {{ t('admin.settings.gatewayForwarding.openaiCodexVersionLoadMore') }}
                      </button>
                    </div>
                    <p
                      v-if="codexVersionsError"
                      role="alert"
                      class="mt-2 text-xs text-red-600 dark:text-red-400"
                      data-testid="openai-codex-versions-error"
                    >{{ codexVersionsError }}</p>
                    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.settings.gatewayForwarding.openaiCodexVersionSource') }}
                      <a class="text-primary-600 hover:underline" href="https://github.com/openai/codex/releases" target="_blank" rel="noopener noreferrer">openai/codex</a>
                      <span v-if="codexLatestVersion"> · {{ t('admin.settings.gatewayForwarding.openaiCodexVersionLatest', { version: codexLatestVersion }) }}</span>
                    </p>
                    <p
                      v-if="form.openai_codex_client_version_default"
                      class="mt-1.5 break-all font-mono text-xs text-gray-500 dark:text-gray-400"
                      data-testid="openai-codex-version-default"
                    >
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexDefaultValue",
                          {
                            value: form.openai_codex_client_version_default,
                          },
                        )
                      }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{
                        t(
                          "admin.settings.gatewayForwarding.openaiCodexClientVersionHint",
                        )
                      }}
                    </p>
                  </div>
                </div>
              </div>

              <!-- Codex 版本号自动同步 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexVersionAutoSync",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.openaiCodexVersionAutoSyncHint",
                      )
                    }}
                  </p>
                  <p
                    v-if="codexSyncedVersionLabel"
                    class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                  >
                    {{ codexSyncedVersionLabel }}
                  </p>
                </div>
                <Toggle v-model="form.openai_codex_version_auto_sync_enabled" />
              </div>

              <!-- Claude Code 客户端版本号 -->
              <div>
                <label
                  for="claude-code-client-version"
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeCodeClientVersion",
                    )
                  }}
                </label>
                <input
                  id="claude-code-client-version"
                  v-model="form.claude_code_client_version"
                  type="text"
                  class="input w-full font-mono text-sm"
                  data-testid="claude-code-client-version"
                  maxlength="64"
                  placeholder="2.1.280"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeCodeClientVersionHint",
                    )
                  }}
                </p>
              </div>

              <!-- Claude Code 版本号自动同步 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeCodeVersionAutoSync",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeCodeVersionAutoSyncHint",
                      )
                    }}
                  </p>
                  <p
                    v-if="claudeSyncedVersionLabel"
                    class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                    data-testid="claude-code-client-version-synced"
                  >
                    {{ claudeSyncedVersionLabel }}
                  </p>
                </div>
                <Toggle
                  v-model="form.claude_code_version_auto_sync_enabled"
                  data-testid="claude-code-version-auto-sync"
                />
              </div>
            </div>
          </div>

          <!-- OpenAI Fast/Flex Policy Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.openaiFastPolicy.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.openaiFastPolicy.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Empty state -->
              <div
                v-if="openaiFastPolicyForm.rules.length === 0"
                class="rounded-lg border border-dashed border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
              >
                {{ t("admin.settings.openaiFastPolicy.empty") }}
              </div>

              <!-- Rule Cards -->
              <div
                v-for="(rule, ruleIndex) in openaiFastPolicyForm.rules"
                :key="ruleIndex"
                class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
              >
                <div class="mb-3 flex items-center justify-between">
                  <span
                    class="text-sm font-medium text-gray-900 dark:text-white"
                  >
                    {{
                      t("admin.settings.openaiFastPolicy.ruleHeader", {
                        index: ruleIndex + 1,
                      })
                    }}
                  </span>
                  <button
                    type="button"
                    @click="removeOpenAIFastPolicyRule(ruleIndex)"
                    class="rounded p-1 text-red-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                    :title="t('admin.settings.openaiFastPolicy.removeRule')"
                  >
                    <svg
                      class="h-4 w-4"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M6 18L18 6M6 6l12 12"
                      />
                    </svg>
                  </button>
                </div>

                <div
                  class="mb-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-gray-400"
                  :data-testid="`openai-fast-policy-summary-${ruleIndex}`"
                >
                  <span class="font-medium text-gray-700 dark:text-gray-300">
                    {{
                      t(
                        hasOpenAIFastPolicyTargetModels(rule)
                          ? "admin.settings.openaiFastPolicy.summaryTargetModels"
                          : "admin.settings.openaiFastPolicy.summaryAllModels",
                      )
                    }}
                  </span>
                  <span aria-hidden="true">→</span>
                  <span
                    class="inline-flex items-center rounded bg-primary-50 px-2 py-0.5 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                  >
                    {{ openaiFastPolicyActionSummary(rule.action) }}
                  </span>
                  <template v-if="hasOpenAIFastPolicyTargetModels(rule)">
                    <span aria-hidden="true">·</span>
                    <span class="font-medium text-gray-700 dark:text-gray-300">
                      {{
                        t(
                          "admin.settings.openaiFastPolicy.summaryOtherModels",
                        )
                      }}
                    </span>
                    <span aria-hidden="true">→</span>
                    <span
                      class="inline-flex items-center rounded bg-gray-100 px-2 py-0.5 font-medium text-gray-700 dark:bg-dark-600 dark:text-gray-300"
                    >
                      {{
                        openaiFastPolicyActionSummary(
                          rule.fallback_action || "pass",
                        )
                      }}
                    </span>
                  </template>
                </div>

                <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
                  <!-- Service Tier -->
                  <div>
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.openaiFastPolicy.serviceTier") }}
                    </label>
                    <Select
                      :modelValue="rule.service_tier"
                      @update:modelValue="
                        rule.service_tier = $event as
                          | 'all'
                          | 'priority'
                          | 'flex'
                      "
                      :options="openaiFastPolicyTierOptions"
                    />
                  </div>

                  <!-- Action -->
                  <div>
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.openaiFastPolicy.action") }}
                    </label>
                    <Select
                      :modelValue="rule.action"
                      @update:modelValue="
                        rule.action = $event as
                          | 'pass'
                          | 'filter'
                          | 'block'
                          | 'force_priority'
                      "
                      :options="openaiFastPolicyActionOptions"
                    />
                  </div>

                  <!-- Scope -->
                  <div>
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.openaiFastPolicy.scope") }}
                    </label>
                    <Select
                      :modelValue="rule.scope"
                      @update:modelValue="
                        rule.scope = $event as
                          | 'all'
                          | 'oauth'
                          | 'apikey'
                          | 'bedrock'
                      "
                      :options="openaiFastPolicyScopeOptions"
                    />
                  </div>
                </div>

                <!-- Error Message (only when action=block) -->
                <div v-if="rule.action === 'block'" class="mt-3">
                  <label
                    class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                  >
                    {{ t("admin.settings.openaiFastPolicy.errorMessage") }}
                  </label>
                  <input
                    v-model="rule.error_message"
                    type="text"
                    class="input"
                    :placeholder="
                      t(
                        'admin.settings.openaiFastPolicy.errorMessagePlaceholder',
                      )
                    "
                  />
                  <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                    {{ t("admin.settings.openaiFastPolicy.errorMessageHint") }}
                  </p>
                </div>

                <!-- Target Models -->
                <div
                  class="mt-3"
                  role="group"
                  :aria-labelledby="`openai-fast-policy-models-label-${ruleIndex}`"
                  :aria-describedby="`openai-fast-policy-models-hint-${ruleIndex}`"
                >
                  <label
                    :id="`openai-fast-policy-models-label-${ruleIndex}`"
                    class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                  >
                    {{ t("admin.settings.openaiFastPolicy.modelWhitelist") }}
                  </label>
                  <p
                    :id="`openai-fast-policy-models-hint-${ruleIndex}`"
                    class="mb-2 text-xs text-gray-400 dark:text-gray-500"
                  >
                    {{
                      t("admin.settings.openaiFastPolicy.modelWhitelistHint")
                    }}
                  </p>
                  <div
                    v-for="(_, patternIdx) in rule.model_whitelist || []"
                    :key="patternIdx"
                    class="mb-1.5 flex items-center gap-2"
                  >
                    <input
                      v-model="rule.model_whitelist![patternIdx]"
                      type="text"
                      class="input input-sm flex-1"
                      :placeholder="
                        t(
                          'admin.settings.openaiFastPolicy.modelPatternPlaceholder',
                        )
                      "
                    />
                    <button
                      type="button"
                      @click="
                        removeOpenAIFastPolicyModelPattern(rule, patternIdx)
                      "
                      class="shrink-0 rounded p-1 text-red-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                    >
                      <svg
                        class="h-4 w-4"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="M6 18L18 6M6 6l12 12"
                        />
                      </svg>
                    </button>
                  </div>
                  <button
                    type="button"
                    @click="addOpenAIFastPolicyModelPattern(rule)"
                    class="mb-2 inline-flex items-center gap-1 text-xs text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                  >
                    <svg
                      class="h-3.5 w-3.5"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M12 4v16m8-8H4"
                      />
                    </svg>
                    {{ t("admin.settings.openaiFastPolicy.addModelPattern") }}
                  </button>
                </div>

                <!-- Other Models Action (only when target models are non-empty) -->
                <div
                  v-if="hasOpenAIFastPolicyTargetModels(rule)"
                  class="mt-3"
                >
                  <label
                    class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                  >
                    {{ t("admin.settings.openaiFastPolicy.fallbackAction") }}
                  </label>
                  <Select
                    :modelValue="rule.fallback_action || 'pass'"
                    @update:modelValue="
                      rule.fallback_action = $event as
                        | 'pass'
                        | 'filter'
                        | 'block'
                        | 'force_priority'
                    "
                    :options="openaiFastPolicyActionOptions"
                  />
                  <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                    {{
                      t("admin.settings.openaiFastPolicy.fallbackActionHint")
                    }}
                  </p>
                  <div v-if="rule.fallback_action === 'block'" class="mt-2">
                    <input
                      v-model="rule.fallback_error_message"
                      type="text"
                      class="input"
                      :placeholder="
                        t(
                          'admin.settings.openaiFastPolicy.fallbackErrorMessagePlaceholder',
                        )
                      "
                    />
                  </div>
                </div>
              </div>

              <!-- Add Rule Button -->
              <div>
                <button
                  type="button"
                  @click="addOpenAIFastPolicyRule"
                  class="btn btn-secondary btn-sm inline-flex items-center gap-1"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 4v16m8-8H4"
                    />
                  </svg>
                  {{ t("admin.settings.openaiFastPolicy.addRule") }}
                </button>
                <p class="mt-2 text-xs text-gray-400 dark:text-gray-500">
                  {{ t("admin.settings.openaiFastPolicy.saveHint") }}
                </p>
              </div>
            </div>
          </div>
        </div>
        <!-- /Tab: GPT -->

        <!-- Tab: Claude -->
        <div v-show="activeTab === 'claude'" class="space-y-6" data-testid="claude-settings">
          <!-- Claude Code Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.claudeCode.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.claudeCode.description") }}
              </p>
            </div>
            <div class="p-6">
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.claudeCode.minVersion") }}
                </label>
                <input
                  v-model="form.min_claude_code_version"
                  type="text"
                  class="input max-w-xs font-mono text-sm"
                  :placeholder="
                    t('admin.settings.claudeCode.minVersionPlaceholder')
                  "
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.claudeCode.minVersionHint") }}
                </p>
              </div>
              <div class="mt-4">
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.claudeCode.maxVersion") }}
                </label>
                <input
                  v-model="form.max_claude_code_version"
                  type="text"
                  class="input max-w-xs font-mono text-sm"
                  :placeholder="
                    t('admin.settings.claudeCode.maxVersionPlaceholder')
                  "
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.claudeCode.maxVersionHint") }}
                </p>
              </div>
            </div>
          </div>

          <!-- Claude Forwarding Behavior -->
          <div class="card">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.gatewayForwarding.claudeTitle") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.gatewayForwarding.claudeDescription") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Fingerprint Unification -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.fingerprintUnification",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.fingerprintUnificationHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.enable_fingerprint_unification" />
              </div>

              <!-- Metadata Passthrough -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t("admin.settings.gatewayForwarding.metadataPassthrough")
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.metadataPassthroughHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.enable_metadata_passthrough" />
              </div>

              <!-- CCH Signing -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.gatewayForwarding.cchSigning") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.gatewayForwarding.cchSigningHint") }}
                  </p>
                </div>
                <Toggle v-model="form.enable_cch_signing" />
              </div>

              <!-- Claude OAuth System Prompt Injection -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjection",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjectionHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_claude_oauth_system_prompt_injection"
                />
              </div>

              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocks",
                    )
                  }}
                </label>
                <div class="space-y-3">
                  <div
                    v-for="(block, index) in claudeOAuthSystemPromptBlocks"
                    :key="block.id"
                    class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60"
                  >
                    <div
                      :class="[
                        'flex flex-wrap items-center justify-between gap-3',
                        block.expanded && 'mb-3',
                      ]"
                    >
                      <div class="min-w-0">
                        <div
                          class="text-sm font-medium text-gray-900 dark:text-white"
                        >
                          {{
                            t(
                              "admin.settings.gatewayForwarding.systemBlockTitle",
                              { index: index + 1 },
                            )
                          }}
                        </div>
                        <div
                          class="mt-0.5 text-xs text-gray-500 dark:text-gray-400"
                        >
                          {{ getClaudeOAuthPresetLabel(block.preset) }}
                        </div>
                      </div>
                      <div class="flex items-center gap-2">
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm px-2"
                          :title="
                            block.expanded
                              ? t(
                                  'admin.settings.gatewayForwarding.systemBlockHide',
                                )
                              : t(
                                  'admin.settings.gatewayForwarding.systemBlockShow',
                                )
                          "
                          :aria-label="
                            block.expanded
                              ? t(
                                  'admin.settings.gatewayForwarding.systemBlockHide',
                                )
                              : t(
                                  'admin.settings.gatewayForwarding.systemBlockShow',
                                )
                          "
                          @click="toggleClaudeOAuthSystemPromptBlock(index)"
                        >
                          <Icon
                            :name="block.expanded ? 'eyeOff' : 'eye'"
                            size="xs"
                          />
                        </button>
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm px-2"
                          :disabled="index === 0"
                          @click="moveClaudeOAuthSystemPromptBlock(index, -1)"
                        >
                          <Icon name="arrowUp" size="xs" />
                        </button>
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm px-2"
                          :disabled="
                            index === claudeOAuthSystemPromptBlocks.length - 1
                          "
                          @click="moveClaudeOAuthSystemPromptBlock(index, 1)"
                        >
                          <Icon name="arrowDown" size="xs" />
                        </button>
                        <Toggle v-model="block.enabled" />
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm px-2 text-red-600 hover:text-red-700 dark:text-red-400"
                          @click="removeClaudeOAuthSystemPromptBlock(index)"
                        >
                          <Icon name="trash" size="xs" />
                        </button>
                      </div>
                    </div>

                    <div v-show="block.expanded">
                      <div class="grid gap-3 md:grid-cols-2">
                        <div>
                          <label
                            class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300"
                          >
                            {{
                              t(
                                "admin.settings.gatewayForwarding.systemBlockPreset",
                              )
                            }}
                          </label>
                          <Select
                            v-model="block.preset"
                            :options="claudeOAuthSystemPromptPresetOptions"
                            @change="
                              (value) =>
                                applyClaudeOAuthSystemPromptPreset(index, value)
                            "
                          />
                        </div>
                        <div>
                          <label
                            class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300"
                          >
                            {{
                              t(
                                "admin.settings.gatewayForwarding.systemBlockType",
                              )
                            }}
                          </label>
                          <Select
                            v-model="block.type"
                            :options="claudeOAuthSystemPromptBlockTypeOptions"
                          />
                        </div>
                      </div>

                      <div class="mt-3">
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300"
                        >
                          {{ t("admin.settings.gatewayForwarding.systemBlockText") }}
                        </label>
                        <textarea
                          v-model="block.text"
                          rows="6"
                          class="input w-full resize-y font-mono text-xs leading-5"
                          @input="markClaudeOAuthSystemPromptBlockCustom(block)"
                        />
                      </div>

                      <div
                        class="mt-3 grid gap-3 md:grid-cols-[minmax(0,1fr)_160px]"
                      >
                        <div class="flex items-center justify-between gap-4">
                          <div>
                            <label
                              class="text-xs font-medium text-gray-600 dark:text-gray-300"
                            >
                              {{
                                t(
                                  "admin.settings.gatewayForwarding.systemBlockCacheControl",
                                )
                              }}
                            </label>
                          </div>
                          <Toggle v-model="block.cacheControlEnabled" />
                        </div>
                        <div v-if="block.cacheControlEnabled">
                          <Select
                            v-model="block.cacheControlTTL"
                            :options="claudeOAuthSystemPromptCacheTTLOptions"
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <div class="mt-3 flex flex-wrap gap-2">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="addClaudeOAuthSystemPromptBlock"
                  >
                    <Icon name="plus" size="xs" />
                    {{ t("admin.settings.gatewayForwarding.addSystemBlock") }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="resetClaudeOAuthSystemPromptBlocks"
                  >
                    <Icon name="refresh" size="xs" />
                    {{
                      t("admin.settings.gatewayForwarding.resetSystemBlocks")
                    }}
                  </button>
                </div>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocksHint",
                    )
                  }}
                </p>
              </div>

              <!-- Anthropic Cache TTL 1h Injection -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.anthropicCacheTTL1hInjection",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.anthropicCacheTTL1hInjectionHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_anthropic_cache_ttl_1h_injection"
                />
              </div>

              <!-- messages cache_control 改写 -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.rewriteMessageCacheControl",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.rewriteMessageCacheControlHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle v-model="form.rewrite_message_cache_control" />
              </div>

              <!-- 客户端 dateline 归一化（仅 Anthropic OAuth/SetupToken） -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{
                      t(
                        "admin.settings.gatewayForwarding.clientDatelineNormalization",
                      )
                    }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{
                      t(
                        "admin.settings.gatewayForwarding.clientDatelineNormalizationHint",
                      )
                    }}
                  </p>
                </div>
                <Toggle
                  v-model="form.enable_client_dateline_normalization"
                />
              </div>
            </div>
          </div>

          <!-- Request Rectifier Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.rectifier.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.rectifier.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Loading State -->
              <div
                v-if="rectifierLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <!-- Master Toggle -->
                <div class="flex items-center justify-between">
                  <div>
                    <label class="font-medium text-gray-900 dark:text-white">{{
                      t("admin.settings.rectifier.enabled")
                    }}</label>
                    <p class="text-sm text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.rectifier.enabledHint") }}
                    </p>
                  </div>
                  <Toggle v-model="rectifierForm.enabled" />
                </div>

                <!-- Sub-toggles (only show when master is enabled) -->
                <div
                  v-if="rectifierForm.enabled"
                  class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <!-- Thinking Signature Rectifier -->
                  <div class="flex items-center justify-between">
                    <div>
                      <label
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                        >{{
                          t("admin.settings.rectifier.thinkingSignature")
                        }}</label
                      >
                      <p class="text-xs text-gray-500 dark:text-gray-400">
                        {{
                          t("admin.settings.rectifier.thinkingSignatureHint")
                        }}
                      </p>
                    </div>
                    <Toggle
                      v-model="rectifierForm.thinking_signature_enabled"
                    />
                  </div>

                  <!-- Thinking Budget Rectifier -->
                  <div class="flex items-center justify-between">
                    <div>
                      <label
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                        >{{
                          t("admin.settings.rectifier.thinkingBudget")
                        }}</label
                      >
                      <p class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.rectifier.thinkingBudgetHint") }}
                      </p>
                    </div>
                    <Toggle v-model="rectifierForm.thinking_budget_enabled" />
                  </div>

                  <!-- API Key Signature Rectifier -->
                  <div class="flex items-center justify-between">
                    <div>
                      <label
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                        >{{
                          t("admin.settings.rectifier.apikeySignature")
                        }}</label
                      >
                      <p class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.rectifier.apikeySignatureHint") }}
                      </p>
                    </div>
                    <Toggle v-model="rectifierForm.apikey_signature_enabled" />
                  </div>

                  <!-- Custom Patterns (only when apikey_signature_enabled) -->
                  <div
                    v-if="rectifierForm.apikey_signature_enabled"
                    class="ml-4 space-y-3 border-l-2 border-gray-200 pl-4 dark:border-dark-600"
                  >
                    <div>
                      <label
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                        >{{
                          t("admin.settings.rectifier.apikeyPatterns")
                        }}</label
                      >
                      <p class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.settings.rectifier.apikeyPatternsHint") }}
                      </p>
                    </div>
                    <div
                      v-for="(
                        _, index
                      ) in rectifierForm.apikey_signature_patterns"
                      :key="index"
                      class="flex items-center gap-2"
                    >
                      <input
                        v-model="rectifierForm.apikey_signature_patterns[index]"
                        type="text"
                        class="input input-sm flex-1"
                        :placeholder="
                          t('admin.settings.rectifier.apikeyPatternPlaceholder')
                        "
                      />
                      <button
                        type="button"
                        @click="
                          rectifierForm.apikey_signature_patterns.splice(
                            index,
                            1,
                          )
                        "
                        class="btn btn-ghost btn-xs text-red-500 hover:text-red-700"
                      >
                        <svg
                          class="h-4 w-4"
                          fill="none"
                          stroke="currentColor"
                          viewBox="0 0 24 24"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            stroke-width="2"
                            d="M6 18L18 6M6 6l12 12"
                          />
                        </svg>
                      </button>
                    </div>
                    <button
                      type="button"
                      @click="rectifierForm.apikey_signature_patterns.push('')"
                      class="btn btn-ghost btn-xs text-primary-600 dark:text-primary-400"
                    >
                      + {{ t("admin.settings.rectifier.addPattern") }}
                    </button>
                  </div>
                </div>

                <!-- Save Button -->
                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveRectifierSettings"
                    :disabled="rectifierSaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="rectifierSaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      rectifierSaving ? t("common.saving") : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>
          <!-- Beta Policy Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.betaPolicy.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.betaPolicy.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Loading State -->
              <div
                v-if="betaPolicyLoading"
                class="flex items-center gap-2 text-gray-500"
              >
                <div
                  class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
                ></div>
                {{ t("common.loading") }}
              </div>

              <template v-else>
                <!-- Rule Cards -->
                <div
                  v-for="rule in betaPolicyForm.rules"
                  :key="rule.beta_token"
                  class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
                >
                  <div class="mb-3 flex items-center gap-2">
                    <span
                      class="text-sm font-medium text-gray-900 dark:text-white"
                    >
                      {{ getBetaDisplayName(rule.beta_token) }}
                    </span>
                    <span
                      class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-400"
                    >
                      {{ rule.beta_token }}
                    </span>
                  </div>

                  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <!-- Action -->
                    <div>
                      <label
                        class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                      >
                        {{ t("admin.settings.betaPolicy.action") }}
                      </label>
                      <Select
                        :modelValue="rule.action"
                        @update:modelValue="rule.action = $event as any"
                        :options="betaPolicyActionOptions"
                      />
                    </div>

                    <!-- Scope -->
                    <div>
                      <label
                        class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                      >
                        {{ t("admin.settings.betaPolicy.scope") }}
                      </label>
                      <Select
                        :modelValue="rule.scope"
                        @update:modelValue="rule.scope = $event as any"
                        :options="betaPolicyScopeOptions"
                      />
                    </div>
                  </div>

                  <!-- Error Message (only when action=block) -->
                  <div v-if="rule.action === 'block'" class="mt-3">
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.betaPolicy.errorMessage") }}
                    </label>
                    <input
                      v-model="rule.error_message"
                      type="text"
                      class="input"
                      :placeholder="
                        t('admin.settings.betaPolicy.errorMessagePlaceholder')
                      "
                    />
                    <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.betaPolicy.errorMessageHint") }}
                    </p>
                  </div>

                  <!-- Quick Presets (only for tokens with presets) -->
                  <div v-if="betaPresets[rule.beta_token]?.length" class="mt-3">
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.betaPolicy.quickPresets") }}
                    </label>
                    <div class="flex flex-wrap gap-2">
                      <button
                        v-for="preset in betaPresets[rule.beta_token]"
                        :key="preset.label"
                        type="button"
                        class="inline-flex items-center gap-1 rounded-md border border-primary-200 bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 transition-colors hover:bg-primary-100 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300 dark:hover:bg-primary-900/50"
                        @click="applyBetaPreset(rule, preset)"
                        :title="preset.description"
                      >
                        {{ preset.label }}
                      </button>
                    </div>
                  </div>

                  <!-- Model Whitelist -->
                  <div class="mt-3">
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.betaPolicy.modelWhitelist") }}
                    </label>
                    <p class="mb-2 text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.betaPolicy.modelWhitelistHint") }}
                    </p>
                    <!-- Existing patterns -->
                    <div
                      v-for="(_, index) in rule.model_whitelist || []"
                      :key="index"
                      class="mb-1.5 flex items-center gap-2"
                    >
                      <input
                        v-model="rule.model_whitelist![index]"
                        type="text"
                        class="input input-sm flex-1"
                        :placeholder="
                          t('admin.settings.betaPolicy.modelPatternPlaceholder')
                        "
                      />
                      <button
                        type="button"
                        @click="rule.model_whitelist!.splice(index, 1)"
                        class="shrink-0 rounded p-1 text-red-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                      >
                        <svg
                          class="h-4 w-4"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            d="M6 18L18 6M6 6l12 12"
                          />
                        </svg>
                      </button>
                    </div>
                    <!-- Add pattern button -->
                    <button
                      type="button"
                      @click="
                        if (!rule.model_whitelist) rule.model_whitelist = [];
                        rule.model_whitelist.push('');
                      "
                      class="mb-2 inline-flex items-center gap-1 text-xs text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                    >
                      <svg
                        class="h-3.5 w-3.5"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          d="M12 4v16m8-8H4"
                        />
                      </svg>
                      {{ t("admin.settings.betaPolicy.addModelPattern") }}
                    </button>
                    <!-- Common pattern chips -->
                    <div class="flex flex-wrap items-center gap-1.5">
                      <span class="text-xs text-gray-400 dark:text-gray-500"
                        >{{
                          t("admin.settings.betaPolicy.commonPatterns")
                        }}:</span
                      >
                      <button
                        v-for="pattern in commonModelPatterns"
                        :key="pattern"
                        type="button"
                        class="rounded border border-gray-200 px-2 py-0.5 text-xs text-gray-600 transition-colors hover:border-primary-300 hover:bg-primary-50 hover:text-primary-700 dark:border-dark-600 dark:text-gray-400 dark:hover:border-primary-700 dark:hover:bg-primary-900/30 dark:hover:text-primary-300"
                        @click="addQuickPattern(rule, pattern)"
                      >
                        {{ pattern }}
                      </button>
                    </div>
                  </div>

                  <!-- Fallback Action (only when model_whitelist is non-empty) -->
                  <div
                    v-if="
                      rule.model_whitelist && rule.model_whitelist.length > 0
                    "
                    class="mt-3"
                  >
                    <label
                      class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                    >
                      {{ t("admin.settings.betaPolicy.fallbackAction") }}
                    </label>
                    <Select
                      :modelValue="rule.fallback_action || 'pass'"
                      @update:modelValue="rule.fallback_action = $event as any"
                      :options="betaPolicyActionOptions"
                    />
                    <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                      {{ t("admin.settings.betaPolicy.fallbackActionHint") }}
                    </p>
                    <!-- Fallback Error Message (only when fallback_action=block) -->
                    <div v-if="rule.fallback_action === 'block'" class="mt-2">
                      <input
                        v-model="rule.fallback_error_message"
                        type="text"
                        class="input"
                        :placeholder="
                          t(
                            'admin.settings.betaPolicy.fallbackErrorMessagePlaceholder',
                          )
                        "
                      />
                      <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                        {{ t("admin.settings.betaPolicy.errorMessageHint") }}
                      </p>
                    </div>
                  </div>
                </div>

                <!-- Save Button -->
                <div
                  class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
                >
                  <button
                    type="button"
                    @click="saveBetaPolicySettings"
                    :disabled="betaPolicySaving"
                    class="btn btn-primary btn-sm"
                  >
                    <svg
                      v-if="betaPolicySaving"
                      class="mr-1 h-4 w-4 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        class="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        stroke-width="4"
                      ></circle>
                      <path
                        class="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    {{
                      betaPolicySaving ? t("common.saving") : t("common.save")
                    }}
                  </button>
                </div>
              </template>
            </div>
          </div>

          <!-- Web Search Emulation -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.webSearchEmulation.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.webSearchEmulation.description") }}
              </p>
            </div>
            <div class="space-y-5 p-6">
              <!-- Global Toggle -->
              <div class="flex items-center justify-between">
                <div>
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.webSearchEmulation.enabled") }}
                  </label>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.webSearchEmulation.enabledHint") }}
                  </p>
                </div>
                <Toggle v-model="webSearchConfig.enabled" />
              </div>

              <!-- Providers -->
              <div v-if="webSearchConfig.enabled" class="space-y-4">
                <div class="flex items-center justify-between">
                  <label
                    class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.webSearchEmulation.providers") }}
                  </label>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="addWebSearchProvider"
                  >
                    {{ t("admin.settings.webSearchEmulation.addProvider") }}
                  </button>
                </div>

                <div
                  v-if="webSearchConfig.providers.length === 0"
                  class="rounded-lg border border-dashed border-gray-300 p-4 text-center text-sm text-gray-400 dark:border-dark-600"
                >
                  {{ t("admin.settings.webSearchEmulation.noProviders") }}
                </div>

                <div
                  v-for="(provider, pIdx) in webSearchConfig.providers"
                  :key="pIdx"
                  class="rounded-lg border border-gray-200 dark:border-dark-600"
                >
                  <!-- Collapsible header -->
                  <div
                    class="flex cursor-pointer items-center justify-between px-4 py-3"
                    @click="toggleProviderExpand(pIdx)"
                  >
                    <div class="flex items-center gap-3">
                      <svg
                        class="h-4 w-4 text-gray-400 transition-transform"
                        :class="{ 'rotate-90': expandedProviders[pIdx] }"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          stroke-width="2"
                          d="M9 5l7 7-7 7"
                        />
                      </svg>
                      <Select
                        v-model="provider.type"
                        :options="[
                          { value: 'brave', label: 'Brave Search' },
                          { value: 'tavily', label: 'Tavily' },
                        ]"
                        class="w-36"
                        @click.stop
                      />
                      <!-- Quota summary (always visible) -->
                      <span class="text-xs text-gray-400">
                        {{ provider.quota_used ?? 0 }} /
                        {{
                          provider.quota_limit != null &&
                          provider.quota_limit > 0
                            ? provider.quota_limit
                            : "∞"
                        }}
                      </span>
                      <span
                        v-if="
                          !expandedProviders[pIdx] &&
                          provider.api_key_configured
                        "
                        class="text-xs text-green-500"
                      >
                        {{
                          t(
                            "admin.settings.webSearchEmulation.apiKeyConfigured",
                          )
                        }}
                      </span>
                    </div>
                    <button
                      type="button"
                      class="text-red-500 hover:text-red-700 text-xs"
                      @click.stop="removeWebSearchProvider(pIdx)"
                    >
                      {{
                        t("admin.settings.webSearchEmulation.removeProvider")
                      }}
                    </button>
                  </div>

                  <!-- Expanded content -->
                  <div
                    v-if="expandedProviders[pIdx]"
                    class="space-y-3 border-t border-gray-100 px-4 pb-4 pt-3 dark:border-dark-700"
                  >
                    <!-- API Key with inline show/copy -->
                    <div>
                      <label class="text-xs text-gray-500">{{
                        t("admin.settings.webSearchEmulation.apiKey")
                      }}</label>
                      <div class="relative">
                        <input
                          v-model="provider.api_key"
                          :type="apiKeyVisible[pIdx] ? 'text' : 'password'"
                          class="input w-full text-sm"
                          :class="
                            provider.api_key || provider.api_key_configured
                              ? 'pr-16'
                              : ''
                          "
                          :placeholder="
                            provider.api_key_configured
                              ? '••••••••'
                              : t(
                                  'admin.settings.webSearchEmulation.apiKeyPlaceholder',
                                )
                          "
                        />
                        <div
                          v-if="provider.api_key || provider.api_key_configured"
                          class="absolute inset-y-0 right-0 flex items-center pr-1.5"
                        >
                          <button
                            type="button"
                            class="rounded p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                            :title="
                              apiKeyVisible[pIdx]
                                ? t(
                                    'admin.settings.webSearchEmulation.hideApiKey',
                                  )
                                : t(
                                    'admin.settings.webSearchEmulation.showApiKey',
                                  )
                            "
                            @click="apiKeyVisible[pIdx] = !apiKeyVisible[pIdx]"
                          >
                            <svg
                              v-if="!apiKeyVisible[pIdx]"
                              class="h-4 w-4"
                              fill="none"
                              viewBox="0 0 24 24"
                              stroke="currentColor"
                            >
                              <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
                              />
                              <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
                              />
                            </svg>
                            <svg
                              v-else
                              class="h-4 w-4"
                              fill="none"
                              viewBox="0 0 24 24"
                              stroke="currentColor"
                            >
                              <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.878 9.878L3 3m6.878 6.878L21 21"
                              />
                            </svg>
                          </button>
                          <button
                            type="button"
                            class="rounded p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                            :class="{
                              'opacity-30 cursor-not-allowed':
                                !provider.api_key,
                            }"
                            :title="
                              t('admin.settings.webSearchEmulation.copyApiKey')
                            "
                            :disabled="!provider.api_key"
                            @click="copyApiKey(pIdx)"
                          >
                            <svg
                              class="h-4 w-4"
                              fill="none"
                              viewBox="0 0 24 24"
                              stroke="currentColor"
                            >
                              <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                              />
                            </svg>
                          </button>
                        </div>
                      </div>
                    </div>

                    <!-- Quota + Subscription in compact row -->
                    <div class="grid grid-cols-2 gap-3">
                      <div>
                        <label class="text-xs text-gray-500">{{
                          t("admin.settings.webSearchEmulation.quotaLimit")
                        }}</label>
                        <input
                          v-model="provider.quota_limit"
                          type="number"
                          min="1"
                          class="input text-sm"
                          :placeholder="'∞'"
                        />
                        <p class="mt-0.5 text-xs text-gray-400">
                          {{
                            t(
                              "admin.settings.webSearchEmulation.quotaLimitHint",
                            )
                          }}
                        </p>
                      </div>
                      <div>
                        <label class="text-xs text-gray-500">{{
                          t("admin.settings.webSearchEmulation.subscribedAt")
                        }}</label>
                        <input
                          :value="formatSubscribedAt(provider.subscribed_at)"
                          type="date"
                          class="input text-sm"
                          @input="
                            provider.subscribed_at = parseSubscribedAt(
                              ($event.target as HTMLInputElement).value,
                            )
                          "
                        />
                        <p class="mt-0.5 text-xs text-gray-400">
                          {{
                            t(
                              "admin.settings.webSearchEmulation.subscribedAtHint",
                            )
                          }}
                        </p>
                      </div>
                    </div>

                    <!-- Usage display -->
                    <div class="flex items-center gap-2">
                      <span class="text-xs text-gray-500"
                        >{{
                          t("admin.settings.webSearchEmulation.quotaUsage")
                        }}:</span
                      >
                      <div
                        v-if="
                          provider.quota_limit != null &&
                          provider.quota_limit > 0
                        "
                        class="flex-1 rounded-full bg-gray-200 dark:bg-dark-600"
                        style="height: 6px"
                      >
                        <div
                          class="h-full rounded-full transition-all"
                          :class="
                            quotaPercentage(provider) > 90
                              ? 'bg-red-500'
                              : quotaPercentage(provider) > 70
                                ? 'bg-yellow-500'
                                : 'bg-green-500'
                          "
                          :style="{
                            width:
                              Math.min(quotaPercentage(provider), 100) + '%',
                          }"
                        />
                      </div>
                      <div v-else class="flex-1" />
                      <span class="text-xs text-gray-500"
                        >{{ provider.quota_used ?? 0 }} /
                        {{
                          provider.quota_limit != null &&
                          provider.quota_limit > 0
                            ? provider.quota_limit
                            : "∞"
                        }}</span
                      >
                      <button
                        v-if="(provider.quota_used ?? 0) > 0"
                        type="button"
                        class="text-xs text-primary-600 hover:text-primary-700"
                        @click="resetWebSearchUsage(pIdx)"
                      >
                        {{ t("admin.settings.webSearchEmulation.resetUsage") }}
                      </button>
                    </div>

                    <!-- Proxy + Test on same row -->
                    <div class="flex items-end gap-3">
                      <div class="flex-1">
                        <label class="text-xs text-gray-500">{{
                          t("admin.settings.webSearchEmulation.proxy")
                        }}</label>
                        <ProxySelector
                          v-model="provider.proxy_id"
                          :proxies="webSearchProxies"
                        />
                      </div>
                      <button
                        type="button"
                        class="btn btn-secondary btn-sm whitespace-nowrap"
                        @click="openTestDialog()"
                      >
                        {{ t("admin.settings.webSearchEmulation.test") }}
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Web Search Test Dialog -->
          <div
            v-if="wsTestDialogOpen"
            class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
            @click.self="wsTestDialogOpen = false"
          >
            <div
              class="mx-4 w-full max-w-lg rounded-xl bg-white p-6 shadow-xl dark:bg-dark-800"
            >
              <h3
                class="mb-4 text-lg font-semibold text-gray-900 dark:text-white"
              >
                {{ t("admin.settings.webSearchEmulation.testResultTitle") }}
              </h3>
              <div class="flex items-center gap-2">
                <input
                  v-model="wsTestQuery"
                  type="text"
                  class="input flex-1 text-sm"
                  :placeholder="
                    t('admin.settings.webSearchEmulation.testDefaultQuery')
                  "
                  @keyup.enter="testWebSearchProvider()"
                />
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="wsTestLoading"
                  @click="testWebSearchProvider()"
                >
                  {{
                    wsTestLoading
                      ? t("admin.settings.webSearchEmulation.testing")
                      : t("admin.settings.webSearchEmulation.test")
                  }}
                </button>
              </div>
              <!-- Test results -->
              <div
                v-if="wsTestResult"
                class="mt-4 max-h-80 overflow-y-auto rounded-lg bg-gray-50 p-4 dark:bg-dark-700"
              >
                <p
                  class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{
                    t("admin.settings.webSearchEmulation.testResultProvider")
                  }}: {{ wsTestResult.provider }}
                </p>
                <div
                  v-if="wsTestResult.results.length === 0"
                  class="text-sm text-gray-400"
                >
                  {{ t("admin.settings.webSearchEmulation.testNoResults") }}
                </div>
                <div
                  v-for="(r, rIdx) in wsTestResult.results"
                  :key="rIdx"
                  class="mt-2 border-t border-gray-200 pt-2 first:mt-0 first:border-0 first:pt-0 dark:border-dark-600"
                >
                  <a
                    :href="r.url"
                    target="_blank"
                    class="text-sm font-medium text-blue-600 hover:underline dark:text-blue-400"
                    >{{ r.title }}</a
                  >
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ r.snippet }}
                  </p>
                </div>
              </div>
              <div class="mt-4 flex justify-end">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="wsTestDialogOpen = false"
                >
                  {{ t("common.close") }}
                </button>
              </div>
            </div>
          </div>
        </div>
        <!-- /Tab: Claude -->

        <!-- Tab: General -->
        <div v-show="activeTab === 'general'" class="space-y-6">
          <!-- Site Settings -->
          <div class="card">
            <div
              class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t("admin.settings.site.title") }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.site.description") }}
              </p>
            </div>
            <div class="space-y-6 p-6">
              <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.site.siteName") }}
                  </label>
                  <input
                    v-model="form.site_name"
                    type="text"
                    class="input"
                    :placeholder="t('admin.settings.site.siteNamePlaceholder')"
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.siteNameHint") }}
                  </p>
                </div>
                <div>
                  <label
                    class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    {{ t("admin.settings.site.siteSubtitle") }}
                  </label>
                  <input
                    v-model="form.site_subtitle"
                    type="text"
                    class="input"
                    :placeholder="
                      t('admin.settings.site.siteSubtitlePlaceholder')
                    "
                  />
                  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.siteSubtitleHint") }}
                  </p>
                </div>
              </div>

              <!-- API Base URL -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.apiBaseUrl") }}
                </label>
                <input
                  v-model="form.api_base_url"
                  type="text"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.apiBaseUrlPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.apiBaseUrlHint") }}
                </p>
              </div>

              <!-- Global Table Preferences -->
              <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
                <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ t("admin.settings.site.tablePreferencesTitle") }}
                </h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.tablePreferencesDescription") }}
                </p>
                <div class="mt-4 grid grid-cols-1 gap-6 md:grid-cols-2">
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.site.tableDefaultPageSize") }}
                    </label>
                    <input
                      v-model.number="form.table_default_page_size"
                      type="number"
                      min="5"
                      max="1000"
                      step="1"
                      class="input w-40"
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.site.tableDefaultPageSizeHint") }}
                    </p>
                  </div>
                  <div>
                    <label
                      class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                    >
                      {{ t("admin.settings.site.tablePageSizeOptions") }}
                    </label>
                    <input
                      v-model="tablePageSizeOptionsInput"
                      type="text"
                      class="input font-mono text-sm"
                      :placeholder="
                        t('admin.settings.site.tablePageSizeOptionsPlaceholder')
                      "
                    />
                    <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                      {{ t("admin.settings.site.tablePageSizeOptionsHint") }}
                    </p>
                  </div>
                </div>
              </div>

              <!-- Custom Endpoints -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.customEndpoints.title") }}
                </label>
                <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.customEndpoints.description") }}
                </p>

                <div class="space-y-3">
                  <div
                    v-for="(ep, index) in form.custom_endpoints"
                    :key="index"
                    class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
                  >
                    <div class="mb-3 flex items-center justify-between">
                      <span
                        class="text-sm font-medium text-gray-700 dark:text-gray-300"
                      >
                        {{
                          t("admin.settings.site.customEndpoints.itemLabel", {
                            n: index + 1,
                          })
                        }}
                      </span>
                      <button
                        type="button"
                        class="rounded p-1 text-red-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                        @click="removeEndpoint(index)"
                      >
                        <svg
                          class="h-4 w-4"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                          />
                        </svg>
                      </button>
                    </div>
                    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div>
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{ t("admin.settings.site.customEndpoints.name") }}
                        </label>
                        <input
                          v-model="ep.name"
                          type="text"
                          class="input text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.namePlaceholder',
                            )
                          "
                        />
                      </div>
                      <div>
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{
                            t("admin.settings.site.customEndpoints.endpointUrl")
                          }}
                        </label>
                        <input
                          v-model="ep.endpoint"
                          type="url"
                          class="input font-mono text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.endpointUrlPlaceholder',
                            )
                          "
                        />
                      </div>
                      <div class="sm:col-span-2">
                        <label
                          class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400"
                        >
                          {{
                            t(
                              "admin.settings.site.customEndpoints.descriptionLabel",
                            )
                          }}
                        </label>
                        <input
                          v-model="ep.description"
                          type="text"
                          class="input text-sm"
                          :placeholder="
                            t(
                              'admin.settings.site.customEndpoints.descriptionPlaceholder',
                            )
                          "
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <button
                  type="button"
                  class="mt-3 flex w-full items-center justify-center gap-2 rounded-lg border-2 border-dashed border-gray-300 px-4 py-2.5 text-sm text-gray-500 transition-colors hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-gray-400 dark:hover:border-primary-500 dark:hover:text-primary-400"
                  @click="addEndpoint"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 4v16m8-8H4"
                    />
                  </svg>
                  {{ t("admin.settings.site.customEndpoints.add") }}
                </button>
              </div>

              <!-- Contact Info -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.contactInfo") }}
                </label>
                <input
                  v-model="form.contact_info"
                  type="text"
                  class="input"
                  :placeholder="t('admin.settings.site.contactInfoPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.contactInfoHint") }}
                </p>
              </div>

              <!-- Doc URL -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.docUrl") }}
                </label>
                <input
                  v-model="form.doc_url"
                  type="url"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.docUrlPlaceholder')"
                />
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.docUrlHint") }}
                </p>
              </div>

              <!-- Site Logo Upload -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.siteLogo") }}
                </label>
                <ImageUpload
                  v-model="form.site_logo"
                  mode="image"
                  :upload-label="t('admin.settings.site.uploadImage')"
                  :remove-label="t('admin.settings.site.remove')"
                  :hint="t('admin.settings.site.logoHint')"
                  :max-size="300 * 1024"
                />
              </div>

              <!-- Home Content -->
              <div>
                <label
                  class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  {{ t("admin.settings.site.homeContent") }}
                </label>
                <textarea
                  v-model="form.home_content"
                  rows="6"
                  class="input font-mono text-sm"
                  :placeholder="t('admin.settings.site.homeContentPlaceholder')"
                ></textarea>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.site.homeContentHint") }}
                </p>
                <!-- iframe CSP Warning -->
                <p class="mt-2 text-xs text-amber-600 dark:text-amber-400">
                  {{ t("admin.settings.site.homeContentIframeWarning") }}
                </p>
              </div>

              <!-- Compact Home Page -->
              <div class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700">
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">{{
                    t("admin.settings.site.compactHome")
                  }}</label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.compactHomeHint") }}
                  </p>
                </div>
                <Toggle v-model="form.compact_home_enabled" data-testid="compact-home-toggle" />
              </div>

              <!-- Hide CCS Import Button -->
              <div
                class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700"
              >
                <div>
                  <label class="font-medium text-gray-900 dark:text-white">{{
                    t("admin.settings.site.hideCcsImportButton")
                  }}</label>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t("admin.settings.site.hideCcsImportButtonHint") }}
                  </p>
                </div>
                <Toggle v-model="form.hide_ccs_import_button" />
              </div>
            </div>
          </div>

          <!-- Custom user menu items were removed with the user portal. -->

	        </div>
	        <!-- /Tab: General -->

	        <!-- Tab: Login Agreement -->

        <!-- /Tab: Login Agreement -->

	        <!-- Tab: Features (功能开关) -->
        <div v-show="activeTab === 'features'" class="space-y-6">

        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.settings.features.riskControl.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.features.riskControl.description') }}
            </p>
            <p class="mt-1.5 text-xs">
              <router-link
                to="/admin/risk-control"
                class="inline-flex items-center gap-1 text-primary-600 hover:underline dark:text-primary-400"
              >
                {{ t('admin.settings.features.riskControl.configureLink') }}
                <span aria-hidden="true">→</span>
              </router-link>
            </p>
          </div>
          <div class="space-y-5 p-6">
            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.riskControl.enabled') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.riskControl.enabledHint') }}
                </p>
              </div>
              <Toggle v-model="form.risk_control_enabled" />
            </div>



            <div class="flex items-center justify-between">
              <div>
                <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('admin.settings.features.riskControl.cyberSessionBlock') }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.settings.features.riskControl.cyberSessionBlockHint') }}
                </p>
              </div>
              <Toggle v-model="form.cyber_session_block_enabled" />
            </div>

            <div v-if="form.cyber_session_block_enabled">
              <label class="input-label">
                {{ t('admin.settings.features.riskControl.cyberSessionBlockTTL') }}
                <span class="text-red-500">*</span>
              </label>
              <input
                v-model.number="form.cyber_session_block_ttl_seconds"
                type="number"
                min="1"
                class="input"
              />
            </div>
          </div>
        </div>




        </div><!-- /Tab: Features -->

        <!-- Save Button -->
        <div class="flex justify-end">
          <button
            type="submit"
            :disabled="saving || loadFailed"
            class="btn btn-primary"
          >
            <svg
              v-if="saving"
              class="h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{
              saving
                ? t("admin.settings.saving")
                : t("admin.settings.saveSettings")
            }}
          </button>
        </div>
      </form>

      <!-- 关闭 step-up 开关等敏感保存操作触发的 TOTP 二次验证 -->
      <TotpStepUpDialog :controller="settingsStepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api";
import { normalizeAccountSchedulingThresholdsMap, sanitizeAccountSchedulingThresholdsMap, SCHEDULING_THRESHOLD_PLATFORMS } from "@/api/admin/settings";
import type { SystemSettings, UpdateSettingsRequest, OpenAIFastPolicyRule, WebSearchEmulationConfig, WebSearchProviderConfig, WebSearchTestResult, OpenAICodexVersionRelease } from "@/api/admin/settings";
import type { ImageStorageConfig } from "@/api/admin/imageStorage";
import type { LoginAgreementDocument, NotifyEmailEntry, Proxy } from "@/types";
import AppLayout from "@/components/layout/AppLayout.vue";
import Icon from "@/components/icons/Icon.vue";
import Select from "@/components/common/Select.vue";
import { buildCodexUserAgentTemplate, codexOriginators, compareCodexVersions, defaultCodexClientVersion, isStableCodexVersion, setCodexTemplateVersion } from "./codexHeaderPresets";
import Toggle from "@/components/common/Toggle.vue";
import ProxySelector from "@/components/common/ProxySelector.vue";
import ImageUpload from "@/components/common/ImageUpload.vue";
import { useStepUp, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from "@/composables/useStepUp";
import TotpStepUpDialog from "@/components/auth/TotpStepUpDialog.vue";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useAppStore } from "@/stores";
import { useAdminSettingsStore } from "@/stores/adminSettings";
import { parseFingerprintSignalsToRows, serializeFingerprintRowsToJSON, defaultFingerprintSignalRows, type FingerprintSignalRow } from "./codexFingerprintSignals";

const { t, locale } = useI18n();
const appStore = useAppStore();
// 关闭 step-up 开关是敏感操作：后端返回 STEP_UP_REQUIRED 时弹 TOTP 码重试
const settingsStepUp = useStepUp();
const adminSettingsStore = useAdminSettingsStore();
const isZhLocale = computed(() => locale.value.startsWith("zh"));

function localText(zh: string, en: string): string {
  return isZhLocale.value ? zh : en;
}

type SettingsTab =
  | "general"
  | "features"
  | "security"
  | "gateway"
  | "gpt"
  | "claude";
const activeTab = ref<SettingsTab>("general");
const settingsTabs = [
  { key: "general" as SettingsTab, icon: "home" as const },
  { key: "features" as SettingsTab, icon: "bolt" as const },
  { key: "security" as SettingsTab, icon: "shield" as const },
  { key: "gateway" as SettingsTab, icon: "server" as const },
  { key: "gpt" as SettingsTab, icon: "terminal" as const },
  { key: "claude" as SettingsTab, icon: "sparkles" as const },
];

const settingsTabKeyboardActions = {
  ArrowLeft: -1,
  ArrowUp: -1,
  ArrowRight: 1,
  ArrowDown: 1,
  Home: "first",
  End: "last",
} as const;

function selectSettingsTab(tab: SettingsTab): void {
  activeTab.value = tab;
}

function focusSettingsTab(tab: SettingsTab): void {
  window.requestAnimationFrame(() => {
    document.getElementById(`settings-tab-${tab}`)?.focus();
  });
}

function handleSettingsTabKeydown(event: KeyboardEvent, tab: SettingsTab): void {
  const action =
    settingsTabKeyboardActions[
      event.key as keyof typeof settingsTabKeyboardActions
    ];
  if (action === undefined) {
    return;
  }

  event.preventDefault();
  const currentIndex = settingsTabs.findIndex((item) => item.key === tab);
  let nextIndex = currentIndex < 0 ? 0 : currentIndex;

  if (action === "first") {
    nextIndex = 0;
  } else if (action === "last") {
    nextIndex = settingsTabs.length - 1;
  } else {
    nextIndex =
      (nextIndex + action + settingsTabs.length) % settingsTabs.length;
  }

  const nextTab = settingsTabs[nextIndex]?.key;
  if (!nextTab) {
    return;
  }

  selectSettingsTab(nextTab);
  focusSettingsTab(nextTab);
}

const loading = ref(true);
const loadFailed = ref(false);
const saving = ref(false);
const forwardedClientIpHeaderDraft = ref("");
const tablePageSizeOptionsInput = ref("10, 20, 50, 100");

// Admin API Key 状态
const adminApiKeyLoading = ref(true);
const adminApiKeyExists = ref(false);
const adminApiKeyMasked = ref("");
const adminApiKeyOperating = ref(false);
const newAdminApiKey = ref("");

const imageStorageLoading = ref(true);
const imageStorageSaving = ref(false);
const imageStorageTesting = ref(false);
const imageStorageLoadError = ref("");
const imageStorageSecretConfigured = ref(false);
const imageStorageForm = reactive<ImageStorageConfig>({
  enabled: false,
  endpoint: "",
  region: "auto",
  bucket: "",
  access_key_id: "",
  secret_access_key: "",
  prefix: "images/",
  public_base_url: "",
  force_path_style: false,
  presign_expiry_hours: 24,
  max_download_bytes: 33554432,
});

// Upstream billing probe state
const upstreamBillingProbeLoading = ref(true);
const upstreamBillingProbeSaving = ref(false);
const upstreamBillingProbeForm = reactive({
  enabled: true,
  interval_minutes: 30,
});

// Overload Cooldown (529) 状态
const overloadCooldownLoading = ref(true);
const overloadCooldownSaving = ref(false);
const overloadCooldownForm = reactive({
  enabled: true,
  cooldown_minutes: 10,
});

// Rate Limit Cooldown (429) 状态
const rateLimit429CooldownLoading = ref(true);
const rateLimit429CooldownSaving = ref(false);
const rateLimit429CooldownForm = reactive({
  enabled: true,
  cooldown_seconds: 5,
});

// Panel API Rate Limit 状态
const panelRateLimitLoading = ref(true);
const panelRateLimitSaving = ref(false);
const panelRateLimitForm = reactive({
  enabled: true,
  user_rpm: 240,
  heavy_rpm: 60,
  exempt_admin: true,
  public_ip_rpm: 300,
});

// Stream Timeout 状态
const streamTimeoutLoading = ref(true);
const streamTimeoutSaving = ref(false);
const streamTimeoutForm = reactive({
  enabled: true,
  action: "temp_unsched" as "temp_unsched" | "error" | "none",
  temp_unsched_minutes: 5,
  threshold_count: 3,
  threshold_window_minutes: 10,
});

// Rectifier 状态
const rectifierLoading = ref(true);
const rectifierSaving = ref(false);
const rectifierForm = reactive({
  enabled: true,
  thinking_signature_enabled: true,
  thinking_budget_enabled: true,
  apikey_signature_enabled: false,
  apikey_signature_patterns: [] as string[],
});

// Beta Policy 状态
const betaPolicyLoading = ref(true);
const betaPolicySaving = ref(false);
const betaPolicyForm = reactive({
  rules: [] as Array<{
    beta_token: string;
    action: "pass" | "filter" | "block";
    scope: "all" | "oauth" | "apikey" | "bedrock";
    error_message?: string;
    model_whitelist?: string[];
    fallback_action?: "pass" | "filter" | "block";
    fallback_error_message?: string;
  }>,
});

// OpenAI Fast/Flex Policy 状态
const openaiFastPolicyForm = reactive({
  rules: [] as OpenAIFastPolicyRule[],
});
// 标记 openai_fast_policy_settings 是否已成功从后端加载，
// 避免后端 GET 出错或字段缺失时，保存把默认规则覆盖成空数组。
const openaiFastPolicyLoaded = ref(false);

const tablePageSizeMin = 5;
const tablePageSizeMax = 1000;
const tablePageSizeDefault = 20;

function defaultLoginAgreementDocuments(): LoginAgreementDocument[] {
  return [
    {
      id: "terms",
      title: localText("服务条款", "Terms of Service"),
      content_md: "",
    },
    {
      id: "usage-policy",
      title: localText("使用政策", "Usage Policy"),
      content_md: "",
    },
    {
      id: "supported-regions",
      title: localText("支持的国家和地区", "Supported Countries and Regions"),
      content_md: "",
    },
    {
      id: "service-specific-terms",
      title: localText("服务特定条款", "Service-Specific Terms"),
      content_md: "",
    },
  ];
}

type ClaudeOAuthSystemPromptPreset =
  | "billing"
  | "system"
  | "expansion"
  | "custom";

interface ClaudeOAuthSystemPromptBlock {
  id: string;
  enabled: boolean;
  expanded: boolean;
  type: "text";
  preset: ClaudeOAuthSystemPromptPreset;
  text: string;
  cacheControlEnabled: boolean;
  cacheControlTTL: string;
}

interface ClaudeOAuthSystemPromptRawBlock {
  enabled?: boolean;
  type?: string;
  text?: string;
  cache_control?: unknown;
}

const defaultClaudeCodeSystemPrompt =
  "You are Claude Code, Anthropic's official CLI for Claude.";

const defaultClaudeCodeExpansionPrompt = `You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts. Refuse requests for destructive techniques, DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases.
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.

# Tone and style
 - Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.
 - Your responses should be short and concise.
 - When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.
 - When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.
 - Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`;

let claudeOAuthSystemPromptBlockID = 0;

function nextClaudeOAuthSystemPromptBlockID(): string {
  claudeOAuthSystemPromptBlockID += 1;
  return `claude-oauth-system-prompt-block-${claudeOAuthSystemPromptBlockID}`;
}

function normalizeClaudeOAuthSystemPromptCacheTTL(value: unknown): string {
  return typeof value === "string" && value.trim() ? value.trim() : "5m";
}

function detectClaudeOAuthSystemPromptPreset(
  text: string,
): ClaudeOAuthSystemPromptPreset {
  const trimmed = text.trim();
  if (trimmed === "{billing_header}") {
    return "billing";
  }
  if (
    trimmed === "{claude_code_system_prompt}" ||
    trimmed === defaultClaudeCodeSystemPrompt
  ) {
    return "system";
  }
  if (
    trimmed === "{claude_code_expansion_prompt}" ||
    trimmed === defaultClaudeCodeExpansionPrompt
  ) {
    return "expansion";
  }
  return "custom";
}

function normalizeClaudeOAuthSystemPromptBlockText(
  text: string,
  expansionPrompt = "",
): string {
  const trimmed = text.trim();
  if (trimmed === "{claude_code_system_prompt}") {
    return defaultClaudeCodeSystemPrompt;
  }
  if (trimmed === "{claude_code_expansion_prompt}") {
    return expansionPrompt.trim() || defaultClaudeCodeExpansionPrompt;
  }
  return text;
}

function createClaudeOAuthSystemPromptBlock(
  overrides: Partial<ClaudeOAuthSystemPromptBlock> = {},
): ClaudeOAuthSystemPromptBlock {
  const text = overrides.text ?? "";
  return {
    id: nextClaudeOAuthSystemPromptBlockID(),
    enabled: overrides.enabled ?? true,
    expanded: overrides.expanded ?? true,
    type: "text",
    preset: overrides.preset ?? detectClaudeOAuthSystemPromptPreset(text),
    text,
    cacheControlEnabled: overrides.cacheControlEnabled ?? false,
    cacheControlTTL: overrides.cacheControlTTL ?? "5m",
  };
}

function createDefaultClaudeOAuthSystemPromptBlocks(
  expansionPrompt = "",
): ClaudeOAuthSystemPromptBlock[] {
  const normalizedExpansionPrompt = expansionPrompt.trim();
  const expansionText =
    normalizedExpansionPrompt || defaultClaudeCodeExpansionPrompt;

  return [
    createClaudeOAuthSystemPromptBlock({
      preset: "billing",
      text: "{billing_header}",
    }),
    createClaudeOAuthSystemPromptBlock({
      preset: "system",
      text: defaultClaudeCodeSystemPrompt,
    }),
    createClaudeOAuthSystemPromptBlock({
      preset:
        expansionText === defaultClaudeCodeExpansionPrompt
          ? "expansion"
          : "custom",
      text: expansionText,
      cacheControlEnabled: true,
      cacheControlTTL: "5m",
    }),
  ];
}

function parseClaudeOAuthSystemPromptCacheControl(cacheControl: unknown): {
  enabled: boolean;
  ttl: string;
} {
  if (cacheControl === true) {
    return { enabled: true, ttl: "5m" };
  }
  if (
    cacheControl &&
    typeof cacheControl === "object" &&
    !Array.isArray(cacheControl)
  ) {
    return {
      enabled: true,
      ttl: normalizeClaudeOAuthSystemPromptCacheTTL(
        (cacheControl as Record<string, unknown>).ttl,
      ),
    };
  }
  return { enabled: false, ttl: "5m" };
}

function parseClaudeOAuthSystemPromptBlocks(
  raw: string,
  expansionPrompt = "",
): ClaudeOAuthSystemPromptBlock[] {
  const trimmed = raw.trim();
  if (!trimmed) {
    return createDefaultClaudeOAuthSystemPromptBlocks(expansionPrompt);
  }

  try {
    const parsed = JSON.parse(trimmed) as
      | ClaudeOAuthSystemPromptRawBlock[]
      | { blocks?: ClaudeOAuthSystemPromptRawBlock[] };
    const rawBlocks = Array.isArray(parsed)
      ? parsed
      : Array.isArray(parsed.blocks)
        ? parsed.blocks
        : [];

    if (rawBlocks.length === 0) {
      return createDefaultClaudeOAuthSystemPromptBlocks(expansionPrompt);
    }

    return rawBlocks.map((block) => {
      const cacheControl = parseClaudeOAuthSystemPromptCacheControl(
        block.cache_control,
      );
      const text = normalizeClaudeOAuthSystemPromptBlockText(
        typeof block.text === "string" ? block.text : "",
        expansionPrompt,
      );
      return createClaudeOAuthSystemPromptBlock({
        enabled: block.enabled !== false,
        type: "text",
        text,
        preset: detectClaudeOAuthSystemPromptPreset(text),
        cacheControlEnabled: cacheControl.enabled,
        cacheControlTTL: cacheControl.ttl,
      });
    });
  } catch (_error) {
    return createDefaultClaudeOAuthSystemPromptBlocks(expansionPrompt);
  }
}

function serializeClaudeOAuthSystemPromptBlocksToJSON(
  blocks: ClaudeOAuthSystemPromptBlock[],
): string {
  const source =
    blocks.length > 0
      ? blocks
      : [
          createClaudeOAuthSystemPromptBlock({
            enabled: false,
            preset: "custom",
            text: "",
          }),
        ];

  const rawBlocks = source.map((block) => {
    const raw: ClaudeOAuthSystemPromptRawBlock = {
      enabled: block.enabled,
      type: block.type || "text",
      text: block.text,
    };
    if (block.cacheControlEnabled) {
      raw.cache_control = {
        type: "ephemeral",
        ttl: normalizeClaudeOAuthSystemPromptCacheTTL(block.cacheControlTTL),
      };
    }
    return raw;
  });

  return JSON.stringify(rawBlocks, null, 2);
}

const defaultClaudeOAuthSystemPromptBlocks =
  serializeClaudeOAuthSystemPromptBlocksToJSON(
    createDefaultClaudeOAuthSystemPromptBlocks(),
  );

const claudeOAuthSystemPromptBlocks = ref<ClaudeOAuthSystemPromptBlock[]>(
  createDefaultClaudeOAuthSystemPromptBlocks(),
);

const claudeOAuthSystemPromptPresetOptions = computed(() => [
  {
    value: "billing",
    label: t("admin.settings.gatewayForwarding.systemBlockPresetBilling"),
  },
  {
    value: "system",
    label: t("admin.settings.gatewayForwarding.systemBlockPresetIdentity"),
  },
  {
    value: "expansion",
    label: t("admin.settings.gatewayForwarding.systemBlockPresetExpansion"),
  },
  {
    value: "custom",
    label: t("admin.settings.gatewayForwarding.systemBlockPresetCustom"),
  },
]);

const claudeOAuthSystemPromptBlockTypeOptions = computed(() => [
  {
    value: "text",
    label: t("admin.settings.gatewayForwarding.systemBlockTypeText"),
  },
]);

const claudeOAuthSystemPromptCacheTTLOptions = computed(() => [
  { value: "5m", label: t("admin.settings.gatewayForwarding.cacheTTL5m") },
  { value: "1h", label: t("admin.settings.gatewayForwarding.cacheTTL1h") },
]);

function getClaudeOAuthPresetLabel(
  preset: ClaudeOAuthSystemPromptPreset,
): string {
  return (
    claudeOAuthSystemPromptPresetOptions.value.find(
      (option) => option.value === preset,
    )?.label || t("admin.settings.gatewayForwarding.systemBlockPresetCustom")
  );
}

function syncClaudeOAuthSystemPromptBlocksFormField(): void {
  form.claude_oauth_system_prompt_blocks =
    serializeClaudeOAuthSystemPromptBlocksToJSON(
      claudeOAuthSystemPromptBlocks.value,
    );
}

function addClaudeOAuthSystemPromptBlock(): void {
  claudeOAuthSystemPromptBlocks.value.push(
    createClaudeOAuthSystemPromptBlock({
      expanded: true,
      preset: "custom",
      text: "",
    }),
  );
  syncClaudeOAuthSystemPromptBlocksFormField();
}

function toggleClaudeOAuthSystemPromptBlock(index: number): void {
  const block = claudeOAuthSystemPromptBlocks.value[index];
  if (!block) {
    return;
  }
  block.expanded = !block.expanded;
}

function removeClaudeOAuthSystemPromptBlock(index: number): void {
  claudeOAuthSystemPromptBlocks.value.splice(index, 1);
  syncClaudeOAuthSystemPromptBlocksFormField();
}

function moveClaudeOAuthSystemPromptBlock(
  index: number,
  direction: -1 | 1,
): void {
  const targetIndex = index + direction;
  if (
    targetIndex < 0 ||
    targetIndex >= claudeOAuthSystemPromptBlocks.value.length
  ) {
    return;
  }
  const blocks = claudeOAuthSystemPromptBlocks.value;
  const current = blocks[index];
  blocks[index] = blocks[targetIndex];
  blocks[targetIndex] = current;
  syncClaudeOAuthSystemPromptBlocksFormField();
}

function applyClaudeOAuthSystemPromptPreset(
  index: number,
  value: string | number | boolean | null,
): void {
  const block = claudeOAuthSystemPromptBlocks.value[index];
  if (!block) {
    return;
  }
  const preset = String(value || "custom") as ClaudeOAuthSystemPromptPreset;
  block.preset = preset;
  block.type = "text";
  if (preset === "billing") {
    block.text = "{billing_header}";
    block.cacheControlEnabled = false;
    block.cacheControlTTL = "5m";
  } else if (preset === "system") {
    block.text = defaultClaudeCodeSystemPrompt;
    block.cacheControlEnabled = false;
    block.cacheControlTTL = "5m";
  } else if (preset === "expansion") {
    block.text =
      form.claude_oauth_system_prompt.trim() ||
      defaultClaudeCodeExpansionPrompt;
    block.cacheControlEnabled = true;
    block.cacheControlTTL = "5m";
  }
  syncClaudeOAuthSystemPromptBlocksFormField();
}

function markClaudeOAuthSystemPromptBlockCustom(
  block: ClaudeOAuthSystemPromptBlock,
): void {
  block.preset = detectClaudeOAuthSystemPromptPreset(block.text);
  syncClaudeOAuthSystemPromptBlocksFormField();
}

function resetClaudeOAuthSystemPromptBlocks(): void {
  claudeOAuthSystemPromptBlocks.value = createDefaultClaudeOAuthSystemPromptBlocks(
    form.claude_oauth_system_prompt,
  );
  syncClaudeOAuthSystemPromptBlocksFormField();
}

type SettingsForm = Omit<
  SystemSettings,
  | "openai_oauth_scheduling_rate_multiplier"
> & {
  /** Form always binds a concrete boolean (SystemSettings marks this optional). */
  channel_monitor_hide_throughput: boolean;
  channel_monitor_show_quota: boolean;
  turnstile_secret_key: string;
  tencent_captcha_app_secret_key: string;
  tencent_captcha_cloud_secret_id: string;
  tencent_captcha_cloud_secret_key: string;
  aliyun_captcha_access_key_secret: string;
  openai_low_upstream_rate_priority_enabled: boolean;
  openai_oauth_scheduling_rate_multiplier: number | "" | null;
  openai_advanced_scheduler_enabled: boolean;
  openai_advanced_scheduler_sticky_weighted_enabled: boolean;
  openai_advanced_scheduler_subscription_priority_enabled: boolean;
  openai_advanced_scheduler_lb_top_k: string;
  openai_advanced_scheduler_weight_priority: string;
  openai_advanced_scheduler_weight_load: string;
  openai_advanced_scheduler_weight_queue: string;
  openai_advanced_scheduler_weight_error_rate: string;
  openai_advanced_scheduler_weight_ttft: string;
  openai_advanced_scheduler_weight_reset: string;
  openai_advanced_scheduler_weight_quota_headroom: string;
  openai_advanced_scheduler_weight_upstream_cost: string;
  openai_advanced_scheduler_weight_previous_response: string;
  openai_advanced_scheduler_weight_session_sticky: string;
  account_scheduling_thresholds: ReturnType<typeof normalizeAccountSchedulingThresholdsMap>;
};

const schedulingThresholdPlatforms = SCHEDULING_THRESHOLD_PLATFORMS;

const form = reactive<SettingsForm>({
  totp_enabled: false,
  totp_encryption_key_configured: false,
  session_binding_enabled: false,
  step_up_enabled: false,
  audit_log_retention_days: 180,
  login_agreement_enabled: false,
  login_agreement_mode: "modal",
  login_agreement_updated_at: "2026-03-31",
  login_agreement_documents: defaultLoginAgreementDocuments(),
  account_scheduling_thresholds: normalizeAccountSchedulingThresholdsMap(),
  site_name: "QQQ2API",
  site_logo: "",
  site_subtitle: "Claude Code & OpenAI API Gateway",
  api_base_url: "",
  contact_info: "",
  doc_url: "",
  home_content: "",
  compact_home_enabled: false,
  backend_mode_enabled: false,
  hide_ccs_import_button: false,
  risk_control_enabled: false,
  cyber_session_block_enabled: false,
  cyber_session_block_ttl_seconds: 3600,
  table_default_page_size: tablePageSizeDefault,
  table_page_size_options: [10, 20, 50, 100],
  custom_endpoints: [] as Array<{
    name: string;
    endpoint: string;
    description: string;
  }>,
  // Cloudflare Turnstile
  turnstile_enabled: false,
  turnstile_site_key: "",
  turnstile_secret_key: "",
  turnstile_secret_key_configured: false,
  tencent_captcha_enabled: false,
  tencent_captcha_app_id: "",
  tencent_captcha_app_secret_key: "",
  tencent_captcha_app_secret_key_configured: false,
  tencent_captcha_cloud_secret_id: "",
  tencent_captcha_cloud_secret_id_configured: false,
  tencent_captcha_cloud_secret_key: "",
  tencent_captcha_cloud_secret_key_configured: false,
  tencent_captcha_region: "cn",
  aliyun_captcha_enabled: false,
  aliyun_captcha_access_key_id: "",
  aliyun_captcha_access_key_secret: "",
  aliyun_captcha_access_key_secret_configured: false,
  aliyun_captcha_scene_id: "",
  aliyun_captcha_prefix: "",
  aliyun_captcha_region: "cn",
  api_key_acl_trust_forwarded_ip: true,
  forwarded_client_ip_headers: [],
  // Model fallback
  enable_model_fallback: false,
  fallback_model_anthropic: "claude-3-5-sonnet-20241022",
  fallback_model_openai: "gpt-4o",
  grok_default_text_model: "grok-4.6",
  grok_cross_client_model_map_enabled: true,
  grok_default_base_url_mode: "cli",
  // Ops monitoring (vNext)
  ops_monitoring_enabled: true,
  ops_realtime_monitoring_enabled: true,
  ops_query_mode_default: "auto",
  ops_metrics_interval_seconds: 60,
  // Claude Code version check
  min_claude_code_version: "",
  max_claude_code_version: "",
  // 分组隔离
  allow_ungrouped_key_scheduling: false,
  openai_low_upstream_rate_priority_enabled: false,
  openai_oauth_scheduling_rate_multiplier: 1,
  openai_advanced_scheduler_enabled: false,
  openai_advanced_scheduler_sticky_weighted_enabled: false,
  openai_advanced_scheduler_subscription_priority_enabled: false,
  openai_advanced_scheduler_lb_top_k: "",
  openai_advanced_scheduler_weight_priority: "",
  openai_advanced_scheduler_weight_load: "",
  openai_advanced_scheduler_weight_queue: "",
  openai_advanced_scheduler_weight_error_rate: "",
  openai_advanced_scheduler_weight_ttft: "",
  openai_advanced_scheduler_weight_reset: "",
  openai_advanced_scheduler_weight_quota_headroom: "",
  openai_advanced_scheduler_weight_upstream_cost: "",
  openai_advanced_scheduler_weight_previous_response: "",
  openai_advanced_scheduler_weight_session_sticky: "",
  // Gateway forwarding behavior
  openai_ttft_mode: "semantic",
  enable_fingerprint_unification: true,
  enable_metadata_passthrough: false,
  enable_cch_signing: false,
  enable_claude_oauth_system_prompt_injection: true,
  claude_oauth_system_prompt: "",
  claude_oauth_system_prompt_blocks: defaultClaudeOAuthSystemPromptBlocks,
  enable_anthropic_cache_ttl_1h_injection: false,
  rewrite_message_cache_control: false,
  enable_client_dateline_normalization: true,
  openai_codex_originator: "",
  openai_codex_user_agent: "",
  openai_codex_client_version: "",
  openai_codex_client_version_mode: "auto",
  // 只读展示：与后端实际出站解析链同源的当前默认请求头值，
  // 保持可编辑字段为空时的“恢复默认”语义；不参与提交。
  openai_codex_originator_default: "",
  openai_codex_user_agent_default: "",
  openai_codex_client_version_default: "",
  enable_openai_account_local_device_identity: true,
  // 只读展示：自动同步任务写入的官方最新稳定版，供 UA engine 与
  // Responses/WS Version 共用；不参与提交（提交载荷按字段显式构造）
  openai_codex_client_version_synced: "",
  openai_codex_version_auto_sync_enabled: true,
  claude_code_client_version: "",
  // 只读展示：自动同步任务写入的官方最新稳定版，不参与提交（提交载荷按字段显式构造）
  claude_code_client_version_synced: "",
  claude_code_version_auto_sync_enabled: true,
  // codex_cli_only 加固
  min_codex_version: "",
  max_codex_version: "",
  codex_cli_only_blacklist: "",
  codex_cli_only_whitelist: "",
  codex_cli_only_allow_app_server_clients: false,
  codex_cli_only_engine_fingerprint_signals: "",
  // 账号限额通知
  account_quota_notify_enabled: false,
  account_quota_notify_emails: [] as NotifyEmailEntry[],
  // Channel Monitor feature switch
  channel_monitor_enabled: true,
  channel_monitor_mode: 'v1' as 'v1' | 'v2',
  channel_monitor_default_interval_seconds: 60,
  channel_monitor_hide_throughput: false,
  channel_monitor_show_quota: false,
  // Available Channels feature switch
  available_channels_enabled: false,
  // Model Plaza feature switches + description
  model_plaza_enabled: false,
  model_plaza_require_auth: false,
  model_plaza_description: '',
  // Allow user view error requests
  allow_user_view_error_requests: false,
});

// 人机验证 UI 状态：单卡片「总开关 + 服务商单选」，落库仍是三个独立
// enabled 键（与上游一致），由下面的映射保证同一时间至多一家启用。
type CaptchaProviderSelection = "turnstile" | "tencent" | "aliyun";

const captchaProviderSelection = ref<CaptchaProviderSelection>("turnstile");

function syncCaptchaProviderSelection(): void {
  if (form.tencent_captcha_enabled) {
    captchaProviderSelection.value = "tencent";
  } else if (form.aliyun_captcha_enabled) {
    captchaProviderSelection.value = "aliyun";
  } else if (form.turnstile_enabled) {
    captchaProviderSelection.value = "turnstile";
  }
}

type OpenAIAdvancedSchedulerOverrideKey =
  | "openai_advanced_scheduler_lb_top_k"
  | "openai_advanced_scheduler_weight_priority"
  | "openai_advanced_scheduler_weight_load"
  | "openai_advanced_scheduler_weight_queue"
  | "openai_advanced_scheduler_weight_error_rate"
  | "openai_advanced_scheduler_weight_ttft"
  | "openai_advanced_scheduler_weight_reset"
  | "openai_advanced_scheduler_weight_quota_headroom"
  | "openai_advanced_scheduler_weight_upstream_cost"
  | "openai_advanced_scheduler_weight_previous_response"
  | "openai_advanced_scheduler_weight_session_sticky";

type OpenAIAdvancedSchedulerEffectiveKey =
  | "openai_advanced_scheduler_effective_lb_top_k"
  | "openai_advanced_scheduler_effective_weight_priority"
  | "openai_advanced_scheduler_effective_weight_load"
  | "openai_advanced_scheduler_effective_weight_queue"
  | "openai_advanced_scheduler_effective_weight_error_rate"
  | "openai_advanced_scheduler_effective_weight_ttft"
  | "openai_advanced_scheduler_effective_weight_reset"
  | "openai_advanced_scheduler_effective_weight_quota_headroom"
  | "openai_advanced_scheduler_effective_weight_upstream_cost"
  | "openai_advanced_scheduler_effective_weight_previous_response"
  | "openai_advanced_scheduler_effective_weight_session_sticky";

const openAIAdvancedSchedulerWeightFields = computed<
  Array<{
    key: OpenAIAdvancedSchedulerOverrideKey;
    label: string;
    placeholder: string;
  }>
>(() => {
  const placeholder = (
    effectiveKey: OpenAIAdvancedSchedulerEffectiveKey,
    fallbackValue: string,
  ) => {
    const effectiveValue = String(
      (form as Record<string, unknown>)[effectiveKey] ?? "",
    ).trim();
    return t("admin.settings.openaiExperimentalScheduler.defaultPlaceholder", {
      value: effectiveValue || fallbackValue,
    });
  };

  return [
    {
      key: "openai_advanced_scheduler_lb_top_k",
      label: t("admin.settings.openaiExperimentalScheduler.topKLabel"),
      placeholder: placeholder("openai_advanced_scheduler_effective_lb_top_k", "7"),
    },
    {
      key: "openai_advanced_scheduler_weight_priority",
      label: t("admin.settings.openaiExperimentalScheduler.priorityWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_priority", "1"),
    },
    {
      key: "openai_advanced_scheduler_weight_load",
      label: t("admin.settings.openaiExperimentalScheduler.loadWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_load", "1"),
    },
    {
      key: "openai_advanced_scheduler_weight_queue",
      label: t("admin.settings.openaiExperimentalScheduler.queueWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_queue", "0.7"),
    },
    {
      key: "openai_advanced_scheduler_weight_error_rate",
      label: t("admin.settings.openaiExperimentalScheduler.errorRateWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_error_rate", "0.8"),
    },
    {
      key: "openai_advanced_scheduler_weight_ttft",
      label: t("admin.settings.openaiExperimentalScheduler.ttftWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_ttft", "0.5"),
    },
    {
      key: "openai_advanced_scheduler_weight_reset",
      label: t("admin.settings.openaiExperimentalScheduler.resetWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_reset", "0"),
    },
    {
      key: "openai_advanced_scheduler_weight_quota_headroom",
      label: t("admin.settings.openaiExperimentalScheduler.quotaHeadroomWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_quota_headroom", "0"),
    },
    {
      key: "openai_advanced_scheduler_weight_upstream_cost",
      label: t("admin.settings.openaiExperimentalScheduler.upstreamCostWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_upstream_cost", "0"),
    },
    {
      key: "openai_advanced_scheduler_weight_previous_response",
      label: t("admin.settings.openaiExperimentalScheduler.previousResponseWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_previous_response", "5"),
    },
    {
      key: "openai_advanced_scheduler_weight_session_sticky",
      label: t("admin.settings.openaiExperimentalScheduler.sessionStickyWeight"),
      placeholder: placeholder("openai_advanced_scheduler_effective_weight_session_sticky", "3"),
    },
  ];
});

// Proxies for web search emulation ProxySelector
const webSearchProxies = ref<Proxy[]>([]);
// Web Search Emulation config (loaded/saved separately)
const DEFAULT_WEB_SEARCH_QUOTA_LIMIT = 1000;

const webSearchConfig = reactive<WebSearchEmulationConfig>({
  enabled: false,
  providers: [],
});

const expandedProviders = reactive<Record<number, boolean>>({});
const apiKeyVisible = reactive<Record<number, boolean>>({});
const wsTestQuery = ref("");
const wsTestLoading = ref(false);
const wsTestResult = ref<WebSearchTestResult | null>(null);
const wsTestDialogOpen = ref(false);

function openTestDialog() {
  wsTestResult.value = null;
  wsTestDialogOpen.value = true;
}

function toggleProviderExpand(idx: number) {
  expandedProviders[idx] = !expandedProviders[idx];
}

function removeWebSearchProvider(idx: number) {
  webSearchConfig.providers.splice(idx, 1);
  // Re-index expandedProviders and apiKeyVisible after removal
  const newExpanded: Record<number, boolean> = {};
  const newVisible: Record<number, boolean> = {};
  for (let i = 0; i < webSearchConfig.providers.length; i++) {
    const oldIdx = i >= idx ? i + 1 : i;
    newExpanded[i] = expandedProviders[oldIdx] ?? false;
    newVisible[i] = apiKeyVisible[oldIdx] ?? false;
  }
  Object.keys(expandedProviders).forEach(
    (k) => delete expandedProviders[Number(k)],
  );
  Object.keys(apiKeyVisible).forEach((k) => delete apiKeyVisible[Number(k)]);
  Object.assign(expandedProviders, newExpanded);
  Object.assign(apiKeyVisible, newVisible);
}

function addWebSearchProvider() {
  const idx = webSearchConfig.providers.length;
  webSearchConfig.providers.push({
    type: "brave",
    api_key: "",
    api_key_configured: false,
    quota_limit: DEFAULT_WEB_SEARCH_QUOTA_LIMIT,
    subscribed_at: null,
    proxy_id: null,
    expires_at: null,
  } as WebSearchProviderConfig);
  expandedProviders[idx] = true;
}

function formatSubscribedAt(ts: number | null): string {
  if (!ts) return "";
  // Use UTC to avoid timezone drift on repeated edits
  const d = new Date(ts * 1000);
  const y = d.getUTCFullYear();
  const m = String(d.getUTCMonth() + 1).padStart(2, "0");
  const day = String(d.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function parseSubscribedAt(dateStr: string): number | null {
  if (!dateStr) return null;
  // Parse as UTC to match formatSubscribedAt
  return Math.floor(new Date(dateStr + "T00:00:00Z").getTime() / 1000);
}

function quotaPercentage(provider: WebSearchProviderConfig): number {
  if (!provider.quota_limit || provider.quota_limit <= 0) return 0;
  return ((provider.quota_used ?? 0) / provider.quota_limit) * 100;
}

async function resetWebSearchUsage(idx: number) {
  const provider = webSearchConfig.providers[idx];
  if (!provider) return;
  if (!confirm(t("admin.settings.webSearchEmulation.resetUsageConfirm")))
    return;
  try {
    await adminAPI.settings.resetWebSearchUsage({
      provider_type: provider.type,
    });
    provider.quota_used = 0;
    appStore.showSuccess(
      t("admin.settings.webSearchEmulation.resetUsageSuccess"),
    );
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t("common.error")));
  }
}

async function copyApiKey(idx: number) {
  const key = webSearchConfig.providers[idx]?.api_key;
  if (!key) {
    appStore.showError(
      t("admin.settings.webSearchEmulation.apiKeyPlaceholder"),
    );
    return;
  }
  try {
    await navigator.clipboard.writeText(key);
    appStore.showSuccess(t("admin.settings.webSearchEmulation.copied"));
  } catch {
    appStore.showError(t("common.error"));
  }
}

async function testWebSearchProvider() {
  wsTestLoading.value = true;
  wsTestResult.value = null;
  try {
    const query =
      wsTestQuery.value.trim() ||
      t("admin.settings.webSearchEmulation.testDefaultQuery");
    wsTestResult.value = await adminAPI.settings.testWebSearchEmulation(query);
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t("common.error")));
  } finally {
    wsTestLoading.value = false;
  }
}

async function loadWebSearchConfig() {
  try {
    const [resp, proxiesResp] = await Promise.all([
      adminAPI.settings.getWebSearchEmulationConfig(),
      adminAPI.proxies.list().catch(() => ({ items: [] as Proxy[] })),
    ]);
    if (resp) {
      webSearchConfig.enabled = resp.enabled || false;
      webSearchConfig.providers = resp.providers || [];
    }
    webSearchProxies.value = proxiesResp.items || [];
  } catch (err: unknown) {
    // 404 is expected when config hasn't been created yet; show error for other failures
    const status = (err as { status?: number })?.status;
    if (status !== 404 && status !== undefined) {
      appStore.showError(extractApiErrorMessage(err, t("common.error")));
    }
  }
}

async function saveWebSearchConfig(): Promise<boolean> {
  try {
    for (const p of webSearchConfig.providers) {
      const raw = p.quota_limit;
      if (raw != null && Number(raw) !== 0 && Number(raw) < 1) {
        appStore.showError(
          t("admin.settings.webSearchEmulation.quotaLimitMustBePositive"),
        );
        return false;
      }
    }
    const providers = webSearchConfig.providers.map(
      (p: WebSearchProviderConfig) => ({
        ...p,
        quota_limit: Number(p.quota_limit) > 0 ? Number(p.quota_limit) : null,
      }),
    );
    await adminAPI.settings.updateWebSearchEmulationConfig({
      enabled: webSearchConfig.enabled,
      providers,
    });
    return true;
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t("common.error")));
    return false;
  }
}

const forwardedClientIpHeaderSeparatorKeys = new Set([
  " ",
  ",",
  "，",
  "Enter",
  "Tab",
]);
const forwardedClientIpHeaderTokenPattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const maxForwardedClientIpHeaders = 16;

type ForwardedClientIpHeaderResult = "added" | "duplicate" | "invalid" | "full";

function normalizeForwardedClientIpHeader(raw: string): string {
  const header = raw.trim();
  if (!forwardedClientIpHeaderTokenPattern.test(header)) {
    return "";
  }

  return header
    .toLowerCase()
    .split("-")
    .map((part) => `${part.charAt(0).toUpperCase()}${part.slice(1)}`)
    .join("-");
}

function normalizeForwardedClientIpHeaders(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }

  const headers: string[] = [];
  const seen = new Set<string>();
  for (const raw of value) {
    if (typeof raw !== "string") {
      continue;
    }
    const header = normalizeForwardedClientIpHeader(raw);
    const key = header.toLowerCase();
    if (!header || seen.has(key) || headers.length >= maxForwardedClientIpHeaders) {
      continue;
    }
    seen.add(key);
    headers.push(header);
  }
  return headers;
}

function removeForwardedClientIpHeader(header: string) {
  form.forwarded_client_ip_headers = form.forwarded_client_ip_headers.filter(
    (item) => item !== header,
  );
}

function addForwardedClientIpHeader(raw: string): ForwardedClientIpHeaderResult {
  const header = normalizeForwardedClientIpHeader(raw);
  if (!header) {
    return "invalid";
  }
  if (
    form.forwarded_client_ip_headers.some(
      (item) => item.toLowerCase() === header.toLowerCase(),
    )
  ) {
    return "duplicate";
  }
  if (form.forwarded_client_ip_headers.length >= maxForwardedClientIpHeaders) {
    return "full";
  }
  form.forwarded_client_ip_headers = [
    ...form.forwarded_client_ip_headers,
    header,
  ];
  return "added";
}

function showForwardedClientIpHeaderError(result: ForwardedClientIpHeaderResult) {
  if (result === "invalid") {
    appStore.showError(t("admin.settings.apiKeyAcl.forwardedClientIpHeaderInvalid"));
  } else if (result === "full") {
    appStore.showError(
      t("admin.settings.apiKeyAcl.forwardedClientIpHeadersLimit", {
        max: maxForwardedClientIpHeaders,
      }),
    );
  }
}

function commitForwardedClientIpHeaderDraft() {
  const draft = forwardedClientIpHeaderDraft.value;
  if (!draft) {
    return;
  }
  const result = addForwardedClientIpHeader(draft);
  showForwardedClientIpHeaderError(result);
  forwardedClientIpHeaderDraft.value = "";
}

function handleForwardedClientIpHeaderKeydown(event: KeyboardEvent) {
  if (event.isComposing) {
    return;
  }
  if (forwardedClientIpHeaderSeparatorKeys.has(event.key)) {
    event.preventDefault();
    commitForwardedClientIpHeaderDraft();
    return;
  }
  if (
    event.key === "Backspace" &&
    !forwardedClientIpHeaderDraft.value &&
    form.forwarded_client_ip_headers.length > 0
  ) {
    form.forwarded_client_ip_headers.pop();
  }
}

function handleForwardedClientIpHeaderPaste(event: ClipboardEvent) {
  const text = event.clipboardData?.getData("text") || "";
  if (!text.trim()) {
    return;
  }
  event.preventDefault();

  let error: ForwardedClientIpHeaderResult | undefined;
  for (const token of text.split(/[,，;\r\n]+/)) {
    if (!token.trim()) {
      continue;
    }
    const result = addForwardedClientIpHeader(token);
    if (result === "invalid" || result === "full") {
      error = result;
    }
  }
  if (error) {
    showForwardedClientIpHeaderError(error);
  }
}

// Custom endpoint management
function addEndpoint() {
  form.custom_endpoints.push({ name: "", endpoint: "", description: "" });
}

function removeEndpoint(index: number) {
  form.custom_endpoints.splice(index, 1);
}

function formatTablePageSizeOptions(options: number[]): string {
  return options.join(", ");
}

function parseTablePageSizeOptionsInput(raw: string): number[] | null {
  const tokens = raw
    .split(",")
    .map((token) => token.trim())
    .filter((token) => token.length > 0);

  if (tokens.length === 0) {
    return null;
  }

  const parsed = tokens.map((token) => Number(token));
  if (parsed.some((value) => !Number.isInteger(value))) {
    return null;
  }

  const deduped = Array.from(new Set(parsed)).sort((a, b) => a - b);
  if (
    deduped.some(
      (value) => value < tablePageSizeMin || value > tablePageSizeMax,
    )
  ) {
    return null;
  }

  return deduped;
}

// ── codex_cli_only 黑/白名单结构化编辑（行 ↔ JSON）──
interface CodexClientRow {
  originator: string;
  uaContains: string; // 逗号分隔，序列化时拆成 ua_contains 数组
  skipEngineFingerprint?: boolean; // 仅白名单：命中即跳过引擎指纹门
}
const codexBlacklistRows = ref<CodexClientRow[]>([]);
const codexWhitelistRows = ref<CodexClientRow[]>([]);
const codexFingerprintRows = ref<FingerprintSignalRow[]>([]);
const codexFingerprintNoRequired = computed(
  () => !codexFingerprintRows.value.some((r) => r.required),
);
function addCodexFingerprintRow(): void {
  codexFingerprintRows.value.push({ type: "header_exact", match: "", required: false });
}
function removeCodexFingerprintRow(i: number): void {
  codexFingerprintRows.value.splice(i, 1);
}

function parseCodexEntriesToRows(raw: string): CodexClientRow[] {
  if (!raw || !raw.trim()) return [];
  try {
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr.map((e) => ({
      originator: typeof e?.originator === "string" ? e.originator : "",
      uaContains: Array.isArray(e?.ua_contains)
        ? e.ua_contains
            .filter((x: unknown) => typeof x === "string")
            .join(", ")
        : "",
      skipEngineFingerprint: e?.skip_engine_fingerprint === true,
    }));
  } catch {
    return [];
  }
}

function serializeCodexRowsToJSON(rows: CodexClientRow[]): string {
  const entries = rows
    .map((r) => {
      const entry: {
        originator: string;
        ua_contains: string[];
        skip_engine_fingerprint?: boolean;
      } = {
        originator: r.originator.trim(),
        ua_contains: r.uaContains
          .split(",")
          .map((s) => s.trim())
          .filter((s) => s.length > 0),
      };
      if (r.skipEngineFingerprint) entry.skip_engine_fingerprint = true;
      return entry;
    })
    .filter((e) => e.originator !== "" || e.ua_contains.length > 0);
  return entries.length > 0 ? JSON.stringify(entries) : "";
}

function addCodexBlacklistRow(): void {
  codexBlacklistRows.value.push({ originator: "", uaContains: "" });
}
function removeCodexBlacklistRow(i: number): void {
  codexBlacklistRows.value.splice(i, 1);
}
function addCodexWhitelistRow(): void {
  codexWhitelistRows.value.push({
    originator: "",
    uaContains: "",
    skipEngineFingerprint: false,
  });
}
function removeCodexWhitelistRow(i: number): void {
  codexWhitelistRows.value.splice(i, 1);
}

const codexVersions = ref<OpenAICodexVersionRelease[]>([]);
const codexVersionsLoading = ref(false);
const codexVersionsLoaded = ref(false);
const codexVersionsError = ref("");
const codexVersionsNextPage = ref<number | null>(null);
const codexLatestVersion = ref("");
const codexSyncLoading = ref(false);
const codexOriginatorOptions = codexOriginators.map((value) => ({ value, label: value }));
const codexVersionModeOptions = computed(() => [
  { value: "auto", label: t("admin.settings.gatewayForwarding.openaiCodexVersionModeAuto") },
  { value: "pinned", label: t("admin.settings.gatewayForwarding.openaiCodexVersionModePinned") },
]);
const codexTemplateVersion = computed(() => {
  const manual = form.openai_codex_client_version.trim();
  if (form.openai_codex_client_version_mode === "pinned" && isStableCodexVersion(manual)) {
    return manual;
  }
  return [manual, form.openai_codex_client_version_synced, form.openai_codex_client_version_default]
    .filter(isStableCodexVersion).sort((a, b) => compareCodexVersions(b, a))[0] || defaultCodexClientVersion;
});
const codexUserAgentOptions = computed(() => {
  const version = codexTemplateVersion.value;
  return codexOriginators.map((originator) => ({
    value: buildCodexUserAgentTemplate(originator, version),
    label: `${originator} / ${version} · ${t("admin.settings.gatewayForwarding.openaiCodexTemplate")}`,
  }));
});
const codexVersionOptions = computed(() => {
  const releases = new Map(codexVersions.value.map((release) => [release.version, release]));
  // Retain a saved/custom version even when it is outside the loaded page.
  const versions = new Set([
    ...releases.keys(), form.openai_codex_client_version.trim(),
    form.openai_codex_client_version_default, form.openai_codex_client_version_synced,
  ].filter(isStableCodexVersion));
  return [...versions].sort((a, b) => compareCodexVersions(b, a)).map((version) => {
    const release = releases.get(version);
    const date = release?.published_at?.slice(0, 10);
    return {
      value: version,
      label: `${version}${date ? ` · ${date}` : ""}${version === codexLatestVersion.value ? ` · ${t("admin.settings.gatewayForwarding.openaiCodexLatestLabel")}` : ""}`,
    };
  });
});

function applyCodexOriginatorPreset(value: unknown): void {
  if (typeof value !== "string") return;
  form.openai_codex_originator = value;
  form.openai_codex_user_agent = buildCodexUserAgentTemplate(
    value, codexTemplateVersion.value,
  );
}

function applyCodexUserAgentPreset(value: unknown): void {
  if (typeof value !== "string") return;
  form.openai_codex_user_agent = value;
  form.openai_codex_originator = value.split("/")[0] || "";
}

function syncCodexFormUserAgentVersion(): void {
  if (form.openai_codex_user_agent.trim()) {
    form.openai_codex_user_agent = setCodexTemplateVersion(
      form.openai_codex_user_agent.trim(), codexTemplateVersion.value,
    );
  }
}

function setCodexVersionMode(value: unknown): void {
  form.openai_codex_client_version_mode = value === "pinned" ? "pinned" : "auto";
  if (value === "pinned" && !form.openai_codex_client_version.trim()) {
    form.openai_codex_client_version = codexTemplateVersion.value;
  }
  syncCodexFormUserAgentVersion();
}

function onCodexVersionInput(): void {
  form.openai_codex_client_version_mode = form.openai_codex_client_version.trim() ? "pinned" : "auto";
  syncCodexFormUserAgentVersion();
}

function selectCodexVersion(value: unknown): void {
  if (typeof value !== "string") return;
  form.openai_codex_client_version = value;
  onCodexVersionInput();
}

async function loadCodexVersions(page = 1): Promise<void> {
  if (codexVersionsLoading.value) return;
  codexVersionsLoading.value = true;
  codexVersionsError.value = "";
  try {
    const result = await adminAPI.settings.getOpenAICodexVersions(page);
    const releases = new Map((page === 1 ? [] : codexVersions.value).map((release) => [release.version, release]));
    for (const release of result.versions) releases.set(release.version, release);
    codexVersions.value = [...releases.values()];
    if (isStableCodexVersion(result.latest_version) && (
      !codexLatestVersion.value || compareCodexVersions(result.latest_version, codexLatestVersion.value) > 0
    )) codexLatestVersion.value = result.latest_version;
    codexVersionsNextPage.value = result.has_more ? result.next_page : null;
  } catch (error) {
    codexVersionsError.value = extractApiErrorMessage(
      error, t("admin.settings.gatewayForwarding.openaiCodexVersionLoadFailed"),
    );
  } finally {
    codexVersionsLoaded.value = true;
    codexVersionsLoading.value = false;
  }
}

async function syncCodexVersion(): Promise<void> {
  if (codexSyncLoading.value || saving.value) return;
  codexSyncLoading.value = true;
  try {
    const result = await adminAPI.settings.syncOpenAICodexVersion();
    // This endpoint persists only the synced baseline. Preserve every unsaved
    // override, including a pinned historical version and the auto-sync toggle.
    form.openai_codex_client_version_synced = result.synced_version;
    form.openai_codex_originator_default = result.defaults.originator;
    form.openai_codex_user_agent_default = result.defaults.user_agent;
    form.openai_codex_client_version_default = result.defaults.client_version;
    codexLatestVersion.value = result.latest_version;
    appStore.showSuccess(t("admin.settings.gatewayForwarding.openaiCodexVersionSyncSuccess", {
      version: result.synced_version,
    }));
    await loadCodexVersions();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(
      error, t("admin.settings.gatewayForwarding.openaiCodexVersionSyncFailed"),
    ));
  } finally {
    codexSyncLoading.value = false;
  }
}

watch(activeTab, (tab) => {
  if (tab === "gpt" && !codexVersionsLoaded.value) void loadCodexVersions();
});

const codexSyncedVersionLabel = computed(() => {
  const synced = form.openai_codex_client_version_synced?.trim();
  if (!synced) return "";
  return t("admin.settings.gatewayForwarding.openaiCodexVersionSyncedValue", {
    version: synced,
  });
});

const claudeSyncedVersionLabel = computed(() => {
  const synced = form.claude_code_client_version_synced?.trim();
  if (!synced) return "";
  return t("admin.settings.gatewayForwarding.claudeCodeVersionSyncedValue", {
    version: synced,
  });
});

async function loadSettings() {
  loading.value = true;
  loadFailed.value = false;
  try {
    const settings = await adminAPI.settings.getSettings();
    // Only assign non-null values from backend (null means unconfigured, keep defaults)
    for (const [key, value] of Object.entries(settings)) {
      if (value !== null && value !== undefined) {
        (form as Record<string, unknown>)[key] = value;
      }
    }
    // For this optional override, null explicitly selects per-account rates.
    if (settings.openai_oauth_scheduling_rate_multiplier === null) {
      form.openai_oauth_scheduling_rate_multiplier = null;
    }
    syncCaptchaProviderSelection();
    if (!form.claude_oauth_system_prompt_blocks?.trim()) {
      form.claude_oauth_system_prompt_blocks =
        defaultClaudeOAuthSystemPromptBlocks;
    }
    claudeOAuthSystemPromptBlocks.value = parseClaudeOAuthSystemPromptBlocks(
      form.claude_oauth_system_prompt_blocks,
      form.claude_oauth_system_prompt,
    );
    syncClaudeOAuthSystemPromptBlocksFormField();
    codexBlacklistRows.value = parseCodexEntriesToRows(
      form.codex_cli_only_blacklist,
    );
    codexWhitelistRows.value = parseCodexEntriesToRows(
      form.codex_cli_only_whitelist,
    );
    codexFingerprintRows.value = form.codex_cli_only_engine_fingerprint_signals
      ? parseFingerprintSignalsToRows(form.codex_cli_only_engine_fingerprint_signals)
      : defaultFingerprintSignalRows();
    form.login_agreement_mode =
      settings.login_agreement_mode === "checkbox" ? "checkbox" : "modal";
    form.channel_monitor_mode =
      settings.channel_monitor_mode === "v2" ? "v2" : "v1";
    form.channel_monitor_hide_throughput = Boolean(
      settings.channel_monitor_hide_throughput
    );
    form.channel_monitor_show_quota = Boolean(
      settings.channel_monitor_show_quota
    );
    form.login_agreement_updated_at =
      settings.login_agreement_updated_at || "2026-03-31";
    form.login_agreement_documents =
      Array.isArray(settings.login_agreement_documents) &&
      settings.login_agreement_documents.length > 0
        ? settings.login_agreement_documents.map((doc) => ({
            id: doc.id || "",
            title: doc.title || "",
            content_md: doc.content_md || "",
          }))
        : defaultLoginAgreementDocuments();
    form.account_scheduling_thresholds = normalizeAccountSchedulingThresholdsMap(
      settings.account_scheduling_thresholds,
    );
    form.forwarded_client_ip_headers = normalizeForwardedClientIpHeaders(
      settings.forwarded_client_ip_headers,
    );
    forwardedClientIpHeaderDraft.value = "";
    tablePageSizeOptionsInput.value = formatTablePageSizeOptions(
      Array.isArray(settings.table_page_size_options)
        ? settings.table_page_size_options
        : [10, 20, 50, 100],
    );
    form.turnstile_secret_key = "";
    form.tencent_captcha_app_secret_key = "";
    form.tencent_captcha_cloud_secret_id = "";
    form.tencent_captcha_cloud_secret_key = "";
    form.aliyun_captcha_access_key_secret = "";

    // Load OpenAI fast/flex policy rules from bulk settings.
    // 仅当 payload 真的包含该字段时填充并标记为已加载；否则保持表单空值，
    // 让 saveSettings 在未加载时跳过该字段，防止覆盖后端默认规则。
    if (
      settings.openai_fast_policy_settings &&
      Array.isArray(settings.openai_fast_policy_settings.rules)
    ) {
      openaiFastPolicyForm.rules =
        settings.openai_fast_policy_settings.rules.map((rule) => ({
          ...rule,
          user_ids: [],
          model_whitelist: rule.model_whitelist
            ? [...rule.model_whitelist]
            : [],
        }));
      openaiFastPolicyLoaded.value = true;
    }

    await loadWebSearchConfig();
  } catch (error: unknown) {
    loadFailed.value = true;
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.failedToLoad")),
    );
  } finally {
    loading.value = false;
  }
}

async function saveSettings() {
  saving.value = true;
  try {
    const normalizedTableDefaultPageSize = Math.floor(
      Number(form.table_default_page_size),
    );
    if (
      !Number.isInteger(normalizedTableDefaultPageSize) ||
      normalizedTableDefaultPageSize < tablePageSizeMin ||
      normalizedTableDefaultPageSize > tablePageSizeMax
    ) {
      appStore.showError(
        t("admin.settings.site.tableDefaultPageSizeRangeError", {
          min: tablePageSizeMin,
          max: tablePageSizeMax,
        }),
      );
      return;
    }

    const normalizedTablePageSizeOptions = parseTablePageSizeOptionsInput(
      tablePageSizeOptionsInput.value,
    );
    if (!normalizedTablePageSizeOptions) {
      appStore.showError(
        t("admin.settings.site.tablePageSizeOptionsFormatError", {
          min: tablePageSizeMin,
          max: tablePageSizeMax,
        }),
      );
      return;
    }

    form.table_default_page_size = normalizedTableDefaultPageSize;
    form.table_page_size_options = normalizedTablePageSizeOptions;

    form.forwarded_client_ip_headers = normalizeForwardedClientIpHeaders(
      form.forwarded_client_ip_headers,
    );
    // Validate URL fields — novalidate disables browser-native checks, so we validate here
    const isValidHttpUrl = (url: string): boolean => {
      if (!url) return true;
      try {
        const u = new URL(url);
        return u.protocol === "http:" || u.protocol === "https:";
      } catch {
        return false;
      }
    };
    // Optional URL fields: auto-clear invalid values so they don't cause backend 400 errors
    if (!isValidHttpUrl(form.doc_url)) form.doc_url = "";
    const claudeOAuthSystemPromptBlocksJSON =
      serializeClaudeOAuthSystemPromptBlocksToJSON(
        claudeOAuthSystemPromptBlocks.value,
      );
    form.claude_oauth_system_prompt_blocks =
      claudeOAuthSystemPromptBlocksJSON;

    const oauthSchedulingRate = form.openai_oauth_scheduling_rate_multiplier;
    if (
      oauthSchedulingRate !== "" &&
      oauthSchedulingRate !== null &&
      (!Number.isFinite(oauthSchedulingRate) || oauthSchedulingRate < 0)
    ) {
      appStore.showError(t("admin.settings.openaiExperimentalScheduler.oauthRateInvalid"));
      return;
    }

    const payload: UpdateSettingsRequest = {
      site_name: form.site_name,
      site_logo: form.site_logo,
      site_subtitle: form.site_subtitle,
      api_base_url: form.api_base_url,
      contact_info: form.contact_info,
      doc_url: form.doc_url,
      home_content: form.home_content,
      compact_home_enabled: form.compact_home_enabled,
      hide_ccs_import_button: form.hide_ccs_import_button,
      table_default_page_size: form.table_default_page_size,
      table_page_size_options: form.table_page_size_options,
      api_key_acl_trust_forwarded_ip: form.api_key_acl_trust_forwarded_ip,
      forwarded_client_ip_headers: form.forwarded_client_ip_headers,
      enable_model_fallback: form.enable_model_fallback,
      fallback_model_anthropic: form.fallback_model_anthropic,
      fallback_model_openai: form.fallback_model_openai,
      grok_default_text_model: form.grok_default_text_model.trim() || "grok-4.6",
      grok_cross_client_model_map_enabled: form.grok_cross_client_model_map_enabled,
      grok_default_base_url_mode: form.grok_default_base_url_mode,
      min_claude_code_version: form.min_claude_code_version,
      max_claude_code_version: form.max_claude_code_version,
      allow_ungrouped_key_scheduling: form.allow_ungrouped_key_scheduling,
      openai_ttft_mode:
        form.openai_ttft_mode === "visible" ? "visible" : "semantic",
      enable_fingerprint_unification: form.enable_fingerprint_unification,
      enable_metadata_passthrough: form.enable_metadata_passthrough,
      enable_cch_signing: form.enable_cch_signing,
      enable_claude_oauth_system_prompt_injection:
        form.enable_claude_oauth_system_prompt_injection,
      claude_oauth_system_prompt: form.claude_oauth_system_prompt?.trim()
        ? form.claude_oauth_system_prompt
        : "",
      claude_oauth_system_prompt_blocks: claudeOAuthSystemPromptBlocksJSON,
      enable_anthropic_cache_ttl_1h_injection:
        form.enable_anthropic_cache_ttl_1h_injection,
      rewrite_message_cache_control: form.rewrite_message_cache_control,
      enable_client_dateline_normalization:
        form.enable_client_dateline_normalization,
      openai_codex_originator:
        form.openai_codex_originator?.trim() || "",
      openai_codex_user_agent:
        form.openai_codex_user_agent?.trim() || "",
      openai_codex_client_version:
        form.openai_codex_client_version?.trim() || "",
      openai_codex_client_version_mode: form.openai_codex_client_version?.trim()
        ? form.openai_codex_client_version_mode
        : "auto",
      enable_openai_account_local_device_identity:
        form.enable_openai_account_local_device_identity,
      openai_codex_version_auto_sync_enabled:
        form.openai_codex_version_auto_sync_enabled,
      claude_code_client_version: form.claude_code_client_version?.trim() || "",
      claude_code_version_auto_sync_enabled:
        form.claude_code_version_auto_sync_enabled,
      min_codex_version: form.min_codex_version?.trim() || "",
      max_codex_version: form.max_codex_version?.trim() || "",
      codex_cli_only_allow_app_server_clients:
        form.codex_cli_only_allow_app_server_clients,
      codex_cli_only_engine_fingerprint_signals: serializeFingerprintRowsToJSON(
        codexFingerprintRows.value,
      ),
      codex_cli_only_blacklist: serializeCodexRowsToJSON(
        codexBlacklistRows.value,
      ),
      codex_cli_only_whitelist: serializeCodexRowsToJSON(
        codexWhitelistRows.value,
      ),
      risk_control_enabled: form.risk_control_enabled,
      cyber_session_block_enabled: form.cyber_session_block_enabled,
      cyber_session_block_ttl_seconds:
        Number(form.cyber_session_block_ttl_seconds) || 3600,
      openai_low_upstream_rate_priority_enabled:
        form.openai_low_upstream_rate_priority_enabled,
      openai_oauth_scheduling_rate_multiplier:
        oauthSchedulingRate === "" ? null : oauthSchedulingRate,
      openai_advanced_scheduler_enabled: form.openai_advanced_scheduler_enabled,
      openai_advanced_scheduler_sticky_weighted_enabled:
        form.openai_advanced_scheduler_sticky_weighted_enabled,
      openai_advanced_scheduler_subscription_priority_enabled:
        form.openai_advanced_scheduler_subscription_priority_enabled,
      openai_advanced_scheduler_lb_top_k:
        form.openai_advanced_scheduler_lb_top_k.trim(),
      openai_advanced_scheduler_weight_priority:
        form.openai_advanced_scheduler_weight_priority.trim(),
      openai_advanced_scheduler_weight_load:
        form.openai_advanced_scheduler_weight_load.trim(),
      openai_advanced_scheduler_weight_queue:
        form.openai_advanced_scheduler_weight_queue.trim(),
      openai_advanced_scheduler_weight_error_rate:
        form.openai_advanced_scheduler_weight_error_rate.trim(),
      openai_advanced_scheduler_weight_ttft:
        form.openai_advanced_scheduler_weight_ttft.trim(),
      openai_advanced_scheduler_weight_reset:
        form.openai_advanced_scheduler_weight_reset.trim(),
      openai_advanced_scheduler_weight_quota_headroom:
        form.openai_advanced_scheduler_weight_quota_headroom.trim(),
      openai_advanced_scheduler_weight_upstream_cost:
        form.openai_advanced_scheduler_weight_upstream_cost.trim(),
      openai_advanced_scheduler_weight_previous_response:
        form.openai_advanced_scheduler_weight_previous_response.trim(),
      openai_advanced_scheduler_weight_session_sticky:
        form.openai_advanced_scheduler_weight_session_sticky.trim(),
      account_quota_notify_enabled: form.account_quota_notify_enabled,
      account_quota_notify_emails: (
        form.account_quota_notify_emails || []
      ).filter((entry) => entry.email.trim() !== ""),
    };

    // 仅当 openai_fast_policy_settings 已成功从后端加载时才回写，
    // 否则省略整个字段，让后端保留既有规则（含默认值）。
    if (openaiFastPolicyLoaded.value) {
      payload.openai_fast_policy_settings = {
        rules: openaiFastPolicyForm.rules.map((rule) => {
          const whitelist = (rule.model_whitelist || [])
            .map((p) => p.trim())
            .filter((p) => p !== "");
          const hasWhitelist = whitelist.length > 0;
          return {
            service_tier: rule.service_tier,
            action: rule.action,
            scope: rule.scope,
            user_ids: undefined,
            error_message:
              rule.action === "block" ? rule.error_message : undefined,
            model_whitelist: hasWhitelist ? whitelist : undefined,
            fallback_action: hasWhitelist
              ? rule.fallback_action || "pass"
              : undefined,
            fallback_error_message:
              hasWhitelist && rule.fallback_action === "block"
                ? rule.fallback_error_message
                : undefined,
          };
        }),
      };
    }

    payload.account_scheduling_thresholds = sanitizeAccountSchedulingThresholdsMap(
      form.account_scheduling_thresholds,
    );

    const updated = await settingsStepUp.run(() =>
      adminAPI.settings.updateSettings(payload),
    );
    for (const [key, value] of Object.entries(updated)) {
      if (key === "openai_fast_policy_settings") continue;
      if (value !== null && value !== undefined) {
        (form as Record<string, unknown>)[key] = value;
      }
    }
    if (updated.openai_oauth_scheduling_rate_multiplier === null) {
      form.openai_oauth_scheduling_rate_multiplier = null;
    }
    form.account_scheduling_thresholds = normalizeAccountSchedulingThresholdsMap(
      updated.account_scheduling_thresholds,
    );
    form.forwarded_client_ip_headers = normalizeForwardedClientIpHeaders(
      updated.forwarded_client_ip_headers,
    );
    forwardedClientIpHeaderDraft.value = "";
    tablePageSizeOptionsInput.value = formatTablePageSizeOptions(
      Array.isArray(updated.table_page_size_options)
        ? updated.table_page_size_options
        : [10, 20, 50, 100],
    );
    // Refresh OpenAI fast/flex policy from server response
    if (
      updated.openai_fast_policy_settings &&
      Array.isArray(updated.openai_fast_policy_settings.rules)
    ) {
      openaiFastPolicyForm.rules =
        updated.openai_fast_policy_settings.rules.map((rule) => ({
          ...rule,
          user_ids: [],
          model_whitelist: rule.model_whitelist
            ? [...rule.model_whitelist]
            : [],
        }));
      openaiFastPolicyLoaded.value = true;
    }
    // Save web search emulation config separately (errors handled internally)
    const wsOk = await saveWebSearchConfig();
    // Refresh cached settings so sidebar/header update immediately
    await appStore.fetchPublicSettings(true);
    await adminSettingsStore.fetch(true);
    if (wsOk) {
      appStore.showSuccess(t("admin.settings.settingsSaved"));
    }
  } catch (error: unknown) {
    // 用户取消 step-up 验证：静默返回，不弹错误
    if (isStepUpCancelled(error)) {
      return;
    }
    if (isStepUpBlocked(error)) {
      appStore.showError(
        stepUpBlockReason(error) === "STEP_UP_ADMIN_API_KEY_FORBIDDEN"
          ? t("stepUp.adminApiKeyForbidden")
          : t("stepUp.notEnabled"),
      );
      return;
    }
    // 开启 step-up 开关但本人未启用 2FA：给出可操作的专用提示
    if (
      (error as { reason?: string })?.reason === "STEP_UP_ENABLE_REQUIRES_TOTP"
    ) {
      appStore.showError(t("admin.settings.security.stepUpEnableRequiresTotp"));
      return;
    }
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.failedToSave")),
    );
  } finally {
    saving.value = false;
  }
}

function normalizeImageStorageConfig(
  config?: Partial<ImageStorageConfig> | null,
): ImageStorageConfig {
  return {
    enabled: config?.enabled === true,
    endpoint: typeof config?.endpoint === "string" ? config.endpoint : "",
    region:
      typeof config?.region === "string" && config.region.trim()
        ? config.region
        : "auto",
    bucket: typeof config?.bucket === "string" ? config.bucket : "",
    access_key_id:
      typeof config?.access_key_id === "string" ? config.access_key_id : "",
    secret_access_key: "",
    prefix:
      typeof config?.prefix === "string" && config.prefix.trim()
        ? config.prefix
        : "images/",
    public_base_url:
      typeof config?.public_base_url === "string"
        ? config.public_base_url
        : "",
    force_path_style: config?.force_path_style === true,
    presign_expiry_hours:
      typeof config?.presign_expiry_hours === "number" &&
      Number.isFinite(config.presign_expiry_hours) &&
      config.presign_expiry_hours > 0
        ? Math.trunc(config.presign_expiry_hours)
        : 24,
    max_download_bytes:
      typeof config?.max_download_bytes === "number" &&
      Number.isFinite(config.max_download_bytes) &&
      config.max_download_bytes > 0
        ? Math.trunc(config.max_download_bytes)
        : 33554432,
  };
}

function buildImageStoragePayload(): ImageStorageConfig {
  const presignExpiryHours = Number(imageStorageForm.presign_expiry_hours);
  const maxDownloadBytes = Number(imageStorageForm.max_download_bytes);
  return {
    enabled: imageStorageForm.enabled,
    endpoint: imageStorageForm.endpoint.trim(),
    region: imageStorageForm.region.trim() || "auto",
    bucket: imageStorageForm.bucket.trim(),
    access_key_id: imageStorageForm.access_key_id.trim(),
    secret_access_key: imageStorageForm.secret_access_key?.trim() || "",
    prefix: imageStorageForm.prefix.trim() || "images/",
    public_base_url: imageStorageForm.public_base_url.trim(),
    force_path_style: imageStorageForm.force_path_style,
    presign_expiry_hours:
      Number.isFinite(presignExpiryHours) && presignExpiryHours > 0
        ? Math.trunc(presignExpiryHours)
        : 24,
    max_download_bytes:
      Number.isFinite(maxDownloadBytes) && maxDownloadBytes > 0
        ? Math.trunc(maxDownloadBytes)
        : 33554432,
  };
}

async function loadImageStorageConfig() {
  imageStorageLoading.value = true;
  imageStorageLoadError.value = "";
  try {
    const response = await adminAPI.imageStorage.getConfig();
    Object.assign(imageStorageForm, normalizeImageStorageConfig(response.config));
    imageStorageSecretConfigured.value = response.secret_configured === true;
  } catch (error: unknown) {
    imageStorageLoadError.value = extractApiErrorMessage(
      error,
      t("admin.settings.imageStorage.loadFailed"),
    );
  } finally {
    imageStorageLoading.value = false;
  }
}

async function saveImageStorageConfig() {
  imageStorageSaving.value = true;
  const payload = buildImageStoragePayload();
  const suppliedSecret = Boolean(payload.secret_access_key);
  try {
    const updated = await settingsStepUp.run(() =>
      adminAPI.imageStorage.updateConfig(payload),
    );
    Object.assign(imageStorageForm, normalizeImageStorageConfig(updated));
    imageStorageSecretConfigured.value =
      imageStorageSecretConfigured.value || suppliedSecret;
    appStore.showSuccess(t("admin.settings.imageStorage.saved"));
  } catch (error: unknown) {
    if (isStepUpCancelled(error)) return;
    if (isStepUpBlocked(error)) {
      appStore.showError(
        stepUpBlockReason(error) === "STEP_UP_ADMIN_API_KEY_FORBIDDEN"
          ? t("stepUp.adminApiKeyForbidden")
          : t("stepUp.notEnabled"),
      );
      return;
    }
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.imageStorage.saveFailed")),
    );
  } finally {
    imageStorageSaving.value = false;
  }
}

async function testImageStorageConnection() {
  imageStorageTesting.value = true;
  try {
    const result = await adminAPI.imageStorage.testConnection(
      buildImageStoragePayload(),
    );
    if (result.ok) {
      appStore.showSuccess(
        result.message || t("admin.settings.imageStorage.testSuccess"),
      );
    } else {
      appStore.showError(
        result.message || t("admin.settings.imageStorage.testFailed"),
      );
    }
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.imageStorage.testFailed")),
    );
  } finally {
    imageStorageTesting.value = false;
  }
}

// Admin API Key 方法
async function loadAdminApiKey() {
  adminApiKeyLoading.value = true;
  try {
    const status = await adminAPI.settings.getAdminApiKey();
    adminApiKeyExists.value = status.exists;
    adminApiKeyMasked.value = status.masked_key;
  } catch (_error: unknown) {
    // Silent fail - admin API key status is non-critical
  } finally {
    adminApiKeyLoading.value = false;
  }
}

async function createAdminApiKey() {
  adminApiKeyOperating.value = true;
  try {
    const result = await adminAPI.settings.regenerateAdminApiKey();
    newAdminApiKey.value = result.key;
    adminApiKeyExists.value = true;
    adminApiKeyMasked.value =
      result.key.substring(0, 10) + "..." + result.key.slice(-4);
    appStore.showSuccess(t("admin.settings.adminApiKey.keyGenerated"));
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t("common.error")));
  } finally {
    adminApiKeyOperating.value = false;
  }
}

async function regenerateAdminApiKey() {
  if (!confirm(t("admin.settings.adminApiKey.regenerateConfirm"))) return;
  await createAdminApiKey();
}

async function deleteAdminApiKey() {
  if (!confirm(t("admin.settings.adminApiKey.deleteConfirm"))) return;
  adminApiKeyOperating.value = true;
  try {
    await adminAPI.settings.deleteAdminApiKey();
    adminApiKeyExists.value = false;
    adminApiKeyMasked.value = "";
    newAdminApiKey.value = "";
    appStore.showSuccess(t("admin.settings.adminApiKey.keyDeleted"));
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t("common.error")));
  } finally {
    adminApiKeyOperating.value = false;
  }
}

function copyNewKey() {
  navigator.clipboard
    .writeText(newAdminApiKey.value)
    .then(() => {
      appStore.showSuccess(t("admin.settings.adminApiKey.keyCopied"));
    })
    .catch(() => {
      appStore.showError(t("common.copyFailed"));
    });
}

async function loadUpstreamBillingProbeSettings() {
  upstreamBillingProbeLoading.value = true;
  try {
    Object.assign(
      upstreamBillingProbeForm,
      await adminAPI.accounts.getUpstreamBillingProbeSettings(),
    );
  } catch (_error: unknown) {
    // Keep defaults when this optional setting cannot be loaded.
  } finally {
    upstreamBillingProbeLoading.value = false;
  }
}

async function saveUpstreamBillingProbeSettings() {
  upstreamBillingProbeSaving.value = true;
  try {
    const updated = await adminAPI.accounts.updateUpstreamBillingProbeSettings({
      ...upstreamBillingProbeForm,
    });
    Object.assign(upstreamBillingProbeForm, updated);
    appStore.showSuccess(t("admin.settings.upstreamBillingProbe.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.upstreamBillingProbe.saveFailed"),
      ),
    );
  } finally {
    upstreamBillingProbeSaving.value = false;
  }
}

// Overload Cooldown 方法
async function loadOverloadCooldownSettings() {
  overloadCooldownLoading.value = true;
  try {
    const settings = await adminAPI.settings.getOverloadCooldownSettings();
    Object.assign(overloadCooldownForm, settings);
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    overloadCooldownLoading.value = false;
  }
}

async function saveOverloadCooldownSettings() {
  overloadCooldownSaving.value = true;
  try {
    const updated = await adminAPI.settings.updateOverloadCooldownSettings({
      enabled: overloadCooldownForm.enabled,
      cooldown_minutes: overloadCooldownForm.cooldown_minutes,
    });
    Object.assign(overloadCooldownForm, updated);
    appStore.showSuccess(t("admin.settings.overloadCooldown.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.overloadCooldown.saveFailed"),
      ),
    );
  } finally {
    overloadCooldownSaving.value = false;
  }
}

// Panel API Rate Limit 方法
async function loadPanelRateLimitSettings() {
  panelRateLimitLoading.value = true;
  try {
    const settings = await adminAPI.settings.getPanelRateLimitSettings();
    Object.assign(panelRateLimitForm, settings);
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    panelRateLimitLoading.value = false;
  }
}

async function savePanelRateLimitSettings() {
  panelRateLimitSaving.value = true;
  try {
    const updated = await adminAPI.settings.updatePanelRateLimitSettings({
      enabled: panelRateLimitForm.enabled,
      user_rpm: panelRateLimitForm.user_rpm,
      heavy_rpm: panelRateLimitForm.heavy_rpm,
      exempt_admin: panelRateLimitForm.exempt_admin,
      public_ip_rpm: panelRateLimitForm.public_ip_rpm,
    });
    Object.assign(panelRateLimitForm, updated);
    appStore.showSuccess(t("admin.settings.panelRateLimit.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.panelRateLimit.saveFailed"),
      ),
    );
  } finally {
    panelRateLimitSaving.value = false;
  }
}

// Rate Limit Cooldown (429) 方法
async function loadRateLimit429CooldownSettings() {
  rateLimit429CooldownLoading.value = true;
  try {
    const settings = await adminAPI.settings.getRateLimit429CooldownSettings();
    Object.assign(rateLimit429CooldownForm, settings);
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    rateLimit429CooldownLoading.value = false;
  }
}

async function saveRateLimit429CooldownSettings() {
  rateLimit429CooldownSaving.value = true;
  try {
    const updated = await adminAPI.settings.updateRateLimit429CooldownSettings({
      enabled: rateLimit429CooldownForm.enabled,
      cooldown_seconds: rateLimit429CooldownForm.cooldown_seconds,
    });
    Object.assign(rateLimit429CooldownForm, updated);
    appStore.showSuccess(t("admin.settings.rateLimit429Cooldown.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.rateLimit429Cooldown.saveFailed"),
      ),
    );
  } finally {
    rateLimit429CooldownSaving.value = false;
  }
}

// Stream Timeout 方法
async function loadStreamTimeoutSettings() {
  streamTimeoutLoading.value = true;
  try {
    const settings = await adminAPI.settings.getStreamTimeoutSettings();
    Object.assign(streamTimeoutForm, settings);
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    streamTimeoutLoading.value = false;
  }
}

async function saveStreamTimeoutSettings() {
  streamTimeoutSaving.value = true;
  try {
    const updated = await adminAPI.settings.updateStreamTimeoutSettings({
      enabled: streamTimeoutForm.enabled,
      action: streamTimeoutForm.action,
      temp_unsched_minutes: streamTimeoutForm.temp_unsched_minutes,
      threshold_count: streamTimeoutForm.threshold_count,
      threshold_window_minutes: streamTimeoutForm.threshold_window_minutes,
    });
    Object.assign(streamTimeoutForm, updated);
    appStore.showSuccess(t("admin.settings.streamTimeout.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(
        error,
        t("admin.settings.streamTimeout.saveFailed"),
      ),
    );
  } finally {
    streamTimeoutSaving.value = false;
  }
}

// Rectifier 方法
async function loadRectifierSettings() {
  rectifierLoading.value = true;
  try {
    const settings = await adminAPI.settings.getRectifierSettings();
    Object.assign(rectifierForm, settings);
    // 确保 patterns 是数组（旧数据可能为 null）
    if (!Array.isArray(rectifierForm.apikey_signature_patterns)) {
      rectifierForm.apikey_signature_patterns = [];
    }
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    rectifierLoading.value = false;
  }
}

async function saveRectifierSettings() {
  rectifierSaving.value = true;
  try {
    const updated = await adminAPI.settings.updateRectifierSettings({
      enabled: rectifierForm.enabled,
      thinking_signature_enabled: rectifierForm.thinking_signature_enabled,
      thinking_budget_enabled: rectifierForm.thinking_budget_enabled,
      apikey_signature_enabled: rectifierForm.apikey_signature_enabled,
      apikey_signature_patterns: rectifierForm.apikey_signature_patterns.filter(
        (p) => p.trim() !== "",
      ),
    });
    Object.assign(rectifierForm, updated);
    if (!Array.isArray(rectifierForm.apikey_signature_patterns)) {
      rectifierForm.apikey_signature_patterns = [];
    }
    appStore.showSuccess(t("admin.settings.rectifier.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.rectifier.saveFailed")),
    );
  } finally {
    rectifierSaving.value = false;
  }
}

const betaPolicyActionOptions = computed(() => [
  { value: "pass", label: t("admin.settings.betaPolicy.actionPass") },
  { value: "filter", label: t("admin.settings.betaPolicy.actionFilter") },
  { value: "block", label: t("admin.settings.betaPolicy.actionBlock") },
]);

const betaPolicyScopeOptions = computed(() => [
  { value: "all", label: t("admin.settings.betaPolicy.scopeAll") },
  { value: "oauth", label: t("admin.settings.betaPolicy.scopeOAuth") },
  { value: "apikey", label: t("admin.settings.betaPolicy.scopeAPIKey") },
  { value: "bedrock", label: t("admin.settings.betaPolicy.scopeBedrock") },
]);

// Beta Policy 方法
const betaDisplayNames: Record<string, string> = {
  "fast-mode-2026-02-01": "Fast Mode",
  "context-1m-2025-08-07": "Context 1M",
};

// 快捷预设：按 beta_token 定义预设方案
const betaPresets: Record<
  string,
  Array<{
    label: string;
    description: string;
    action: "pass" | "filter" | "block";
    model_whitelist: string[];
    fallback_action: "pass" | "filter" | "block";
  }>
> = {
  "context-1m-2025-08-07": [
    {
      label: t("admin.settings.betaPolicy.presetOpusOnly"),
      description: t("admin.settings.betaPolicy.presetOpusOnlyDesc"),
      action: "pass",
      model_whitelist: ["claude-opus-4-6"],
      fallback_action: "filter",
    },
  ],
};

// 常用模型模式（具体 ID + 通配符示例）
const commonModelPatterns = [
  "claude-opus-4-6",
  "claude-sonnet-4-6",
  "claude-opus-*",
  "claude-sonnet-*",
];

function getBetaDisplayName(token: string): string {
  return betaDisplayNames[token] || token;
}

function applyBetaPreset(
  rule: (typeof betaPolicyForm.rules)[number],
  preset: {
    action: "pass" | "filter" | "block";
    model_whitelist: string[];
    fallback_action: "pass" | "filter" | "block";
  },
) {
  rule.action = preset.action;
  rule.model_whitelist = [...preset.model_whitelist];
  rule.fallback_action = preset.fallback_action;
}

function addQuickPattern(
  rule: (typeof betaPolicyForm.rules)[number],
  pattern: string,
) {
  if (!rule.model_whitelist) rule.model_whitelist = [];
  if (!rule.model_whitelist.includes(pattern)) {
    rule.model_whitelist.push(pattern);
  }
}

async function loadBetaPolicySettings() {
  betaPolicyLoading.value = true;
  try {
    const settings = await adminAPI.settings.getBetaPolicySettings();
    betaPolicyForm.rules = settings.rules;
  } catch (_error: unknown) {
    // Silent fail - settings will use defaults
  } finally {
    betaPolicyLoading.value = false;
  }
}

// ==================== OpenAI Fast/Flex Policy ====================

const openaiFastPolicyTierOptions = computed(() => [
  { value: "all", label: t("admin.settings.openaiFastPolicy.tierAll") },
  {
    value: "priority",
    label: t("admin.settings.openaiFastPolicy.tierPriority"),
  },
  {
    value: "ultrafast",
    label: t("admin.settings.openaiFastPolicy.tierUltrafast"),
  },
  { value: "flex", label: t("admin.settings.openaiFastPolicy.tierFlex") },
  { value: "missing", label: t("admin.settings.openaiFastPolicy.tierMissing") },
]);

const openaiFastPolicyActionOptions = computed(() => [
  { value: "pass", label: t("admin.settings.openaiFastPolicy.actionPass") },
  { value: "filter", label: t("admin.settings.openaiFastPolicy.actionFilter") },
  {
    value: "force_priority",
    label: t("admin.settings.openaiFastPolicy.actionForcePriority"),
  },
  { value: "block", label: t("admin.settings.openaiFastPolicy.actionBlock") },
]);

function openaiFastPolicyActionSummary(
  action: OpenAIFastPolicyRule["action"],
) {
  return t(`admin.settings.openaiFastPolicy.summaryAction.${action}`);
}

function hasOpenAIFastPolicyTargetModels(rule: OpenAIFastPolicyRule) {
  return Boolean(rule.model_whitelist?.some((pattern) => pattern.trim() !== ""));
}

const openaiFastPolicyScopeOptions = computed(() => [
  { value: "all", label: t("admin.settings.openaiFastPolicy.scopeAll") },
  { value: "oauth", label: t("admin.settings.openaiFastPolicy.scopeOAuth") },
  { value: "apikey", label: t("admin.settings.openaiFastPolicy.scopeAPIKey") },
  {
    value: "bedrock",
    label: t("admin.settings.openaiFastPolicy.scopeBedrock"),
  },
]);

function addOpenAIFastPolicyRule() {
  openaiFastPolicyForm.rules.push({
    service_tier: "priority",
    action: "filter",
    scope: "all",
    user_ids: [],
    error_message: "",
    model_whitelist: [],
    fallback_action: "pass",
    fallback_error_message: "",
  });
}

function removeOpenAIFastPolicyRule(index: number) {
  openaiFastPolicyForm.rules.splice(index, 1);
}

function addOpenAIFastPolicyModelPattern(rule: OpenAIFastPolicyRule) {
  if (!rule.model_whitelist) rule.model_whitelist = [];
  rule.model_whitelist.push("");
}

function removeOpenAIFastPolicyModelPattern(
  rule: OpenAIFastPolicyRule,
  idx: number,
) {
  rule.model_whitelist?.splice(idx, 1);
}

async function saveBetaPolicySettings() {
  betaPolicySaving.value = true;
  try {
    // Clean up empty patterns before saving
    const cleanedRules = betaPolicyForm.rules.map((rule) => {
      const whitelist = rule.model_whitelist?.filter((p) => p.trim() !== "");
      const hasWhitelist = whitelist && whitelist.length > 0;
      return {
        beta_token: rule.beta_token,
        action: rule.action,
        scope: rule.scope,
        error_message: rule.error_message,
        model_whitelist: hasWhitelist ? whitelist : undefined,
        fallback_action: hasWhitelist
          ? rule.fallback_action || "pass"
          : undefined,
        fallback_error_message:
          hasWhitelist && rule.fallback_action === "block"
            ? rule.fallback_error_message
            : undefined,
      };
    });
    const updated = await adminAPI.settings.updateBetaPolicySettings({
      rules: cleanedRules,
    });
    betaPolicyForm.rules = updated.rules;
    appStore.showSuccess(t("admin.settings.betaPolicy.saved"));
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t("admin.settings.betaPolicy.saveFailed")),
    );
  } finally {
    betaPolicySaving.value = false;
  }
}

onMounted(() => {
  loadSettings();
  loadAdminApiKey();
  loadImageStorageConfig();
  loadUpstreamBillingProbeSettings();
  loadOverloadCooldownSettings();
  loadRateLimit429CooldownSettings();
  loadPanelRateLimitSettings();
  loadStreamTimeoutSettings();
  loadRectifierSettings();
  loadBetaPolicySettings();
});

</script>

<style scoped>
/* ============ 系统设置 Tab 导航 ============ */
.settings-tabs-shell {
  @apply sticky z-20 -mx-1 rounded-2xl border border-white/80 bg-white/90 p-1.5 backdrop-blur-xl;
  top: 4.75rem;
  box-shadow:
    0 12px 28px rgb(15 23 42 / 0.07),
    0 1px 0 rgb(255 255 255 / 0.9) inset;
}

.settings-tabs-scroll {
  @apply overflow-x-auto;
  -ms-overflow-style: none;
  scrollbar-width: none;
}

.settings-tabs-scroll::-webkit-scrollbar {
  display: none;
}

.settings-tabs {
  @apply flex min-w-max items-center gap-1;
}

.settings-tab {
  @apply relative isolate flex h-10 min-w-[6.75rem] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-xl border border-transparent px-3 text-sm font-medium text-gray-600 outline-none transition-colors duration-200 ease-out dark:text-gray-300;
}

@media (min-width: 768px) {
  .settings-tabs {
    @apply min-w-full;
  }

  .settings-tab {
    @apply min-w-max flex-1 px-2 text-[13px];
  }

  .settings-tab-icon {
    @apply h-6 w-6;
  }
}

.settings-tab::before {
  @apply absolute inset-0 -z-10 rounded-xl opacity-0 transition-opacity duration-200;
  content: "";
  background: linear-gradient(135deg, rgb(248 250 252 / 0.95), rgb(241 245 249 / 0.8));
}

.settings-tab:hover::before,
.settings-tab:focus-visible::before {
  opacity: 1;
}

.settings-tab:focus-visible {
  @apply ring-2 ring-primary-500/40 ring-offset-2 ring-offset-white dark:ring-offset-dark-900;
}

.settings-tab-active {
  @apply border-primary-200/80 bg-white text-primary-700 shadow-sm dark:border-primary-400/30 dark:bg-dark-700/95 dark:text-primary-200;
  box-shadow:
    0 8px 18px rgb(15 23 42 / 0.08),
    0 1px 0 rgb(255 255 255 / 0.92) inset;
}

.settings-tab-active::before {
  opacity: 0;
}

.settings-tab-active::after {
  position: absolute;
  right: 0.75rem;
  bottom: 0.25rem;
  left: 0.75rem;
  height: 2px;
  border-radius: 9999px;
  content: "";
  background: linear-gradient(90deg, #14b8a6, #0ea5e9);
}

.settings-tab-icon {
  @apply flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-gray-500 transition-colors duration-200 dark:text-gray-400;
}

.settings-tab:hover .settings-tab-icon,
.settings-tab:focus-visible .settings-tab-icon {
  @apply text-gray-700 dark:text-gray-200;
}

.settings-tab-active .settings-tab-icon {
  @apply bg-primary-50 text-primary-600 dark:bg-primary-400/10 dark:text-primary-300;
}

.settings-tab-label {
  @apply min-w-0 overflow-hidden text-ellipsis whitespace-nowrap leading-none;
}
</style>

<style>
/* Dark-mode overrides for the settings tabs shell. Kept in an UNSCOPED block
   because Vue's scoped-CSS compiler was dropping the `:global(.dark) ...`
   rules in the production build, leaving inactive tabs unreadable on dark. */
.dark .settings-tabs-shell {
  border-color: rgb(51 65 85 / 0.65);
  background: rgb(15 23 42 / 0.86);
  box-shadow:
    0 16px 36px rgb(0 0 0 / 0.28),
    0 1px 0 rgb(255 255 255 / 0.06) inset;
}

.dark .settings-tab::before {
  background: linear-gradient(135deg, rgb(30 41 59 / 0.9), rgb(51 65 85 / 0.62));
}

.dark .settings-tab-active {
  box-shadow:
    0 12px 26px rgb(0 0 0 / 0.22),
    0 1px 0 rgb(255 255 255 / 0.08) inset;
}
</style>
