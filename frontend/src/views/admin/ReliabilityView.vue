<template>
  <AppLayout>
    <div class="admin-workspace space-y-5" data-testid="reliability-page">
      <AdminPageHeader
        :eyebrow="t('admin.reliability.eyebrow')"
        :title="t('admin.reliability.title')"
        :description="t('admin.reliability.description')"
      >
        <template #actions>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="pageBusy"
            :title="t('admin.reliability.refresh')"
            @click="loadPageData"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': pageLoading }" />
            {{ pageLoading ? t('admin.reliability.refreshing') : t('admin.reliability.refresh') }}
          </button>
        </template>
      </AdminPageHeader>

      <div
        v-if="loadError"
        class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >
        <span class="flex items-start gap-2">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
          {{ loadError }}
        </span>
        <button type="button" class="btn btn-secondary text-xs" @click="loadData">
          {{ t('admin.reliability.retry') }}
        </button>
      </div>

      <section class="admin-surface p-4 sm:p-5" data-testid="turn-state-status">
        <div class="flex items-start gap-3">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-teal-50 text-teal-700 dark:bg-teal-950/40 dark:text-teal-300" aria-hidden="true">
            <Icon name="sync" size="md" />
          </span>
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.reliability.turnState.title') }}</h2>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.description') }}</p>
          </div>
        </div>
        <div
          class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700"
          data-testid="turn-state-settings"
        >
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
            {{ t('admin.reliability.turnState.settingsTitle') }}
          </h3>
          <div class="mt-3 grid gap-x-6 gap-y-3 sm:grid-cols-2">
            <div class="flex min-h-14 items-center justify-between gap-4">
              <div class="min-w-0">
                <p id="turn-state-probe-label" class="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {{ t('admin.reliability.turnState.probeToggle') }}
                </p>
                <p class="mt-1 min-h-5 text-xs text-gray-500 dark:text-dark-400">
                  {{ settingStatusLabel('probe', turnStateProbeEnabled) }}
                </p>
              </div>
              <Toggle
                data-testid="turn-state-probe-toggle"
                :model-value="turnStateProbeEnabled"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                aria-labelledby="turn-state-probe-label"
                @update:model-value="saveTurnStateSetting('probe', $event)"
              />
            </div>
            <div class="flex min-h-14 items-center justify-between gap-4">
              <div class="min-w-0">
                <p id="turn-state-injection-label" class="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {{ t('admin.reliability.turnState.injectionToggle') }}
                </p>
                <p class="mt-1 min-h-5 text-xs text-gray-500 dark:text-dark-400">
                  {{ settingStatusLabel('injection', turnStateCacheInjectionEnabled) }}
                </p>
              </div>
              <Toggle
                data-testid="turn-state-injection-toggle"
                :model-value="turnStateCacheInjectionEnabled"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                aria-labelledby="turn-state-injection-label"
                @update:model-value="saveTurnStateSetting('injection', $event)"
              />
            </div>
          </div>
          <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="turn-state-harvest-policy">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h4 class="text-sm font-medium text-gray-900 dark:text-gray-100">
                {{ t('admin.reliability.turnState.harvestPolicyTitle') }}
              </h4>
              <button
                type="button"
                class="btn btn-secondary shrink-0 text-xs"
                data-testid="turn-state-harvest-policy-save"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded || !turnStatePolicyValid"
                @click="saveTurnStatePolicy"
              >
                <Icon name="check" size="sm" />
                {{ turnStatePolicySaving ? t('admin.reliability.turnState.settingsSaving') : t('admin.reliability.turnState.harvestPolicySave') }}
              </button>
            </div>
            <div
              class="mt-3 grid grid-cols-2 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600 sm:grid-cols-4"
              data-testid="turn-state-speed-preset"
              role="group"
              :aria-label="t('admin.reliability.turnState.speedPreset')"
            >
              <button
                v-for="preset in turnStateSpeedPresets"
                :key="preset"
                type="button"
                class="min-h-10 border-gray-200 px-3 py-2 text-xs font-medium transition-colors dark:border-dark-600"
                :class="turnStateSpeedPreset === preset
                  ? 'bg-teal-50 text-teal-800 dark:bg-teal-950/40 dark:text-teal-200'
                  : 'bg-white text-gray-600 hover:bg-gray-50 dark:bg-dark-800 dark:text-dark-300 dark:hover:bg-dark-700'"
                :aria-pressed="turnStateSpeedPreset === preset"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                @click="selectSpeedPreset(preset)"
              >
                {{ speedPresetLabel(preset) }}
              </button>
            </div>
            <div class="mt-3 grid gap-3 sm:grid-cols-2">
              <label class="block text-xs font-medium text-gray-600 dark:text-dark-300">
                {{ t('admin.reliability.turnState.maxRequestsPerRound') }}
                <input
                  v-model.number="turnStateMaxRequestsPerRound"
                  type="number"
                  class="input mt-1 w-full"
                  data-testid="turn-state-max-requests"
                  :min="maxRequestsBounds.min"
                  :max="maxRequestsBounds.max"
                  :step="maxRequestsBounds.step"
                  :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                />
              </label>
              <label class="block text-xs font-medium text-gray-600 dark:text-dark-300">
                {{ t('admin.reliability.turnState.failureCooldownSeconds') }}
                <input
                  v-model.number="turnStateFailureCooldownSeconds"
                  type="number"
                  class="input mt-1 w-full"
                  data-testid="turn-state-failure-cooldown"
                  :min="failureCooldownBounds.min"
                  :max="failureCooldownBounds.max"
                  :step="failureCooldownBounds.step"
                  :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                />
              </label>
            </div>
          </div>
          <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="turn-state-proxy-pool-settings">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <label for="turn-state-proxy-pool-input" class="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {{ t('admin.reliability.turnState.proxyPoolTitle') }}
                </label>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">
                  {{ t('admin.reliability.turnState.proxyPoolDescription') }}
                </p>
              </div>
              <button
                type="button"
                class="btn btn-secondary shrink-0 text-xs"
                data-testid="turn-state-proxy-pool-save"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded || proxyPoolParse.invalid > 0"
                @click="saveTurnStateProxyPool"
              >
                <Icon name="check" size="sm" />
                {{ turnStateProxyPoolSaving ? t('admin.reliability.turnState.settingsSaving') : t('admin.reliability.turnState.proxyPoolSave') }}
              </button>
            </div>
            <textarea
              v-model="turnStateProxyPoolText"
              id="turn-state-proxy-pool-input"
              data-testid="turn-state-proxy-pool-input"
              class="input mt-3 min-h-28 w-full resize-y font-mono text-xs"
              rows="5"
              spellcheck="false"
              :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
              :placeholder="t('admin.reliability.turnState.proxyPoolPlaceholder')"
              @input="turnStateProxyPoolError = ''"
            ></textarea>
            <p v-if="turnStateSettingsHasProxyPool" class="input-hint mt-2 text-amber-700 dark:text-amber-300">
              {{ t('admin.reliability.turnState.proxyPoolConfiguredHint') }}
            </p>
            <p class="input-hint mt-2">{{ t('admin.reliability.turnState.proxyPoolHint') }}</p>
            <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs" data-testid="turn-state-proxy-pool-parse">
              <span class="text-gray-600 dark:text-dark-300">{{ t('admin.reliability.turnState.proxyPoolValid', { count: proxyPoolParse.valid }) }}</span>
              <span v-if="proxyPoolParse.invalid > 0" class="text-red-700 dark:text-red-300">{{ t('admin.reliability.turnState.proxyPoolInvalid', { count: proxyPoolParse.invalid }) }}</span>
              <span v-else class="text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.proxyPoolInvalid', { count: 0 }) }}</span>
            </div>
            <p v-if="turnStateProxyPoolError" class="mt-2 text-xs text-red-700 dark:text-red-300" data-testid="turn-state-proxy-pool-error" role="alert">
              {{ turnStateProxyPoolError }}
            </p>
          </div>
          <div
            v-if="turnStateSettingsError"
            class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-red-700 dark:text-red-300"
            data-testid="turn-state-settings-error"
            role="alert"
          >
            <span>{{ turnStateSettingsError }}</span>
            <button
              v-if="!turnStateSettingsLoaded"
              type="button"
              class="font-medium text-red-700 underline decoration-red-300 underline-offset-2 hover:text-red-800 disabled:cursor-not-allowed disabled:opacity-60 dark:text-red-300 dark:hover:text-red-200"
              :disabled="turnStateSettingsBusy"
              @click="loadTurnStateSettings"
            >
              {{ t('admin.reliability.turnState.settingsRetry') }}
            </button>
          </div>
        </div>
        <dl class="mt-4 grid gap-3 border-t border-gray-100 pt-4 sm:grid-cols-2 dark:border-dark-700 lg:grid-cols-3 xl:grid-cols-5">
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.supported') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateSupported) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.http') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateHTTP) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.websocket') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateWebSocket) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.httpCrossAccountProtection') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-http-protection">{{ protectionLabel(turnStateHTTPCrossAccountProtection) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.websocketCrossAccountProtection') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-websocket-protection">{{ protectionLabel(turnStateWebSocketCrossAccountProtection) }}</dd>
          </div>
        </dl>
        <div
          v-if="turnStateCollector"
          class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700"
          data-testid="turn-state-collector"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
              {{ t('admin.reliability.turnState.collectorTitle') }}
            </h3>
            <span
              class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium"
              :class="collectorStatusToneClass"
              data-testid="turn-state-collector-status"
            >
              {{ collectorStatusLabel }}
            </span>
          </div>
          <dl class="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorReady') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ collectorReadyLabel(collectorReady) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorInjection') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-injection">{{ collectorInjectionLabel(collectorInjection) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCollecting') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ collectorCollectingLabel(collectorCollecting) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorEntries') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.active_entries) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCandidates') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.ready_candidates) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorObservations') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.observations) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorSuccesses') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.successes) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorFailures') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums" :class="numeric(turnStateCollector.failures) > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-gray-100'">{{ formatCount(turnStateCollector.failures) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorBudget') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100" data-testid="turn-state-budget">{{ formatCount(turnStateCollector.budget_used) }} / {{ formatCount(turnStateCollector.budget_limit) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorBudgetReset') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-budget-reset">{{ formatTimestamp(turnStateCollector.budget_reset_at) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCookies') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100" data-testid="turn-state-cookie-count">{{ formatCount(turnStateCollector.cookie_count) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCookiesActive') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-green-700 dark:text-green-300" data-testid="turn-state-cookie-active-count">{{ formatCount(turnStateCollector.cookie_active_count) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCookiesExpired') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums" :class="numeric(turnStateCollector.cookie_expired_count) > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-gray-100'" data-testid="turn-state-cookie-expired-count">{{ formatCount(turnStateCollector.cookie_expired_count) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCookieRemaining') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100" data-testid="turn-state-cookie-remaining">{{ formatDuration(turnStateCollector.cookie_remaining_seconds) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastSuccess') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-last-success">{{ formatTimestamp(turnStateCollector.last_success_at) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastFailure') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-last-failure">{{ formatTimestamp(turnStateCollector.last_failure_at) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastError') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-amber-700 dark:text-amber-300" data-testid="turn-state-collector-last-error">{{ collectorLastErrorLabel }}</dd>
            </div>
          </dl>
          <section class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="turn-state-collector-nodes">
            <div class="flex items-center justify-between gap-2">
              <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                {{ t('admin.reliability.turnState.collectorNodes') }}
              </h4>
              <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">
                {{ t('admin.reliability.turnState.collectorNodeCount', { count: collectorNodes.length }) }}
              </span>
            </div>
            <div v-if="collectorNodes.length" class="mt-2 overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
              <table class="w-full min-w-[44rem] divide-y divide-gray-100 text-left text-xs dark:divide-dark-700">
                <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                  <tr>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.collectorNode') }}</th>
                    <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.collectorSuccesses') }}</th>
                    <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.collectorFailures') }}</th>
                    <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.collectorConsecutiveFailures') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.collectorCooldown') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.collectorLastResult') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="(node, index) in collectorNodes" :key="`${node.node_id}-${index}`">
                    <td class="px-3 py-2 text-gray-800 dark:text-gray-200">
                      <span class="block font-medium">{{ safeNodeText(node.label || node.node_id) }}</span>
                      <span v-if="node.label && node.node_id" class="mt-0.5 block font-mono text-[11px] text-gray-500 dark:text-dark-400">{{ safeNodeText(node.node_id) }}</span>
                    </td>
                    <td class="px-3 py-2 text-right font-semibold tabular-nums text-gray-800 dark:text-gray-200">{{ formatCount(node.successes) }}</td>
                    <td class="px-3 py-2 text-right font-semibold tabular-nums text-gray-800 dark:text-gray-200">{{ formatCount(node.failures) }}</td>
                    <td class="px-3 py-2 text-right font-semibold tabular-nums" :class="numeric(node.consecutive_failures) > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-800 dark:text-gray-200'">{{ formatCount(node.consecutive_failures) }}</td>
                    <td class="whitespace-nowrap px-3 py-2 tabular-nums text-gray-700 dark:text-gray-300">{{ formatDuration(node.cooldown_remaining_seconds) }}</td>
                    <td class="whitespace-nowrap px-3 py-2 text-gray-700 dark:text-gray-300">{{ collectorNodeResultLabel(node.last_result) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="mt-2 rounded-lg bg-gray-50 px-3 py-3 text-xs text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
              {{ t('admin.reliability.turnState.collectorNodesEmpty') }}
            </p>
          </section>
          <div class="mt-4 grid gap-4 border-t border-gray-100 pt-4 dark:border-dark-700 lg:grid-cols-2">
            <section data-testid="turn-state-proxy-pool" class="min-w-0">
              <div class="flex items-center justify-between gap-2">
                <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                  {{ t('admin.reliability.turnState.proxyPoolStatus') }}
                </h4>
                <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">
                  {{ t('admin.reliability.turnState.proxyPoolCount', { count: proxyPoolEntries.length }) }}
                </span>
              </div>
              <div v-if="proxyPoolEntries.length" class="mt-2 overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
                <table class="min-w-full divide-y divide-gray-100 text-left text-xs dark:divide-dark-700">
                  <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                    <tr>
                      <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.proxyProtocol') }}</th>
                      <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.proxyHost') }}</th>
                      <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.proxyPort') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                    <tr v-for="(proxy, index) in proxyPoolEntries" :key="`${proxy.protocol}-${proxy.host}-${proxy.port}-${index}`">
                      <td class="whitespace-nowrap px-3 py-2 font-medium uppercase text-gray-800 dark:text-gray-200">{{ proxy.protocol || t('admin.reliability.turnState.unknownValue') }}</td>
                      <td class="max-w-48 truncate px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{{ proxy.host || t('admin.reliability.turnState.unknownValue') }}</td>
                      <td class="whitespace-nowrap px-3 py-2 tabular-nums text-gray-700 dark:text-gray-300">{{ proxy.port || t('admin.reliability.turnState.unknownValue') }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p v-else class="mt-2 rounded-lg bg-gray-50 px-3 py-3 text-xs text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                {{ t('admin.reliability.turnState.proxyPoolEmpty') }}
              </p>
            </section>

            <section data-testid="turn-state-ip-regions" class="min-w-0">
              <div class="flex items-center justify-between gap-2">
                <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                  {{ t('admin.reliability.turnState.successfulIPRegions') }}
                </h4>
                <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400" data-testid="turn-state-ip-observations-total">{{ successfulIPTotalLabel }}</span>
              </div>
              <div v-if="successfulIPRegions.length" class="mt-2 overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
                <table class="min-w-full divide-y divide-gray-100 text-left text-xs dark:divide-dark-700">
                  <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                    <tr>
                      <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.region') }}</th>
                      <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.successCount') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                    <tr v-for="(region, index) in successfulIPRegions" :key="`${region.region}-${index}`">
                      <td class="px-3 py-2 text-gray-800 dark:text-gray-200">{{ regionLabel(region.region, region.country, region.country_code) }}</td>
                      <td class="px-3 py-2 text-right font-semibold tabular-nums text-gray-800 dark:text-gray-200">{{ formatCount(region.count ?? region.successes) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p v-else class="mt-2 rounded-lg bg-gray-50 px-3 py-3 text-xs text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                {{ t('admin.reliability.turnState.noSuccessfulIPs') }}
              </p>
            </section>
          </div>

          <section class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="turn-state-successful-ips">
            <div class="flex items-center justify-between gap-2">
              <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                {{ t('admin.reliability.turnState.successfulIPs') }}
              </h4>
              <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.ipCount', { count: successfulIPs.length }) }}</span>
            </div>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.ipObservationsHint') }}</p>
            <div v-if="successfulIPs.length" class="mt-2 overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
              <table class="w-full min-w-[42rem] divide-y divide-gray-100 text-left text-xs dark:divide-dark-700">
                <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                  <tr>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.ipAddress') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.region') }}</th>
                    <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.successCount') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.lastSuccess') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="(entry, index) in successfulIPs" :key="`${entry.ip || entry.address || entry.ip_address}-${index}`">
                    <td class="whitespace-nowrap px-3 py-2 font-mono text-gray-800 dark:text-gray-200">{{ entry.ip || entry.address || entry.ip_address || t('admin.reliability.turnState.unknownValue') }}</td>
                    <td class="whitespace-nowrap px-3 py-2 text-gray-700 dark:text-gray-300">{{ regionLabel(entry.region || entry.area, entry.country, entry.country_code) }}</td>
                    <td class="px-3 py-2 text-right font-semibold tabular-nums text-gray-800 dark:text-gray-200">{{ formatCount(entry.successes ?? entry.count) }}</td>
                    <td class="whitespace-nowrap px-3 py-2 text-gray-700 dark:text-gray-300">{{ formatTimestamp(entry.last_success_at) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="mt-2 rounded-lg bg-gray-50 px-3 py-3 text-xs text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
              {{ t('admin.reliability.turnState.noSuccessfulIPs') }}
            </p>
          </section>

          <section class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="turn-state-candidate-breakdown">
            <div class="flex items-center justify-between gap-2">
              <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
                {{ t('admin.reliability.turnState.candidateBreakdown') }}
              </h4>
              <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.candidateTotal', { count: candidateTotal }) }}</span>
            </div>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.candidateBreakdownHint') }}</p>
            <div v-if="candidateBreakdown.length" class="mt-2 overflow-x-auto rounded-lg border border-gray-100 dark:border-dark-700">
              <table class="min-w-full divide-y divide-gray-100 text-left text-xs dark:divide-dark-700">
                <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
                  <tr>
                    <th class="px-3 py-2 font-medium">{{ t('admin.reliability.turnState.candidateReason') }}</th>
                    <th class="px-3 py-2 text-right font-medium">{{ t('admin.reliability.turnState.candidateCount') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="(candidate, index) in candidateBreakdown" :key="`${candidate.reason || candidate.code || candidate.cause}-${index}`">
                    <td class="px-3 py-2 text-gray-800 dark:text-gray-200">{{ candidateReasonLabel(candidate.reason || candidate.code || candidate.cause) }}</td>
                    <td class="px-3 py-2 text-right font-semibold tabular-nums text-gray-800 dark:text-gray-200">{{ formatCount(candidate.count) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="mt-2 rounded-lg bg-gray-50 px-3 py-3 text-xs text-gray-500 dark:bg-dark-800/70 dark:text-dark-400">
              {{ t('admin.reliability.turnState.noCandidateBreakdown') }}
            </p>
          </section>
        </div>
        <p class="mt-4 flex items-start gap-2 rounded-xl bg-gray-50 px-3 py-2.5 text-xs leading-5 text-gray-600 dark:bg-dark-800/70 dark:text-dark-300">
          <Icon name="shield" size="sm" class="mt-0.5 shrink-0 text-teal-700 dark:text-teal-300" />
          {{ t('admin.reliability.turnState.privacy') }}
        </p>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import {
  normalizeReliabilityStatus,
  reliabilityTurnStateSpeedPresets as defaultTurnStateSpeedPresets,
  reliabilityAPI,
  type ReliabilityStatusResponse,
  type ReliabilityTurnStateCandidateBreakdown,
  type ReliabilityTurnStateCollectorNodeSummary,
  type ReliabilityTurnStateIPRegionSummary,
  type ReliabilityTurnStateNumberBounds,
  type ReliabilityTurnStateProxyPoolEntry,
  type ReliabilityTurnStateSettings,
  type ReliabilityTurnStateSettingsUpdate,
  type ReliabilityTurnStateSpeedPreset,
  type ReliabilityTurnStateSpeedPresetValues,
  type ReliabilityTurnStateSuccessfulIP,
} from '@/api/admin/reliability'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatNumber } from '@/utils/format'

const { t, locale } = useI18n()
const appStore = useAppStore()
const builtInSpeedPresetValues: Record<ReliabilityTurnStateSpeedPreset, ReliabilityTurnStateSpeedPresetValues> = {
  slow: { max_requests_per_round: 3, failure_cooldown_seconds: 300 },
  standard: { max_requests_per_round: 6, failure_cooldown_seconds: 180 },
  fast: { max_requests_per_round: 12, failure_cooldown_seconds: 60 },
  burst: { max_requests_per_round: 20, failure_cooldown_seconds: 1 },
}

const loading = ref(false)
const loadError = ref('')
const status = ref<ReliabilityStatusResponse | null>(null)
const turnStateSettingsLoading = ref(false)
const turnStateSettingsLoaded = ref(false)
const turnStateSettingsError = ref('')
const turnStateSettingSaving = ref<'probe' | 'injection' | 'policy' | 'proxy_pool' | null>(null)
const turnStateProbeEnabled = ref(true)
const turnStateCacheInjectionEnabled = ref(true)
const turnStateSpeedPreset = ref<ReliabilityTurnStateSpeedPreset>('standard')
const turnStateSpeedPresets = ref<ReliabilityTurnStateSpeedPreset[]>([...defaultTurnStateSpeedPresets])
const turnStateSpeedPresetValues = ref<Partial<Record<ReliabilityTurnStateSpeedPreset, ReliabilityTurnStateSpeedPresetValues>>>({ ...builtInSpeedPresetValues })
const turnStateMaxRequestsPerRound = ref<number | ''>(6)
const turnStateFailureCooldownSeconds = ref<number | ''>(180)
const maxRequestsBounds = ref<ReliabilityTurnStateNumberBounds>({ min: 1, max: 100, step: 1 })
const failureCooldownBounds = ref<ReliabilityTurnStateNumberBounds>({ min: 1, max: 3600, step: 1 })
const turnStateProxyPoolText = ref('')
const turnStateProxyPoolError = ref('')
const turnStateProxyPoolSaving = computed(() => turnStateSettingSaving.value === 'proxy_pool')
const turnStatePolicySaving = computed(() => turnStateSettingSaving.value === 'policy')
const turnStateSettingsHasProxyPool = ref(false)

const turnStateSettingsBusy = computed(() => turnStateSettingsLoading.value || turnStateSettingSaving.value !== null)
const pageLoading = computed(() => loading.value || turnStateSettingsLoading.value)
const pageBusy = computed(() => pageLoading.value || turnStateSettingSaving.value !== null)

const turnState = computed(() => normalizeReliabilityStatus(status.value ?? {}).turn_state ?? {})
const turnStateCollector = computed(() => {
  const collector = turnState.value.collector
  return collector && typeof collector === 'object' ? collector : null
})

const proxyPoolParse = computed(() => parseProxyPoolText(turnStateProxyPoolText.value))
const turnStatePolicyValid = computed(() => (
  isIntegerInBounds(turnStateMaxRequestsPerRound.value, maxRequestsBounds.value)
  && isIntegerInBounds(turnStateFailureCooldownSeconds.value, failureCooldownBounds.value)
  && turnStateSpeedPresets.value.includes(turnStateSpeedPreset.value)
))
const collectorNodes = computed<ReliabilityTurnStateCollectorNodeSummary[]>(() => {
  const value: unknown = turnStateCollector.value?.nodes
  if (!Array.isArray(value)) return []
  return value.flatMap((entry) => (
    entry && typeof entry === 'object' ? [entry as ReliabilityTurnStateCollectorNodeSummary] : []
  ))
})

const proxyPoolEntries = computed<ReliabilityTurnStateProxyPoolEntry[]>(() => {
  const value: unknown = turnStateCollector.value?.proxy_pool
  const entries = Array.isArray(value)
    ? value
    : value && typeof value === 'object' && Array.isArray((value as { entries?: unknown }).entries)
      ? (value as { entries: unknown[] }).entries
      : []
  return entries.flatMap((entry) => {
    if (entry && typeof entry === 'object') return [entry as ReliabilityTurnStateProxyPoolEntry]
    if (typeof entry === 'string') {
      const parsed = parseProxyPoolDisplay(entry)
      return parsed ? [parsed] : []
    }
    return []
  })
})

const successfulIPRegions = computed<ReliabilityTurnStateIPRegionSummary[]>(() => {
  const collector = turnStateCollector.value as (typeof turnStateCollector.value & Record<string, unknown>) | null
  const value: unknown = collector?.successful_ip_regions ?? collector?.ip_regions
  if (Array.isArray(value)) {
    return value.flatMap((entry) => {
      if (typeof entry === 'string') return [{ region: entry }]
      return entry && typeof entry === 'object' ? [entry as ReliabilityTurnStateIPRegionSummary] : []
    })
  }
  if (value && typeof value === 'object') {
    return Object.entries(value).map(([region, count]) => ({ region, count: numeric(count) }))
  }
  return []
})

const successfulIPs = computed<ReliabilityTurnStateSuccessfulIP[]>(() => {
  const collector = turnStateCollector.value as (typeof turnStateCollector.value & Record<string, unknown>) | null
  const value: unknown = collector?.successful_ips ?? collector?.successful_ip_addresses
  if (!Array.isArray(value)) return []
  return value.flatMap((entry) => {
    if (typeof entry === 'string') return [{ ip: entry }]
    return entry && typeof entry === 'object' ? [entry as ReliabilityTurnStateSuccessfulIP] : []
  })
})

const candidateBreakdown = computed<ReliabilityTurnStateCandidateBreakdown[]>(() => {
  const collector = turnStateCollector.value as (typeof turnStateCollector.value & Record<string, unknown>) | null
  const value: unknown = collector?.candidate_breakdown ?? collector?.candidate_reasons
  if (Array.isArray(value)) {
    return value.flatMap((entry) => {
      if (typeof entry === 'string') return [{ reason: entry, count: 1 }]
      return entry && typeof entry === 'object' ? [entry as ReliabilityTurnStateCandidateBreakdown] : []
    })
  }
  if (value && typeof value === 'object') {
    return Object.entries(value).map(([reason, count]) => ({ reason, count: numeric(count) }))
  }
  return []
})

const successfulIPTotal = computed(() => {
  // IP observations include exits whose region lookup did not succeed. Region
  // aggregates are only a fallback for gateways that omit the individual IPs.
  if (successfulIPs.value.length > 0) {
    return successfulIPs.value.reduce((total, entry) => total + numeric(entry.successes ?? entry.count, 1), 0)
  }
  return successfulIPRegions.value.reduce((total, entry) => total + numeric(entry.count ?? entry.successes), 0)
})
const successfulIPTotalLabel = computed(() => t('admin.reliability.turnState.successfulIPTotal', { count: formatCount(successfulIPTotal.value) }))
const candidateTotal = computed(() => candidateBreakdown.value.reduce((total, entry) => total + numeric(entry.count), 0))

function numeric(value: unknown, fallback = 0): number {
  if (value === undefined || value === null || (typeof value === 'string' && value.trim() === '')) return fallback
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function isSpeedPreset(value: unknown): value is ReliabilityTurnStateSpeedPreset {
  return defaultTurnStateSpeedPresets.includes(value as ReliabilityTurnStateSpeedPreset)
}

function normalizeSpeedPresets(value: unknown): {
  names: ReliabilityTurnStateSpeedPreset[]
  values: Partial<Record<ReliabilityTurnStateSpeedPreset, ReliabilityTurnStateSpeedPresetValues>>
} {
  if (Array.isArray(value)) {
    const names = value.filter(isSpeedPreset)
    return {
      names: names.length > 0 ? [...new Set(names)] : [...defaultTurnStateSpeedPresets],
      values: builtInSpeedPresetValues,
    }
  }
  if (value && typeof value === 'object') {
    const values: Partial<Record<ReliabilityTurnStateSpeedPreset, ReliabilityTurnStateSpeedPresetValues>> = {}
    for (const [name, preset] of Object.entries(value)) {
      if (isSpeedPreset(name) && preset && typeof preset === 'object') {
        values[name] = preset as ReliabilityTurnStateSpeedPresetValues
      }
    }
    const names = defaultTurnStateSpeedPresets.filter((name) => values[name] !== undefined)
    if (names.length > 0) return { names, values }
  }
  return { names: [...defaultTurnStateSpeedPresets], values: builtInSpeedPresetValues }
}

function normalizeNumberBounds(
  value: unknown,
  fallback: ReliabilityTurnStateNumberBounds,
): ReliabilityTurnStateNumberBounds {
  if (!value || typeof value !== 'object') return { ...fallback }
  const candidate = value as Partial<ReliabilityTurnStateNumberBounds>
  const min = Number(candidate.min)
  const max = Number(candidate.max)
  const step = Number(candidate.step)
  if (!Number.isFinite(min) || !Number.isFinite(max) || min > max) return { ...fallback }
  return {
    min,
    max,
    step: Number.isFinite(step) && step > 0 ? step : fallback.step,
  }
}

function boundedInteger(value: unknown, bounds: ReliabilityTurnStateNumberBounds, fallback: number): number {
  const parsed = Number(value)
  const candidate = Number.isSafeInteger(parsed) ? parsed : fallback
  return Math.min(bounds.max, Math.max(bounds.min, candidate))
}

function isIntegerInBounds(value: unknown, bounds: ReliabilityTurnStateNumberBounds): boolean {
  const parsed = Number(value)
  return value !== '' && Number.isSafeInteger(parsed) && parsed >= bounds.min && parsed <= bounds.max
}

type ProxyPoolParseResult = {
  validLines: string[]
  valid: number
  invalid: number
}

// Keep the editor strict enough to reject malformed entries before they reach
// the collector. Credentials remain opaque strings and are never echoed in
// the status projection below.
function parseProxyPoolText(value: string): ProxyPoolParseResult {
  const lines = value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  const validLines: string[] = []
  const seen = new Set<string>()
  let invalid = 0
  for (const line of lines) {
    const match = line.match(/^(https?|socks5):\/\/(?:([^:@\s]+):([^@\s]+)@)?(\[[^\]]+\]|[^:/?#\s]+):(\d{1,5})$/i)
    if (!match) {
      invalid++
      continue
    }
    const port = Number(match[5])
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      invalid++
      continue
    }
    const protocol = match[1].toLowerCase()
    const host = match[4].toLowerCase()
    const key = `${protocol}://${match[2] || ''}:${match[3] || ''}@${host}:${port}`
    if (seen.has(key)) continue
    seen.add(key)
    validLines.push(line)
  }
  return { validLines, valid: validLines.length, invalid }
}

function parseProxyPoolDisplay(value: string): ReliabilityTurnStateProxyPoolEntry | null {
  const match = value.trim().match(/^(https?|socks5):\/\/(?:[^@\s]+@)?(\[[^\]]+\]|[^:/?#\s]+):(\d{1,5})$/i)
  if (!match) return null
  const port = Number(match[3])
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null
  return { protocol: match[1].toLowerCase(), host: match[2], port }
}

function optionalBoolean(...values: unknown[]): boolean | null {
  const value = values.find((candidate) => typeof candidate === 'boolean')
  return typeof value === 'boolean' ? value : null
}

function regionLabel(value: unknown, country?: unknown, countryCode?: unknown): string {
  const parts = [value, country, countryCode]
    .map((part) => String(part ?? '').trim())
    .filter(Boolean)
  return [...new Set(parts)].join(' · ') || t('admin.reliability.turnState.unknownValue')
}

function candidateReasonLabel(value: unknown): string {
  const code = String(value ?? '').trim().toLowerCase()
  const supported = new Set([
    'account_identity_changed', 'account_unavailable', 'account_unschedulable', 'account_disabled', 'account_expired',
    'active_healthy_skipped', 'already_ready', 'capacity_full', 'cooldown', 'duplicate',
    'initial_missing', 'invalid_model', 'invalid_state', 'missing_response_state',
    'model_mismatch', 'model_not_supported', 'not_eligible', 'probe_disabled',
    'probe_failed', 'probe_in_progress', 'probe_timeout', 'proxy_failed', 'proxy_stream_quarantined',
    'proxy_timeout', 'proxy_unavailable', 'quota_auto_pause', 'refresh_due', 'response_model_mismatch',
    'request_budget_exhausted', 'routes_cooling_down',
    'runtime_blocked', 'scheduling_threshold', 'shadow_parent_unhealthy',
    'capability_mismatch', 'channel_upstream_restricted', 'group_mismatch',
    'privacy_not_set', 'same_account_retry_mismatch',
    'state_time_rejected', 'unreliable_key', 'transport_error', 'upstream_401', 'upstream_403', 'upstream_429',
    'upstream_5xx', 'model_capacity', 'upstream_rate_limited', 'response_failed',
    'incomplete_stream', 'cancelled', 'unavailable', 'unknown', 'other',
  ])
  if (!supported.has(code)) return t('admin.reliability.turnState.collectorUnknown')
  return t(`admin.reliability.turnState.candidateReasons.${code}`)
}

function speedPresetLabel(preset: ReliabilityTurnStateSpeedPreset): string {
  return t(`admin.reliability.turnState.speedPresets.${preset}`)
}

function selectSpeedPreset(preset: ReliabilityTurnStateSpeedPreset) {
  turnStateSpeedPreset.value = preset
  const values = turnStateSpeedPresetValues.value[preset]
  if (!values) return
  turnStateMaxRequestsPerRound.value = boundedInteger(
    values.max_requests_per_round,
    maxRequestsBounds.value,
    Number(turnStateMaxRequestsPerRound.value) || 6,
  )
  turnStateFailureCooldownSeconds.value = boundedInteger(
    values.failure_cooldown_seconds ?? values.cooldown_seconds,
    failureCooldownBounds.value,
    Number(turnStateFailureCooldownSeconds.value) || 180,
  )
}

function safeNodeText(value: unknown): string {
  const sanitized = String(value ?? '')
    .replace(/([a-z][a-z0-9+.-]*:\/\/)[^/\s@]+@/gi, '$1***@')
    .replace(/\b[^:\s/@]+:[^@\s/]+@(?=[^\s]+)/g, '***@')
    .split('')
    .map((character) => {
      const code = character.charCodeAt(0)
      return code < 32 || code === 127 ? ' ' : character
    })
    .join('')
    .trim()
    .slice(0, 160)
  return sanitized || t('admin.reliability.turnState.unknownValue')
}

function collectorNodeResultLabel(value: unknown): string {
  const aliases: Record<string, string> = {
    failed: 'failure',
    timeout: 'failure',
  }
  const raw = String(value ?? '').trim().toLowerCase()
  const code = aliases[raw] ?? raw
  const supported = new Set(['idle', 'collecting', 'success', 'failure', 'cooldown', 'skipped', 'unavailable'])
  if (supported.has(code)) return t(`admin.reliability.turnState.collectorNodeResults.${code}`)
  const errors = new Set([
    'probe_timeout', 'transport_error', 'upstream_401', 'upstream_403', 'upstream_429',
    'upstream_5xx', 'model_capacity', 'upstream_rate_limited', 'response_failed',
    'response_model_mismatch', 'invalid_state', 'invalid_model', 'incomplete_stream',
    'state_time_rejected', 'cancelled', 'disabled', 'other',
  ])
  if (errors.has(code)) return t(`admin.reliability.turnState.collectorErrors.${code}`)
  return t('admin.reliability.turnState.collectorUnknown')
}

const turnStateSupported = computed(() => optionalBoolean(turnState.value.supported))
const turnStateHTTP = computed(() => optionalBoolean(turnState.value.http_enabled, turnState.value.http_supported))
const turnStateWebSocket = computed(() => optionalBoolean(turnState.value.websocket_enabled, turnState.value.websocket_supported))
const turnStateHTTPCrossAccountProtection = computed(() => optionalBoolean(turnState.value.http_cross_account_protection))
const turnStateWebSocketCrossAccountProtection = computed(() => optionalBoolean(turnState.value.websocket_cross_account_protection))
const collectorReady = computed(() => optionalBoolean(turnStateCollector.value?.ready))
const collectorInjection = computed(() => optionalBoolean(turnStateCollector.value?.injection_enabled))
const collectorCollecting = computed(() => optionalBoolean(turnStateCollector.value?.collecting))
const collectorStatusLabel = computed(() => {
  const status = String(turnStateCollector.value?.status || '').trim().toLowerCase()
  if (!status) return t('admin.reliability.turnState.collectorUnknown')
  const supported = new Set(['disabled', 'unavailable', 'idle', 'collecting', 'warming', 'ready', 'stale', 'cooldown', 'degraded', 'error'])
  if (!supported.has(status)) return t('admin.reliability.turnState.collectorUnknown')
  return t(`admin.reliability.turnState.collectorStatus.${status}`)
})
const collectorStatusToneClass = computed(() => {
  const status = String(turnStateCollector.value?.status || '').trim().toLowerCase()
  if (status === 'ready') return 'bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-300'
  if (status === 'collecting' || status === 'warming') return 'bg-teal-50 text-teal-700 dark:bg-teal-950/40 dark:text-teal-300'
  if (status === 'cooldown' || status === 'degraded' || status === 'stale') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
  if (status === 'error' || status === 'unavailable') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
})
const collectorLastErrorLabel = computed(() => {
  const code = String(turnStateCollector.value?.last_error_code || '').trim().toLowerCase()
  const supported = new Set([
    'probe_timeout', 'transport_error', 'upstream_401', 'upstream_403', 'upstream_429', 'upstream_5xx',
    'model_capacity', 'upstream_rate_limited', 'response_failed', 'response_model_mismatch', 'invalid_state',
    'invalid_model', 'incomplete_stream', 'state_time_rejected', 'cooldown', 'cancelled', 'disabled', 'unavailable', 'other',
  ])
  if (!supported.has(code)) return t('admin.reliability.turnState.collectorUnknown')
  return t(`admin.reliability.turnState.collectorErrors.${code}`)
})

function formatCount(value: unknown): string {
  if (value === null || value === undefined || (typeof value === 'string' && value.trim() === '')) return t('admin.reliability.unavailable')
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? formatNumber(parsed) : t('admin.reliability.unavailable')
}

function formatTimestamp(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return t('admin.reliability.unavailable')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('admin.reliability.unavailable')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(date)
}

function formatDuration(value: unknown): string {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) return t('admin.reliability.unavailable')
  const seconds = Math.floor(parsed)
  if (seconds >= 86400) {
    return t('admin.reliability.turnState.durationDaysHours', {
      days: Math.floor(seconds / 86400),
      hours: Math.floor((seconds % 86400) / 3600),
    })
  }
  if (seconds >= 3600) {
    return t('admin.reliability.turnState.durationHoursMinutes', {
      hours: Math.floor(seconds / 3600),
      minutes: Math.floor((seconds % 3600) / 60),
    })
  }
  if (seconds >= 60) {
    return t('admin.reliability.turnState.durationMinutesSeconds', {
      minutes: Math.floor(seconds / 60),
      seconds: seconds % 60,
    })
  }
  return t('admin.reliability.turnState.durationSeconds', { seconds })
}

function supportLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.supportedValue') : t('admin.reliability.turnState.unsupportedValue')
}

function collectorReadyLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorReadyValue') : t('admin.reliability.turnState.collectorNotReadyValue')
}

function collectorInjectionLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorInjectionEnabled') : t('admin.reliability.turnState.collectorInjectionDisabled')
}

function collectorCollectingLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorCollectingValue') : t('admin.reliability.turnState.collectorIdleValue')
}

function settingStatusLabel(kind: 'probe' | 'injection', enabled: boolean): string {
  if (turnStateSettingsLoading.value || (!turnStateSettingsLoaded.value && !turnStateSettingsError.value)) {
    return t('admin.reliability.turnState.settingsLoading')
  }
  if (turnStateSettingSaving.value === kind) return t('admin.reliability.turnState.settingsSaving')
  return enabled
    ? t('admin.reliability.turnState.settingEnabled')
    : t('admin.reliability.turnState.settingDisabled')
}

function protectionLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.protectedValue') : t('admin.reliability.turnState.unprotectedValue')
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    status.value = await reliabilityAPI.getStatus()
  } catch {
    status.value = null
    loadError.value = t('admin.reliability.loadFailed')
  } finally {
    loading.value = false
  }
}

function enabledSetting(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function applyTurnStateSettings(settings: ReliabilityTurnStateSettings, includeMetadata = false) {
  if (includeMetadata) {
    const presets = normalizeSpeedPresets(settings?.presets)
    turnStateSpeedPresets.value = presets.names
    turnStateSpeedPresetValues.value = presets.values
    maxRequestsBounds.value = normalizeNumberBounds(
      settings?.bounds?.max_requests_per_round,
      { min: 1, max: 100, step: 1 },
    )
    failureCooldownBounds.value = normalizeNumberBounds(
      settings?.bounds?.failure_cooldown_seconds,
      { min: 1, max: 3600, step: 1 },
    )
  }

  turnStateProbeEnabled.value = enabledSetting(settings?.probe_enabled, turnStateProbeEnabled.value)
  turnStateCacheInjectionEnabled.value = enabledSetting(
    settings?.injection_enabled,
    turnStateCacheInjectionEnabled.value,
  )
  const requestedPreset = isSpeedPreset(settings?.speed_preset ?? settings?.harvest?.speed_preset)
    ? (settings.speed_preset ?? settings.harvest?.speed_preset) as ReliabilityTurnStateSpeedPreset
    : turnStateSpeedPreset.value
  turnStateSpeedPreset.value = turnStateSpeedPresets.value.includes(requestedPreset)
    ? requestedPreset
    : (turnStateSpeedPresets.value[0] ?? 'standard')
  turnStateMaxRequestsPerRound.value = boundedInteger(
    settings?.max_requests_per_round ?? settings?.harvest?.max_requests_per_round,
    maxRequestsBounds.value,
    Number(turnStateMaxRequestsPerRound.value) || 6,
  )
  turnStateFailureCooldownSeconds.value = boundedInteger(
    settings?.failure_cooldown_seconds ?? settings?.harvest?.failure_cooldown_seconds,
    failureCooldownBounds.value,
    Number(turnStateFailureCooldownSeconds.value) || 180,
  )
}

function currentTurnStateSettingsPayload(): ReliabilityTurnStateSettingsUpdate {
  return {
    ...currentTurnStateSwitchesPayload(),
    speed_preset: turnStateSpeedPreset.value,
    max_requests_per_round: Number(turnStateMaxRequestsPerRound.value),
    failure_cooldown_seconds: Number(turnStateFailureCooldownSeconds.value),
  }
}

function currentTurnStateSwitchesPayload(): ReliabilityTurnStateSettingsUpdate {
  return {
    probe_enabled: turnStateProbeEnabled.value,
    injection_enabled: turnStateCacheInjectionEnabled.value,
  }
}

async function loadTurnStateSettings() {
  if (turnStateSettingsLoading.value || turnStateSettingSaving.value !== null) return
  turnStateSettingsLoading.value = true
  turnStateSettingsError.value = ''
  try {
    const settings = await reliabilityAPI.getTurnStateSettings()
    applyTurnStateSettings({
      ...settings,
      probe_enabled: enabledSetting(settings?.probe_enabled, true),
      injection_enabled: enabledSetting(settings?.injection_enabled, true),
    }, true)
    turnStateSettingsHasProxyPool.value = settings?.proxy_pool_configured === true
      || (typeof settings?.proxy_pool_count === 'number' && settings.proxy_pool_count > 0)
    // Proxy URLs are write-only. Never hydrate the textarea from a response,
    // including responses from an older gateway that may still contain the
    // legacy proxy_pool_urls field with credentials. Preserve the local draft
    // when status/settings are refreshed.
    turnStateSettingsLoaded.value = true
  } catch (error) {
    turnStateSettingsLoaded.value = false
    turnStateSettingsError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsLoadFailed'),
    )
  } finally {
    turnStateSettingsLoading.value = false
  }
}

async function saveTurnStateSetting(kind: 'probe' | 'injection', enabled: boolean) {
  if (!turnStateSettingsLoaded.value || turnStateSettingsBusy.value) return

  const previousProbe = turnStateProbeEnabled.value
  const previousInjection = turnStateCacheInjectionEnabled.value
  if (kind === 'probe') turnStateProbeEnabled.value = enabled
  else turnStateCacheInjectionEnabled.value = enabled

  turnStateSettingSaving.value = kind
  turnStateSettingsError.value = ''
  try {
    const updated = await reliabilityAPI.updateTurnStateSettings(currentTurnStateSwitchesPayload())
    turnStateProbeEnabled.value = enabledSetting(updated?.probe_enabled, turnStateProbeEnabled.value)
    turnStateCacheInjectionEnabled.value = enabledSetting(updated?.injection_enabled, turnStateCacheInjectionEnabled.value)
    if (typeof updated?.proxy_pool_configured === 'boolean') {
      turnStateSettingsHasProxyPool.value = updated.proxy_pool_configured
    }
    appStore.showSuccess(t('admin.reliability.turnState.settingsSaved'))
  } catch (error) {
    turnStateProbeEnabled.value = previousProbe
    turnStateCacheInjectionEnabled.value = previousInjection
    turnStateSettingsError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsSaveFailed'),
    )
    appStore.showError(turnStateSettingsError.value)
  } finally {
    turnStateSettingSaving.value = null
  }
}

async function saveTurnStatePolicy() {
  if (!turnStateSettingsLoaded.value || turnStateSettingsBusy.value || !turnStatePolicyValid.value) return
  turnStateSettingSaving.value = 'policy'
  turnStateSettingsError.value = ''
  try {
    const updated = await reliabilityAPI.updateTurnStateSettings(currentTurnStateSettingsPayload())
    applyTurnStateSettings(updated)
    appStore.showSuccess(t('admin.reliability.turnState.settingsSaved'))
  } catch (error) {
    turnStateSettingsError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsSaveFailed'),
    )
    appStore.showError(turnStateSettingsError.value)
  } finally {
    turnStateSettingSaving.value = null
  }
}

async function saveTurnStateProxyPool() {
  if (!turnStateSettingsLoaded.value || turnStateSettingsBusy.value) return
  if (proxyPoolParse.value.invalid > 0) {
    turnStateProxyPoolError.value = t('admin.reliability.turnState.proxyPoolValidationFailed')
    return
  }

  const previousText = turnStateProxyPoolText.value
  const proxyPoolURLs = proxyPoolParse.value.validLines
  turnStateSettingSaving.value = 'proxy_pool'
  turnStateProxyPoolError.value = ''
  turnStateSettingsError.value = ''
  try {
    const updated = await reliabilityAPI.updateTurnStateSettings({
      ...currentTurnStateSwitchesPayload(),
      proxy_pool_urls: proxyPoolURLs,
    })
    // Remove saved credentials from both the form and its reactive state.
    turnStateProxyPoolText.value = ''
    if (typeof updated?.proxy_pool_configured === 'boolean') {
      turnStateSettingsHasProxyPool.value = updated.proxy_pool_configured
    } else {
      turnStateSettingsHasProxyPool.value = proxyPoolURLs.length > 0
    }
    appStore.showSuccess(t('admin.reliability.turnState.settingsSaved'))
  } catch (error) {
    turnStateProxyPoolText.value = previousText
    turnStateProxyPoolError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsSaveFailed'),
    )
    appStore.showError(turnStateProxyPoolError.value)
  } finally {
    turnStateSettingSaving.value = null
  }
}

async function loadPageData() {
  await Promise.all([loadData(), loadTurnStateSettings()])
}

onMounted(loadPageData)
</script>
