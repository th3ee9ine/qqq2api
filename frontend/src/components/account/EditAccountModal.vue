<template>
  <BaseDialog
    :show="show && supported"
    :title="t('admin.accounts.editAccount')"
    width="wide"
    @close="handleClose"
  >
    <form
      v-if="account"
      id="edit-account-form"
      @submit.prevent="handleSubmit"
      class="space-y-5"
    >
      <div>
        <label class="input-label">{{ t('common.name') }}</label>
        <input v-model="name" type="text" required class="input" data-tour="edit-account-form-name" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.notes') }}</label>
        <textarea
          v-model="notes"
          rows="3"
          class="input"
          :placeholder="t('admin.accounts.notesPlaceholder')"
        ></textarea>
        <p class="input-hint">{{ t('admin.accounts.notesHint') }}</p>
      </div>


      <!-- API Key fields (only for apikey type) -->
      <div v-if="account.type === 'apikey'" class="space-y-4" data-testid="edit-api-key-section">
        <div>
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input v-model="baseUrl" type="text" class="input" :placeholder="baseUrlPlaceholder" />
          <p class="input-hint">{{ baseUrlHint }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKey') }}</label>
          <input
            v-model="newApiKey"
            type="password"
            class="input font-mono"
            autocomplete="new-password"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            :placeholder="account.platform === 'openai' ? 'sk-proj-...' : 'sk-ant-...'"
          />
          <p class="input-hint">{{ t('admin.accounts.leaveEmptyToKeep') }}</p>
        </div>
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

          <div
            v-if="isOpenAIModelRestrictionDisabled"
            class="mb-3 rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
          >
            <p class="text-xs text-amber-700 dark:text-amber-400">
              {{ t('admin.accounts.openai.modelRestrictionDisabledByPassthrough') }}
            </p>
          </div>

          <template v-else>
            <!-- Mode Toggle -->
            <div class="mb-4 flex gap-2">
              <button
                type="button"
                @click="modelRestrictionMode = 'whitelist'"
                :class="[
                  'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                  modelRestrictionMode === 'whitelist'
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                ]"
              >
                <svg
                  class="mr-1.5 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
                {{ t('admin.accounts.modelWhitelist') }}
              </button>
              <button
                type="button"
                data-testid="edit-model-restriction-mapping"
                @click="modelRestrictionMode = 'mapping'"
                :class="[
                  'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                  modelRestrictionMode === 'mapping'
                    ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                ]"
              >
                <svg
                  class="mr-1.5 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
                  />
                </svg>
                {{ t('admin.accounts.modelMapping') }}
              </button>
            </div>

            <!-- Whitelist Mode -->
            <div v-if="modelRestrictionMode === 'whitelist'">
              <ModelWhitelistSelector v-model="allowedModels" :platform="account?.platform || 'anthropic'" :account-id="account?.id" />
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
                <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{
                  t('admin.accounts.supportsAllModels')
                }}</span>
              </p>
            </div>

            <!-- Mapping Mode -->
            <div v-else>
              <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
                <p class="text-xs text-purple-700 dark:text-purple-400">
                  <svg
                    class="mr-1 inline h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  {{ t('admin.accounts.mapRequestModels') }}
                </p>
              </div>

            <!-- Model Mapping List -->
            <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
              <div
                v-for="(mapping, index) in modelMappings"
                :key="getModelMappingKey(mapping)"
                class="flex items-center gap-2"
              >
                <input
                  v-model="mapping.from"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.requestModel')"
                />
                <svg
                  class="h-4 w-4 flex-shrink-0 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M14 5l7 7m0 0l-7 7m7-7H3"
                  />
                </svg>
                <input
                  v-model="mapping.to"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.actualModel')"
                />
                <button
                  type="button"
                  @click="removeModelMapping(index)"
                  class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <button
              type="button"
              @click="addModelMapping"
              class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            >
              <svg
                class="mr-1 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 4v16m8-8H4"
                />
              </svg>
              {{ t('admin.accounts.addMapping') }}
            </button>

              <!-- Quick Add Buttons -->
              <div class="flex flex-wrap gap-2">
                <button
                  v-for="preset in presetMappings"
                  :key="preset.label"
                  :data-testid="`edit-model-preset-${preset.from}`"
                  type="button"
                  @click="addPresetMapping(preset.from, preset.to)"
                  :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
                >
                  + {{ preset.label }}
                </button>
              </div>
            </div>
          </template>
        </div>

        <!-- Pool Mode Section -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.poolMode') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.poolModeHint') }}
              </p>
            </div>
            <button
              type="button"
              @click="poolModeEnabled = !poolModeEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                poolModeEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  poolModeEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="poolModeEnabled" class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.poolModeInfo') }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryCount') }}</label>
            <input
              v-model.number="poolModeRetryCount"
              type="number"
              min="0"
              :max="MAX_POOL_MODE_RETRY_COUNT"
              step="1"
              class="input"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{
                t('admin.accounts.poolModeRetryCountHint', {
                  default: DEFAULT_POOL_MODE_RETRY_COUNT,
                  max: MAX_POOL_MODE_RETRY_COUNT
                })
              }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryStatusCodes') }}</label>
            <input
              v-model="poolModeRetryStatusCodesInput"
              type="text"
              class="input"
              :placeholder="DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.poolModeRetryStatusCodesHint', { default: DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ') }) }}
            </p>
          </div>
        </div>

        <!-- Custom Error Codes Section -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.customErrorCodes') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.customErrorCodesHint') }}
              </p>
            </div>
            <button
              type="button"
              @click="customErrorCodesEnabled = !customErrorCodesEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                customErrorCodesEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  customErrorCodesEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="customErrorCodesEnabled" class="space-y-3">
            <div class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20">
              <p class="text-xs text-amber-700 dark:text-amber-400">
                <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
                {{ t('admin.accounts.customErrorCodesWarning') }}
              </p>
            </div>

            <!-- Error Code Buttons -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="code in commonErrorCodes"
                :key="code.value"
                type="button"
                @click="toggleErrorCode(code.value)"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                  selectedErrorCodes.includes(code.value)
                    ? 'bg-red-100 text-red-700 ring-1 ring-red-500 dark:bg-red-900/30 dark:text-red-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                ]"
              >
                {{ code.value }} {{ code.label }}
              </button>
            </div>

            <!-- Manual input -->
            <div class="flex items-center gap-2">
              <input
                v-model.number="customErrorCodeInput"
                type="number"
                min="100"
                max="599"
                class="input flex-1"
                :placeholder="t('admin.accounts.enterErrorCode')"
                @keyup.enter="addCustomErrorCode"
              />
              <button type="button" @click="addCustomErrorCode" class="btn btn-secondary px-3">
                <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M12 4v16m8-8H4"
                  />
                </svg>
              </button>
            </div>

            <!-- Selected codes summary -->
            <div class="flex flex-wrap gap-1.5">
              <span
                v-for="code in selectedErrorCodes.sort((a, b) => a - b)"
                :key="code"
                class="inline-flex items-center gap-1 rounded-full bg-red-100 px-2.5 py-0.5 text-sm font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
              >
                {{ code }}
                <button
                  type="button"
                  @click="removeErrorCode(code)"
                  class="hover:text-red-900 dark:hover:text-red-300"
                >
                  <Icon name="x" size="sm" :stroke-width="2" />
                </button>
              </span>
              <span v-if="selectedErrorCodes.length === 0" class="text-xs text-gray-400">
                {{ t('admin.accounts.noneSelectedUsesDefault') }}
              </span>
            </div>
          </div>
        </div>
      </div>
      <!-- Header Override Section -->
      <div v-if="headerOverrideCapable" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.headerOverride.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.headerOverride.hint') }}
            </p>
          </div>
          <button
            type="button"
            @click="headerOverrideEnabled = !headerOverrideEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              headerOverrideEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                headerOverrideEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>

        <div v-if="headerOverrideEnabled" class="space-y-3">
          <div class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.headerOverride.info') }}
            </p>
          </div>

          <HeaderOverrideEditor
            :rows="headerOverrideRows"
            @update:rows="headerOverrideRows = $event"
          />
        </div>
      </div>
      <!-- OpenAI OAuth Model Mapping -->
      <div
        v-if="account.platform === 'openai' && account.type === 'oauth'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

        <div
          v-if="isOpenAIModelRestrictionDisabled"
          class="mb-3 rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
        >
          <p class="text-xs text-amber-700 dark:text-amber-400">
            {{ t('admin.accounts.openai.modelRestrictionDisabledByPassthrough') }}
          </p>
        </div>

        <template v-else>
          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              data-testid="edit-model-restriction-mapping"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector v-model="allowedModels" :platform="account?.platform || 'anthropic'" :account-id="account?.id" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{
                t('admin.accounts.supportsAllModels')
              }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else>
            <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
              <p class="text-xs text-purple-700 dark:text-purple-400">
                {{ t('admin.accounts.mapRequestModels') }}
              </p>
            </div>

            <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
              <div
                v-for="(mapping, index) in modelMappings"
                :key="'oauth-' + getModelMappingKey(mapping)"
                class="flex items-center gap-2"
              >
                <input
                  v-model="mapping.from"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.requestModel')"
                />
                <svg
                  class="h-4 w-4 flex-shrink-0 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M14 5l7 7m0 0l-7 7m7-7H3"
                  />
                </svg>
                <input
                  v-model="mapping.to"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.actualModel')"
                />
                <button
                  type="button"
                  @click="removeModelMapping(index)"
                  class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <button
              type="button"
              @click="addModelMapping"
              class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            >
              + {{ t('admin.accounts.addMapping') }}
            </button>

            <!-- Quick Add Buttons -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in presetMappings"
                :key="'oauth-' + preset.label"
                :data-testid="`edit-model-preset-${preset.from}`"
                type="button"
                @click="addPresetMapping(preset.from, preset.to)"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </template>
      </div>
      <!-- Vertex Service Account -->
      <div v-if="account.platform === 'anthropic' && account.type === 'service_account'" class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">Project ID</label>
            <input
              v-model="vertexProjectId"
              data-testid="edit-vertex-project-id"
              type="text"
              class="input font-mono"
              readonly
              :placeholder="t('admin.accounts.vertexProjectIdPlaceholder')"
            />
            <p class="input-hint">{{ t('admin.accounts.vertexSaJsonEditHint') }}</p>
          </div>
          <div>
            <label class="input-label">Location</label>
            <select
              v-model="vertexLocation"
              data-testid="edit-vertex-location"
              required
              class="input font-mono"
            >
              <optgroup
                v-for="group in VERTEX_LOCATION_OPTIONS"
                :key="group.label"
                :label="group.label"
              >
                <option
                  v-for="option in group.options"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </option>
              </optgroup>
            </select>
            <p class="input-hint">{{ t('admin.accounts.vertexLocationHint') }}</p>
          </div>
        </div>

        <!-- Model Restriction Section for Service Account -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              <svg
                class="mr-1.5 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              data-testid="edit-model-restriction-mapping"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              <svg
                class="mr-1.5 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
                />
              </svg>
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector v-model="allowedModels" :platform="account?.platform || 'anthropic'" :account-id="account?.id" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{
                t('admin.accounts.supportsAllModels')
              }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else>
            <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
              <p class="text-xs text-purple-700 dark:text-purple-400">
                <svg
                  class="mr-1 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
                {{ t('admin.accounts.mapRequestModels') }}
              </p>
            </div>

            <!-- Model Mapping List -->
            <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
              <div
                v-for="(mapping, index) in modelMappings"
                :key="getModelMappingKey(mapping)"
                class="flex items-center gap-2"
              >
                <input
                  v-model="mapping.from"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.requestModel')"
                />
                <svg
                  class="h-4 w-4 flex-shrink-0 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M14 5l7 7m0 0l-7 7m7-7H3"
                  />
                </svg>
                <input
                  v-model="mapping.to"
                  type="text"
                  class="input flex-1"
                  :placeholder="t('admin.accounts.actualModel')"
                />
                <button
                  type="button"
                  @click="removeModelMapping(index)"
                  class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <button
              type="button"
              @click="addModelMapping"
              class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
            >
              <svg
                class="mr-1 inline h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 4v16m8-8H4"
                />
              </svg>
              {{ t('admin.accounts.addMapping') }}
            </button>

            <!-- Quick Add Buttons -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in presetMappings"
                :key="preset.label"
                type="button"
                @click="addPresetMapping(preset.from, preset.to)"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </div>
      </div>
      <!-- Bedrock fields (for bedrock type, both SigV4 and API Key modes) -->
      <div v-if="account.type === 'bedrock'" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockAuthMode') }}</label>
          <div class="mt-2 flex gap-4">
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="sigv4"
                class="mr-2 text-primary-600 focus:ring-primary-500"
                data-testid="edit-bedrock-auth-sigv4"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeSigv4') }}</span>
            </label>
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="apikey"
                class="mr-2 text-primary-600 focus:ring-primary-500"
                data-testid="edit-bedrock-auth-apikey"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeApikey') }}</span>
            </label>
          </div>
        </div>

        <!-- SigV4 fields -->
        <template v-if="bedrockAuthMode !== 'apikey'">
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockAccessKeyId') }}</label>
            <input
              v-model="bedrockAccessKeyId"
              data-testid="edit-bedrock-access-key-id"
              type="text"
              class="input font-mono"
              placeholder="AKIA..."
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSecretAccessKey') }}</label>
            <input
              v-model="bedrockSecretAccessKey"
              data-testid="edit-bedrock-secret-access-key"
              type="password"
              class="input font-mono"
              :placeholder="t('admin.accounts.bedrockSecretKeyLeaveEmpty')"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSecretKeyLeaveEmpty') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSessionToken') }}</label>
            <input
              v-model="bedrockSessionToken"
              data-testid="edit-bedrock-session-token"
              type="password"
              class="input font-mono"
              :placeholder="t('admin.accounts.bedrockSecretKeyLeaveEmpty')"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSessionTokenHint') }}</p>
          </div>
        </template>

        <!-- API Key field -->
        <div v-if="bedrockAuthMode === 'apikey'">
          <label class="input-label">{{ t('admin.accounts.bedrockApiKeyInput') }}</label>
          <input
            v-model="bedrockApiKey"
            data-testid="edit-bedrock-api-key"
            type="password"
            class="input font-mono"
            :placeholder="t('admin.accounts.bedrockApiKeyLeaveEmpty')"
          />
          <p class="input-hint">{{ t('admin.accounts.bedrockApiKeyLeaveEmpty') }}</p>
        </div>

        <!-- Shared: Region -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockRegion') }}</label>
          <input
            v-model="bedrockRegion"
            data-testid="edit-bedrock-region"
            type="text"
            class="input"
            placeholder="us-east-1"
          />
          <p class="input-hint">{{ t('admin.accounts.bedrockRegionHint') }}</p>
        </div>

        <!-- Shared: Force Global -->
        <div>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              v-model="bedrockForceGlobal"
              data-testid="edit-bedrock-force-global"
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockForceGlobal') }}</span>
          </label>
          <p class="input-hint mt-1">{{ t('admin.accounts.bedrockForceGlobalHint') }}</p>
        </div>

        <!-- Model Restriction for Bedrock -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <label class="input-label">{{ t('admin.accounts.modelRestriction') }}</label>

          <!-- Mode Toggle -->
          <div class="mb-4 flex gap-2">
            <button
              type="button"
              @click="modelRestrictionMode = 'whitelist'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'whitelist'
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelWhitelist') }}
            </button>
            <button
              type="button"
              data-testid="edit-model-restriction-mapping"
              @click="modelRestrictionMode = 'mapping'"
              :class="[
                'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                modelRestrictionMode === 'mapping'
                  ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
            >
              {{ t('admin.accounts.modelMapping') }}
            </button>
          </div>

          <!-- Whitelist Mode -->
          <div v-if="modelRestrictionMode === 'whitelist'">
            <ModelWhitelistSelector v-model="allowedModels" platform="anthropic" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0 && modelMappings.length === 0">{{ t('admin.accounts.supportsAllModels') }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else class="space-y-3">
            <div v-for="(mapping, index) in modelMappings" :key="getModelMappingKey(mapping)" class="flex items-center gap-2">
              <input v-model="mapping.from" type="text" class="input flex-1" :placeholder="t('admin.accounts.fromModel')" />
              <span class="text-gray-400">→</span>
              <input v-model="mapping.to" type="text" class="input flex-1" :placeholder="t('admin.accounts.toModel')" />
              <button type="button" @click="modelMappings.splice(index, 1)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <button type="button" @click="modelMappings.push({ from: '', to: '' })" class="btn btn-secondary text-sm">
              + {{ t('admin.accounts.addMapping') }}
            </button>
            <!-- Bedrock Preset Mappings -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in presetMappings"
                :key="preset.from"
                :data-testid="`edit-model-preset-${preset.from}`"
                type="button"
                @click="modelMappings.push({ from: preset.from, to: preset.to })"
                :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
              >
                + {{ preset.label }}
              </button>
            </div>
          </div>
        </div>

        <!-- Pool Mode Section for Bedrock -->
        <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.poolMode') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.poolModeHint') }}
              </p>
            </div>
            <button
              type="button"
              @click="poolModeEnabled = !poolModeEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                poolModeEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  poolModeEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="poolModeEnabled" class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.poolModeInfo') }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryCount') }}</label>
            <input
              v-model.number="poolModeRetryCount"
              type="number"
              min="0"
              :max="MAX_POOL_MODE_RETRY_COUNT"
              step="1"
              class="input"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{
                t('admin.accounts.poolModeRetryCountHint', {
                  default: DEFAULT_POOL_MODE_RETRY_COUNT,
                  max: MAX_POOL_MODE_RETRY_COUNT
                })
              }}
            </p>
          </div>
          <div v-if="poolModeEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.poolModeRetryStatusCodes') }}</label>
            <input
              v-model="poolModeRetryStatusCodesInput"
              type="text"
              class="input"
              :placeholder="DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.poolModeRetryStatusCodesHint', { default: DEFAULT_POOL_MODE_RETRY_STATUS_CODES.join(', ') }) }}
            </p>
          </div>
        </div>
      </div>
      <!-- Temp Unschedulable Rules -->
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.tempUnschedulable.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.tempUnschedulable.hint') }}
            </p>
          </div>
          <button
            type="button"
            @click="tempUnschedEnabled = !tempUnschedEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              tempUnschedEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                tempUnschedEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>

        <div v-if="tempUnschedEnabled" class="space-y-3">
          <div class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.tempUnschedulable.notice') }}
            </p>
          </div>

          <div class="flex flex-wrap gap-2">
            <button
              v-for="preset in tempUnschedPresets"
              :key="preset.label"
              type="button"
              @click="addTempUnschedRule(preset.rule)"
              class="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
            >
              + {{ preset.label }}
            </button>
          </div>

          <div v-if="tempUnschedRules.length > 0" class="space-y-3">
            <div
              v-for="(rule, index) in tempUnschedRules"
              :key="getTempUnschedRuleKey(rule)"
              class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="mb-2 flex items-center justify-between">
                <span class="text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.accounts.tempUnschedulable.ruleIndex', { index: index + 1 }) }}
                </span>
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    :disabled="index === 0"
                    @click="moveTempUnschedRule(index, -1)"
                    class="rounded p-1 text-gray-400 transition-colors hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:text-gray-200"
                  >
                    <Icon name="chevronUp" size="sm" :stroke-width="2" />
                  </button>
                  <button
                    type="button"
                    :disabled="index === tempUnschedRules.length - 1"
                    @click="moveTempUnschedRule(index, 1)"
                    class="rounded p-1 text-gray-400 transition-colors hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:text-gray-200"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    @click="removeTempUnschedRule(index)"
                    class="rounded p-1 text-red-500 transition-colors hover:text-red-600"
                  >
                    <Icon name="x" size="sm" :stroke-width="2" />
                  </button>
                </div>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.errorCode') }}</label>
                  <input
                    v-model.number="rule.error_code"
                    type="number"
                    min="100"
                    max="599"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.errorCodePlaceholder')"
                  />
                </div>
                <div>
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.durationMinutes') }}</label>
                  <input
                    v-model.number="rule.duration_minutes"
                    type="number"
                    min="1"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.durationPlaceholder')"
                  />
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.keywords') }}</label>
                  <input
                    v-model="rule.keywords"
                    type="text"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.keywordsPlaceholder')"
                  />
                  <p class="input-hint">{{ t('admin.accounts.tempUnschedulable.keywordsHint') }}</p>
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label">{{ t('admin.accounts.tempUnschedulable.description') }}</label>
                  <input
                    v-model="rule.description"
                    type="text"
                    class="input"
                    :placeholder="t('admin.accounts.tempUnschedulable.descriptionPlaceholder')"
                  />
                </div>
              </div>
            </div>
          </div>

          <button
            type="button"
            @click="addTempUnschedRule()"
            class="w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-sm text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
          >
            <svg
              class="mr-1 inline h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
            {{ t('admin.accounts.tempUnschedulable.addRule') }}
          </button>
        </div>
      </div>
      <div
        v-if="supportsAccountSchedulingThresholdOverride"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="account-scheduling-threshold-section"
      >
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.accountSchedulingThresholdOverride') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.accountSchedulingThresholdOverrideHint') }}
            </p>
          </div>
          <input
            v-model="accountSchedulingThresholdOverrideEnabled"
            data-testid="account-scheduling-threshold-override-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="accountSchedulingThresholdOverrideEnabled">
          <label class="input-label">{{ t('admin.accounts.accountSchedulingThresholdOverrideValue') }}</label>
          <input
            v-model.number="accountSchedulingThresholdOverrideValue"
            data-testid="account-scheduling-threshold-override-value"
            type="number"
            min="1"
            max="100"
            class="input"
          />
          <p class="input-hint">{{ t('admin.accounts.accountSchedulingThresholdOverrideDisabledHint') }}</p>
        </div>
      </div>
      <!-- Intercept Warmup Requests (Anthropic) -->
      <div
        v-if="account?.platform === 'anthropic'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{
              t('admin.accounts.interceptWarmupRequests')
            }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.interceptWarmupRequestsDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="interceptWarmupRequests = !interceptWarmupRequests"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              interceptWarmupRequests ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                interceptWarmupRequests ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>
      <div v-if="!isSparkShadow">
        <div class="mb-1 flex items-center gap-2">
          <label class="input-label mb-0">{{ t('admin.accounts.proxy') }}</label>
          <ProxyAdBanner />
        </div>
        <ProxySelector v-model="proxyId" :proxies="proxies" />
      </div>
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.concurrency') }}</label>
          <input v-model.number="concurrency" type="number" min="1" class="input"
            @input="concurrency = Math.max(1, concurrency || 1)" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.loadFactor') }}</label>
          <input v-model.number="loadFactor" type="number" min="1"
            class="input" :placeholder="String(concurrency || 1)"
            @input="loadFactor = (loadFactor &amp;&amp; loadFactor >= 1) ? loadFactor : null" />
          <p class="input-hint">{{ t('admin.accounts.loadFactorHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.priority') }}</label>
          <input
            v-model.number="priority"
            type="number"
            min="1"
            class="input"
            data-tour="account-form-priority"
          />
          <p class="input-hint">{{ t('admin.accounts.priorityHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
          <input
            v-model.number="rateMultiplier"
            type="number"
            min="0"
            step="0.001"
            class="input disabled:cursor-not-allowed disabled:opacity-60"
            data-testid="account-rate-multiplier"
            :disabled="upstreamBillingRateSyncEnabled"
          />
          <p class="input-hint">
            {{
              t(
                upstreamBillingRateSyncEnabled
                  ? 'admin.accounts.upstreamBilling.syncRateManagedHint'
                  : 'admin.accounts.billingRateMultiplierHint'
              )
            }}
          </p>
          <div
            v-if="account?.type === 'apikey'"
            class="mt-3 flex items-center justify-between gap-3"
          >
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.accounts.upstreamBilling.syncRate') }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.upstreamBilling.syncRateHint') }}
              </p>
            </div>
            <Toggle
              :model-value="upstreamBillingRateSyncEnabled"
              data-testid="upstream-billing-rate-sync"
              :aria-label="t('admin.accounts.upstreamBilling.syncRate')"
              @update:model-value="handleUpstreamBillingRateSyncChange"
            />
          </div>
        </div>
      </div>
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <label class="input-label">{{ t('admin.accounts.expiresAt') }}</label>
        <input v-model="expiresAtInput" type="datetime-local" class="input" />
        <p class="input-hint">{{ t('admin.accounts.expiresAtHint') }}</p>
      </div>
      <!-- OpenAI 自动透传开关（OAuth/API Key） -->
      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.oauthPassthrough') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="openaiPassthroughEnabled = !openaiPassthroughEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openaiPassthroughEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openaiPassthroughEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- OpenAI Codex namespace 工具摊平（兼容开关，仅 OAuth） -->
      <div
        v-if="account?.platform === 'openai' && account?.type === 'oauth'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.flattenNamespaces') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.flattenNamespacesDesc') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="edit-openai-flatten-namespaces-toggle"
            @click="openAIFlattenNamespaces = !openAIFlattenNamespaces"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAIFlattenNamespaces ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAIFlattenNamespaces ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- OpenAI Codex hosted image_generation bridge policy -->
      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="overflow-hidden rounded-lg border border-sky-100 bg-sky-50/60 shadow-sm dark:border-sky-900/50 dark:bg-sky-950/20">
          <div class="flex items-start gap-3 px-4 py-3">
            <div class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-white text-sky-600 shadow-sm ring-1 ring-sky-100 dark:bg-dark-800 dark:text-sky-300 dark:ring-sky-900/60">
              <Icon name="sparkles" size="sm" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <label class="input-label mb-0">{{ t('admin.accounts.openai.codexImageTool') }}</label>
                <span
                  class="rounded-full px-2 py-0.5 text-[11px] font-medium"
                  :class="codexImageToolBadgeClass"
                >
                  {{ codexImageToolBadgeLabel }}
                </span>
              </div>
              <p class="mt-1 text-xs leading-5 text-slate-600 dark:text-slate-300">
                {{ t('admin.accounts.openai.codexImageToolDesc') }}
              </p>
            </div>
          </div>
          <div class="border-t border-sky-100 bg-white/70 p-2 dark:border-sky-900/50 dark:bg-dark-800/70">
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <button
                v-for="option in codexImageToolOptions"
                :key="option.value"
                type="button"
                :data-testid="`codex-image-tool-${option.value}`"
                @click="codexImageToolMode = option.value"
                :class="[
                  'group flex min-h-[62px] items-start gap-2 rounded-md border px-3 py-2 text-left transition-all',
                  codexImageToolMode === option.value
                    ? option.selectedCardClass
                    : 'border-transparent bg-transparent text-slate-600 hover:border-gray-200 hover:bg-gray-50 dark:text-slate-300 dark:hover:border-dark-500 dark:hover:bg-dark-700'
                ]"
              >
                <span
                  :class="[
                    'mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border transition-colors',
                    codexImageToolMode === option.value
                      ? option.selectedDotClass
                      : 'border-gray-300 text-transparent group-hover:border-gray-400 dark:border-dark-500'
                  ]"
                >
                  <Icon name="check" size="xs" :stroke-width="2" />
                </span>
                <span class="min-w-0">
                  <span class="block text-sm font-medium">{{ option.label }}</span>
                  <span class="mt-0.5 block text-xs leading-4 text-slate-500 dark:text-slate-400">{{ option.description }}</span>
                </span>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.wsMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.wsModeDesc') }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t(openAIWSModeConcurrencyHintKey) }}
            </p>
          </div>
          <div class="w-52">
            <Select v-model="openAIWSMode" data-testid="edit-openai-ws-mode-select" :options="openAIWSModeOptions" />
          </div>
        </div>
      </div>

      <!-- OpenAI APIKey Responses API support mode -->
      <div
        v-if="account?.platform === 'openai' && account?.type === 'apikey'"
        class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.responsesMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.responsesModeDesc') }}
            </p>
          </div>
          <div class="w-56">
            <Select
              v-model="openAIResponsesMode"
              :options="openAIResponsesModeOptions"
              :disabled="!openAITextGenerationEnabled"
              data-testid="openai-responses-mode-select"
            />
          </div>
        </div>
        <div
          v-if="openAITextGenerationEnabled"
          class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300"
        >
          <span class="font-medium">{{ t(openAIResponsesStatusKey) }}</span>
        </div>
        <div
          v-else
          class="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          data-testid="openai-responses-mode-not-applicable"
        >
          {{ t('admin.accounts.openai.responsesModeTextDisabledHint') }}
        </div>
        <div>
          <label class="input-label mb-2 block">{{ t('admin.accounts.openai.endpointCapabilities') }}</label>
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <label
              v-for="option in openAIEndpointCapabilityOptions"
              :key="option.value"
              class="flex cursor-pointer items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"
            >
              <input
                type="checkbox"
                class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                :data-testid="`openai-endpoint-capability-${option.value}`"
                :checked="openAIEndpointCapabilities.includes(option.value)"
                @change="toggleEndpointCapability(option.value, $event)"
              />
              <span class="text-gray-700 dark:text-gray-200">{{ option.label }}</span>
            </label>
          </div>
          <p class="input-hint">{{ t('admin.accounts.openai.endpointCapabilitiesDesc') }}</p>
        </div>
      </div>

      <div
        v-if="account?.type === 'apikey'"
        class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div>
          <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
          </p>
        </div>
        <Toggle
          :model-value="upstreamBillingProbeEnabled"
          data-testid="upstream-billing-auto-probe"
          :aria-label="t('admin.accounts.upstreamBilling.autoProbe')"
          @update:model-value="handleUpstreamBillingProbeChange"
        />
      </div>

      <OllamaCloudUsageSettings
        v-if="account?.ollama_cloud_usage?.eligible"
        :account="account"
        @updated="handleOllamaCloudUsageUpdated"
      />

      <!-- Anthropic API Key 自动透传开关 -->
      <div
        v-if="account?.platform === 'anthropic' && account?.type === 'apikey'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.apiKeyPassthrough') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.apiKeyPassthroughDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="anthropicPassthroughEnabled = !anthropicPassthroughEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              anthropicPassthroughEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                anthropicPassthroughEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div
        v-if="account?.platform === 'anthropic' && account?.type === 'apikey'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.apiKeyAuthScheme') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.apiKeyAuthSchemeDesc') }}
            </p>
          </div>
          <select v-model="anthropicAPIKeyAuthScheme" class="input w-52 text-sm" data-testid="edit-anthropic-auth-scheme">
            <option value="x_api_key">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeXApiKey') }}</option>
            <option value="authorization_bearer">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeBearer') }}</option>
          </select>
        </div>
      </div>

      <!-- Anthropic API Key: Web Search Emulation (hidden when global disabled) -->
      <div
        v-if="account?.platform === 'anthropic' && account?.type === 'apikey' && webSearchGlobalEnabled"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.webSearchEmulation') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.webSearchEmulationDesc') }}
            </p>
          </div>
          <select v-model="webSearchEmulationMode" class="input w-24 text-sm" data-testid="edit-anthropic-web-search-mode">
            <option value="default">{{ t('admin.accounts.anthropic.webSearchDefault') }}</option>
            <option value="enabled">{{ t('admin.accounts.anthropic.webSearchEnabled') }}</option>
            <option value="disabled">{{ t('admin.accounts.anthropic.webSearchDisabled') }}</option>
          </select>
        </div>
      </div>

      <!-- 配额控制 (Anthropic apikey/bedrock: 配额限制 + 亲和) -->
      <div
        v-if="account?.platform === 'anthropic' && (account?.type === 'apikey' || account?.type === 'bedrock')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaControl.hint') }}
          </p>
        </div>
        <QuotaLimitCard
          test-id-prefix="edit"
          :totalLimit="quotaLimit"
          :dailyLimit="quotaDailyLimit"
          :weeklyLimit="quotaWeeklyLimit"
          :dailyResetMode="quotaDailyResetMode"
          :dailyResetHour="quotaDailyResetHour"
          :weeklyResetMode="quotaWeeklyResetMode"
          :weeklyResetDay="quotaWeeklyResetDay"
          :weeklyResetHour="quotaWeeklyResetHour"
          :resetTimezone="quotaResetTimezone"
          :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
          :quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled"
          :quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold"
          :quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType"
          :quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled"
          :quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold"
          :quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType"
          :quotaNotifyTotalEnabled="quotaNotifyState.total.enabled"
          :quotaNotifyTotalThreshold="quotaNotifyState.total.threshold"
          :quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType"
          @update:totalLimit="quotaLimit = $event"
          @update:dailyLimit="quotaDailyLimit = $event"
          @update:weeklyLimit="quotaWeeklyLimit = $event"
          @update:dailyResetMode="quotaDailyResetMode = $event"
          @update:dailyResetHour="quotaDailyResetHour = $event"
          @update:weeklyResetMode="quotaWeeklyResetMode = $event"
          @update:weeklyResetDay="quotaWeeklyResetDay = $event"
          @update:weeklyResetHour="quotaWeeklyResetHour = $event"
          @update:resetTimezone="quotaResetTimezone = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
        />
      </div>
      <!-- 配额控制 (非 Anthropic apikey/bedrock) -->
      <div
        v-else-if="account?.type === 'apikey' || account?.type === 'bedrock'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaLimitHint') }}
          </p>
        </div>
        <QuotaLimitCard
          test-id-prefix="edit"
          :totalLimit="quotaLimit"
          :dailyLimit="quotaDailyLimit"
          :weeklyLimit="quotaWeeklyLimit"
          :dailyResetMode="quotaDailyResetMode"
          :dailyResetHour="quotaDailyResetHour"
          :weeklyResetMode="quotaWeeklyResetMode"
          :weeklyResetDay="quotaWeeklyResetDay"
          :weeklyResetHour="quotaWeeklyResetHour"
          :resetTimezone="quotaResetTimezone"
          :quotaNotifyGlobalEnabled="quotaNotifyGlobalEnabled"
          :quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled"
          :quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold"
          :quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType"
          :quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled"
          :quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold"
          :quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType"
          :quotaNotifyTotalEnabled="quotaNotifyState.total.enabled"
          :quotaNotifyTotalThreshold="quotaNotifyState.total.threshold"
          :quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType"
          @update:totalLimit="quotaLimit = $event"
          @update:dailyLimit="quotaDailyLimit = $event"
          @update:weeklyLimit="quotaWeeklyLimit = $event"
          @update:dailyResetMode="quotaDailyResetMode = $event"
          @update:dailyResetHour="quotaDailyResetHour = $event"
          @update:weeklyResetMode="quotaWeeklyResetMode = $event"
          @update:weeklyResetDay="quotaWeeklyResetDay = $event"
          @update:weeklyResetHour="quotaWeeklyResetHour = $event"
          @update:resetTimezone="quotaResetTimezone = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
        />
      </div>

      <!-- OpenAI API 长上下文计费开关 -->
      <div
        v-if="account?.platform === 'openai' && !isSparkShadow && !hideOpenAILongContextToggle && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.longContextBilling') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.longContextBillingDesc') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="openai-long-context-billing-toggle"
            role="switch"
            :aria-checked="openAILongContextBillingEnabled"
            @click="openAILongContextBillingEnabled = !openAILongContextBillingEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAILongContextBillingEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAILongContextBillingEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnly') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="openAICodexCLIOnlyEnabled = !openAICodexCLIOnlyEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAICodexCLIOnlyEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAICodexCLIOnlyEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <div
          v-if="openAICodexCLIOnlyEnabled"
          class="mt-4 flex items-center justify-between border-l-2 border-gray-200 pl-4 dark:border-dark-600"
        >
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexCLIOnlyAppServer') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexCLIOnlyAppServerDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="openAICodexCLIOnlyAppServerEnabled = !openAICodexCLIOnlyAppServerEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openAICodexCLIOnlyAppServerEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openAICodexCLIOnlyAppServerEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Codex 指纹收敛模式（仅 OpenAI OAuth） -->
      <div
        v-if="account?.platform === 'openai' && account?.type === 'oauth'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.openai.codexFingerprintMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.codexFingerprintModeDesc') }}
            </p>
          </div>
          <div class="w-52 flex-shrink-0">
            <Select v-model="codexFingerprintMode" data-testid="edit-codex-fingerprint-mode-select" :options="codexFingerprintModeOptions" />
          </div>
        </div>
      </div>

      <!-- OpenAI 订阅档位手动覆盖（Plus/Pro/Free），仅 OAuth 非影子账号 -->
      <div
        v-if="account?.platform === 'openai' && account?.type === 'oauth' && !isSparkShadow"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.openai.planType') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.planTypeDesc') }}
            </p>
          </div>
          <div class="w-44 flex-shrink-0">
            <Select v-model="openAIPlanType" :options="planTypeOptions" />
          </div>
        </div>
      </div>

      <div
        v-if="account?.platform === 'openai' && (account?.type === 'oauth' || account?.type === 'setup-token' || account?.type === 'apikey')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.openai.compactMode') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.compactModeDesc') }}
            </p>
          </div>
          <div class="w-44">
            <Select v-model="openAICompactMode" :options="openAICompactModeOptions" />
          </div>
        </div>
        <div class="rounded-lg bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
          <span class="font-medium">{{ t(openAICompactStatusKey) }}</span>
          <span
            v-if="account?.extra?.openai_compact_checked_at"
            class="ml-2 text-gray-500 dark:text-gray-400"
          >
            {{ t('admin.accounts.openai.compactLastChecked') }}:
            {{ formatDateTime(new Date(String(account.extra.openai_compact_checked_at))) }}
          </span>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.openai.compactModelMapping') }}</label>
          <p class="input-hint">{{ t('admin.accounts.openai.compactModelMappingDesc') }}</p>
          <div v-if="openAICompactModelMappings.length > 0" class="mb-3 space-y-2">
            <div
              v-for="(mapping, index) in openAICompactModelMappings"
              :key="getOpenAICompactModelMappingKey(mapping)"
              class="flex items-center gap-2"
            >
              <input
                v-model="mapping.from"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.fromModel')"
              />
              <span class="text-gray-400">→</span>
              <input
                v-model="mapping.to"
                type="text"
                class="input flex-1"
                :placeholder="t('admin.accounts.toModel')"
              />
              <button type="button" @click="removeOpenAICompactModelMapping(index)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <button type="button" @click="addOpenAICompactModelMapping" class="btn btn-secondary text-sm">
            + {{ t('admin.accounts.addMapping') }}
          </button>
        </div>
      </div>
      <div>
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{
              t('admin.accounts.autoPauseOnExpired')
            }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.autoPauseOnExpiredDesc') }}
            </p>
          </div>
          <button
            type="button"
            @click="autoPauseOnExpired = !autoPauseOnExpired"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              autoPauseOnExpired ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                autoPauseOnExpired ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div
        v-if="account?.platform === 'openai'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause5hDisabled') }}</label>
            <button
              type="button"
              @click="autoPause5hDisabled = !autoPause5hDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause5hDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
              data-testid="auto-pause-5h-disabled"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  autoPause5hDisabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <p class="input-hint">{{ t('admin.accounts.autoPauseDisabledHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.autoPause5hThreshold') }}</label>
          <input
            v-model.number="autoPause5hThresholdPercent"
            type="number"
            min="0"
            max="100"
            step="0.1"
            class="input"
            :disabled="autoPause5hDisabled"
            data-testid="auto-pause-5h-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause7dDisabled') }}</label>
            <button
              type="button"
              @click="autoPause7dDisabled = !autoPause7dDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause7dDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
              data-testid="auto-pause-7d-disabled"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  autoPause7dDisabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <p class="input-hint">{{ t('admin.accounts.autoPauseDisabledHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.autoPause7dThreshold') }}</label>
          <input
            v-model.number="autoPause7dThresholdPercent"
            type="number"
            min="0"
            max="100"
            step="0.1"
            class="input"
            :disabled="autoPause7dDisabled"
            data-testid="auto-pause-7d-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
      </div>

      <div
        v-if="account?.platform === 'openai' && account?.type === 'oauth' && !isSparkShadow"
        class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="auto-reset-credit-settings"
      >
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0">
            <label class="input-label mb-0">{{ t('admin.accounts.autoResetCredit.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.autoResetCredit.hint') }}
            </p>
          </div>
          <button
            type="button"
            data-testid="auto-reset-credit-enabled"
            @click="autoResetCreditEnabled = !autoResetCreditEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              autoResetCreditEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                autoResetCreditEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold5h') }}</label>
            <input
              v-model.number="autoResetCredit5hThreshold"
              type="number"
              min="0.1"
              max="100"
              step="0.1"
              class="input"
              :disabled="!autoResetCreditEnabled"
              data-testid="auto-reset-credit-5h-threshold"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold7d') }}</label>
            <input
              v-model.number="autoResetCredit7dThreshold"
              type="number"
              min="0.1"
              max="100"
              step="0.1"
              class="input"
              :disabled="!autoResetCreditEnabled"
              data-testid="auto-reset-credit-7d-threshold"
            />
          </div>
        </div>
        <p class="input-hint">{{ t('admin.accounts.autoResetCredit.thresholdHint') }}</p>
      </div>

      <!-- 配额控制 (Anthropic OAuth/SetupToken: 亲和 + 窗口费用 + 会话 + RPM 等) -->
      <div
        v-if="account?.platform === 'anthropic' && (account?.type === 'oauth' || account?.type === 'setup-token')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaControl.hint') }}
          </p>
        </div>

        <!-- Window Cost Limit -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.windowCost.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.windowCost.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="windowCostEnabled = !windowCostEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                windowCostEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  windowCostEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="windowCostEnabled" class="grid grid-cols-2 gap-4">
            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.windowCost.limit') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">$</span>
                <input
                  v-model.number="windowCostLimit"
                  type="number"
                  min="0"
                  step="1"
                  class="input pl-7"
                  :placeholder="t('admin.accounts.quotaControl.windowCost.limitPlaceholder')"
                />
              </div>
              <p class="input-hint">{{ t('admin.accounts.quotaControl.windowCost.limitHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.windowCost.stickyReserve') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">$</span>
                <input
                  v-model.number="windowCostStickyReserve"
                  type="number"
                  min="0"
                  step="1"
                  class="input pl-7"
                  :placeholder="t('admin.accounts.quotaControl.windowCost.stickyReservePlaceholder')"
                />
              </div>
              <p class="input-hint">{{ t('admin.accounts.quotaControl.windowCost.stickyReserveHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Session Limit -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.sessionLimit.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.sessionLimit.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="sessionLimitEnabled = !sessionLimitEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                sessionLimitEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  sessionLimitEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="sessionLimitEnabled" class="grid grid-cols-2 gap-4">
            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.sessionLimit.maxSessions') }}</label>
              <input
                v-model.number="maxSessions"
                type="number"
                min="1"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.sessionLimit.maxSessionsPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.sessionLimit.maxSessionsHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.sessionLimit.idleTimeout') }}</label>
              <div class="relative">
                <input
                  v-model.number="sessionIdleTimeout"
                  type="number"
                  min="1"
                  step="1"
                  class="input pr-12"
                  :placeholder="t('admin.accounts.quotaControl.sessionLimit.idleTimeoutPlaceholder')"
                />
                <span class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">{{ t('common.minutes') }}</span>
              </div>
              <p class="input-hint">{{ t('admin.accounts.quotaControl.sessionLimit.idleTimeoutHint') }}</p>
            </div>
          </div>
        </div>

        <!-- RPM Limit -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="mb-3 flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.rpmLimit.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.rpmLimit.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="rpmLimitEnabled = !rpmLimitEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                rpmLimitEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  rpmLimitEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="rpmLimitEnabled" class="space-y-4">
            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpm') }}</label>
              <input
                v-model.number="baseRpm"
                type="number"
                min="1"
                max="1000"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.baseRpmPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpmHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.strategy') }}</label>
              <div class="flex gap-2">
                <button
                  type="button"
                  @click="rpmStrategy = 'tiered'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    rpmStrategy === 'tiered'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  <div class="text-center">
                    <div>{{ t('admin.accounts.quotaControl.rpmLimit.strategyTiered') }}</div>
                    <div class="mt-0.5 text-[10px] opacity-70">{{ t('admin.accounts.quotaControl.rpmLimit.strategyTieredHint') }}</div>
                  </div>
                </button>
                <button
                  type="button"
                  @click="rpmStrategy = 'sticky_exempt'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    rpmStrategy === 'sticky_exempt'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  <div class="text-center">
                    <div>{{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExempt') }}</div>
                    <div class="mt-0.5 text-[10px] opacity-70">{{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExemptHint') }}</div>
                  </div>
                </button>
              </div>
            </div>

            <div v-if="rpmStrategy === 'tiered'">
              <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBuffer') }}</label>
              <input
                v-model.number="rpmStickyBuffer"
                type="number"
                min="1"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.stickyBufferPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBufferHint') }}</p>
            </div>

          </div>

          <!-- 用户消息限速模式（独立于 RPM 开关，始终可见） -->
          <div class="mt-4">
            <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueue') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 mb-2">
              {{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueueHint') }}
            </p>
            <div class="flex space-x-2">
              <button type="button" v-for="opt in umqModeOptions" :key="opt.value"
                @click="userMsgQueueMode = opt.value"
                :class="[
                  'px-3 py-1.5 text-sm rounded-md border transition-colors',
                  userMsgQueueMode === opt.value
                    ? 'bg-primary-600 text-white border-primary-600'
                    : 'bg-white dark:bg-dark-700 text-gray-700 dark:text-gray-300 border-gray-300 dark:border-dark-500 hover:bg-gray-50 dark:hover:bg-dark-600'
                ]">
                {{ opt.label }}
              </button>
            </div>
          </div>
        </div>

        <!-- TLS Fingerprint -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.tlsFingerprint.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.tlsFingerprint.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="tlsFingerprintEnabled = !tlsFingerprintEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                tlsFingerprintEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  tlsFingerprintEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <!-- Profile selector -->
          <div v-if="tlsFingerprintEnabled" class="mt-3">
            <select :key="tlsFingerprintProfiles.length" v-model="tlsFingerprintProfileId" class="input" data-testid="edit-tls-fingerprint-profile-select">
              <option :value="null">{{ t('admin.accounts.quotaControl.tlsFingerprint.defaultProfile') }}</option>
              <option v-if="tlsFingerprintProfiles.length > 0" :value="-1">{{ t('admin.accounts.quotaControl.tlsFingerprint.randomProfile') }}</option>
              <option v-for="p in tlsFingerprintProfiles" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
        </div>

        <!-- Session ID Masking -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.sessionIdMasking.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.sessionIdMasking.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="sessionIdMaskingEnabled = !sessionIdMaskingEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                sessionIdMaskingEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  sessionIdMaskingEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
        </div>

        <!-- Cache TTL Override -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.cacheTTLOverride.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.cacheTTLOverride.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="cacheTTLOverrideEnabled = !cacheTTLOverrideEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                cacheTTLOverrideEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  cacheTTLOverrideEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="cacheTTLOverrideEnabled" class="mt-3">
            <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.cacheTTLOverride.target') }}</label>
            <select
              v-model="cacheTTLOverrideTarget"
              class="mt-1 block w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700 dark:text-white"
            >
              <option value="5m">5m</option>
              <option value="1h">1h</option>
            </select>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.quotaControl.cacheTTLOverride.targetHint') }}
            </p>
          </div>
        </div>

        <!-- Custom Base URL Relay -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex items-center justify-between">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.quotaControl.customBaseUrl.label') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaControl.customBaseUrl.hint') }}
              </p>
            </div>
            <button
              type="button"
              @click="customBaseUrlEnabled = !customBaseUrlEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                customBaseUrlEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  customBaseUrlEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="customBaseUrlEnabled" class="mt-3">
            <input
              v-model="customBaseUrl"
              type="text"
              class="input"
              :placeholder="t('admin.accounts.quotaControl.customBaseUrl.urlHint')"
            />
          </div>
        </div>
      </div>


      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div>
          <label class="input-label">{{ t('common.status') }}</label>
          <Select v-model="status" :options="statusOptions" data-testid="edit-account-status" />
        </div>
      </div>

      <!-- Group Selection - 仅标准模式显示 -->
      <GroupSelector
        v-if="!authStore.isSimpleMode"
        v-model="groupIds"
        :groups="groups"
        :platform="account?.platform"
        data-tour="account-form-groups"
      />

    </form>

    <template #footer>
      <div v-if="account" class="flex justify-end gap-3">
        <button @click="handleClose" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="edit-account-form"
          :disabled="submitting"
          class="btn btn-primary"
          data-tour="account-form-submit"
        >
          <svg
            v-if="submitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
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
          {{ submitting ? t('admin.accounts.updating') : t('common.update') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import {
  buildModelMappingObject,
  commonErrorCodes,
  getPresetMappingsByPlatform,
  splitModelMappingObject,
  type ModelMappingEntry
} from '@/composables/useModelWhitelist'
import { useQuotaNotifyState } from '@/composables/useQuotaNotifyState'
import { allSelectedGroupsEnableLongContextPricing } from './longContextBilling'
import {
  applyHeaderOverride,
  applyInterceptWarmup,
  applyPlanType,
  buildPlanTypeOptions,
  isHeaderOverrideCapable,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows,
  type HeaderOverrideRow
} from './credentialsBuilder'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import type {
  Account,
  AdminGroup,
  OpenAICompactMode,
  OpenAIEndpointCapability,
  OpenAIResponsesMode,
  OllamaCloudUsageState,
  Proxy,
  UpdateAccountRequest
} from '@/types'
import {
  resolveOpenAIWSModeConcurrencyHintKey,
  type OpenAIWSMode
} from '@/utils/openaiWsMode'
import { formatDateTime, formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxyAdBanner from '@/components/common/ProxyAdBanner.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import HeaderOverrideEditor from './HeaderOverrideEditor.vue'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import OllamaCloudUsageSettings from './OllamaCloudUsageSettings.vue'
import QuotaLimitCard from './QuotaLimitCard.vue'

type CodexImageToolMode = 'inherit' | 'enabled' | 'disabled' | 'block'
type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'
type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'
type BedrockAuthMode = 'sigv4' | 'apikey'
interface ModelMapping {
  from: string
  to: string
}
interface TempUnschedRuleForm {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
}

const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-model-mapping')
const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('edit-openai-compact-model-mapping')
const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('edit-temp-unsched-rule')

const props = defineProps<{
  show: boolean
  account: Account | null
  proxies: Proxy[]
  groups: AdminGroup[]
}>()
const emit = defineEmits<{ close: []; updated: [account: Account] }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const supported = computed(() =>
  props.account?.platform === 'anthropic' || props.account?.platform === 'openai'
)
const isOpenAI = computed(() => props.account?.platform === 'openai')
const isApiKey = computed(() => props.account?.type === 'apikey')
const isBedrock = computed(() => props.account?.type === 'bedrock' && props.account?.platform === 'anthropic')
const isServiceAccount = computed(() => props.account?.type === 'service_account')
const isAnthropicApiKey = computed(() =>
  props.account?.platform === 'anthropic' && props.account?.type === 'apikey'
)
const isAnthropicOAuth = computed(() =>
  props.account?.platform === 'anthropic' &&
  (props.account?.type === 'oauth' || props.account?.type === 'setup-token')
)
const isSparkShadow = computed(() => Boolean(props.account?.parent_account_id))

const handleOllamaCloudUsageUpdated = (state: OllamaCloudUsageState) => {
  if (props.account) emit('updated', { ...props.account, ollama_cloud_usage: state })
}
const modelPlatform = computed(() => props.account?.type === 'bedrock' ? 'bedrock' : props.account?.platform)
const supportsAccountSchedulingThresholdOverride = computed(() =>
  props.account?.platform === 'anthropic' || props.account?.platform === 'openai'
)

const submitting = ref(false)
const name = ref('')
const notes = ref('')
const newApiKey = ref('')
const baseUrl = ref('')
const status = ref<'active' | 'inactive' | 'error'>('active')
const bedrockAuthMode = ref<BedrockAuthMode>('sigv4')
const bedrockAccessKeyId = ref('')
const bedrockSecretAccessKey = ref('')
const bedrockSessionToken = ref('')
const bedrockApiKey = ref('')
const bedrockRegion = ref('us-east-1')
const bedrockForceGlobal = ref(false)
const serviceAccountJson = ref('')
const vertexProjectId = ref('')
const vertexLocation = ref('global')
const concurrency = ref(1)
const priority = ref(0)
const rateMultiplier = ref(1)
const proxyId = ref<number | null>(null)
const groupIds = ref<number[]>([])
const allowedModels = ref<string[]>([])
const preservedModelMappings = ref<ModelMappingEntry[]>([])
const compactModelMapping = ref<Record<string, unknown> | null>(null)
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const modelMappings = ref<ModelMapping[]>([])
const openAICompactModelMappings = ref<ModelMapping[]>([])
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const openAILongContextBillingEnabled = ref(false)
const openAIFlattenNamespaces = ref(false)
const openAICompactMode = ref<OpenAICompactMode>('auto')
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>([
  'chat_completions',
  'embeddings'
])
const openAIWSMode = ref<OpenAIWSMode>('off')
const codexImageToolMode = ref<CodexImageToolMode>('inherit')
const autoPause5hThresholdPercent = ref<number | null>(null)
const autoPause7dThresholdPercent = ref<number | null>(null)
const autoPause5hDisabled = ref(false)
const autoPause7dDisabled = ref(false)
const autoResetCreditEnabled = ref(false)
const autoResetCredit5hThreshold = ref(100)
const autoResetCredit7dThreshold = ref(100)
const upstreamBillingProbeEnabled = ref(false)
const upstreamBillingRateSyncEnabled = ref(false)
const openaiPassthroughEnabled = ref(false)
const openAICodexCLIOnlyEnabled = ref(false)
const openAICodexCLIOnlyAppServerEnabled = ref(false)
const codexFingerprintMode = ref<CodexFingerprintMode>('off')
const openAIPlanType = ref('')
const anthropicPassthroughEnabled = ref(false)
const anthropicAPIKeyAuthScheme = ref<AnthropicAPIKeyAuthScheme>('x_api_key')
const webSearchEmulationMode = ref<'default' | 'enabled' | 'disabled'>('default')
const webSearchGlobalEnabled = ref(false)
const DEFAULT_POOL_MODE_RETRY_COUNT = 3
const MAX_POOL_MODE_RETRY_COUNT = 10
const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = [401, 403, 429]
const poolModeEnabled = ref(false)
const poolModeRetryCount = ref(3)
const poolModeRetryStatusCodesInput = ref('')
const customErrorCodesEnabled = ref(false)
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | null>(null)
const interceptWarmupRequests = ref(false)
const tempUnschedEnabled = ref(false)
const tempUnschedRules = ref<TempUnschedRuleForm[]>([])
const autoPauseOnExpired = ref(false)
const loadFactor = ref<number | null>(null)
const expiresAt = ref<number | null>(null)
const accountSchedulingThresholdOverrideEnabled = ref(false)
const accountSchedulingThresholdOverrideValue = ref(100)
const ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY = 'account_scheduling_threshold'
const windowCostEnabled = ref(false)
const windowCostLimit = ref<number | null>(null)
const windowCostStickyReserve = ref<number | null>(null)
const sessionLimitEnabled = ref(false)
const maxSessions = ref<number | null>(null)
const sessionIdleTimeout = ref<number | null>(null)
const rpmLimitEnabled = ref(false)
const baseRpm = ref<number | null>(null)
const rpmStrategy = ref<'tiered' | 'sticky_exempt'>('tiered')
const rpmStickyBuffer = ref<number | null>(null)
const userMsgQueueMode = ref('')
const tlsFingerprintEnabled = ref(false)
const tlsFingerprintProfileId = ref<number | null>(null)
const tlsFingerprintProfiles = ref<Array<{ id: number; name: string }>>([])
const sessionIdMaskingEnabled = ref(false)
const cacheTTLOverrideEnabled = ref(false)
const cacheTTLOverrideTarget = ref('5m')
const customBaseUrlEnabled = ref(false)
const customBaseUrl = ref('')
const quotaLimit = ref<number | null>(null)
const quotaDailyLimit = ref<number | null>(null)
const quotaWeeklyLimit = ref<number | null>(null)
const quotaDailyResetMode = ref<'rolling' | 'fixed' | null>(null)
const quotaDailyResetHour = ref<number | null>(null)
const quotaWeeklyResetMode = ref<'rolling' | 'fixed' | null>(null)
const quotaWeeklyResetDay = ref<number | null>(null)
const quotaWeeklyResetHour = ref<number | null>(null)
const quotaResetTimezone = ref<string | null>(null)
const {
  globalEnabled: quotaNotifyGlobalEnabled,
  state: quotaNotifyState,
  loadGlobalState: loadQuotaNotifyGlobal,
  loadFromExtra: loadQuotaNotifyFromExtra,
  writeToExtra: writeQuotaNotifyToExtra,
} = useQuotaNotifyState()

loadQuotaNotifyGlobal()

const baseUrlPlaceholder = computed(() =>
  isOpenAI.value ? 'https://api.openai.com' : 'https://api.anthropic.com'
)
const baseUrlHint = computed(() =>
  isOpenAI.value
    ? t('admin.accounts.openai.baseUrlHint')
    : t('admin.accounts.baseUrlHint')
)
const statusOptions = computed(() => {
  const options: Array<{ value: 'active' | 'inactive' | 'error'; label: string }> = [
    { value: 'active', label: t('common.active') },
    { value: 'inactive', label: t('common.inactive') }
  ]
  if (status.value === 'error') {
    options.push({ value: 'error', label: t('admin.accounts.status.error') })
  }
  return options
})
const headerOverrideCapable = computed(() =>
  Boolean(props.account && isHeaderOverrideCapable(props.account.platform, props.account.type))
)
const hideOpenAILongContextToggle = computed(() =>
  !authStore.isSimpleMode &&
  allSelectedGroupsEnableLongContextPricing(groupIds.value, props.groups)
)
const openAICompactModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.compactModeAuto') },
  { value: 'force_on', label: t('admin.accounts.openai.compactModeForceOn') },
  { value: 'force_off', label: t('admin.accounts.openai.compactModeForceOff') }
])
const openAIResponsesModeOptions = computed(() => [
  { value: 'auto', label: t('admin.accounts.openai.responsesModeAuto') },
  { value: 'force_responses', label: t('admin.accounts.openai.responsesModeForceResponses') },
  { value: 'force_chat_completions', label: t('admin.accounts.openai.responsesModeForceChatCompletions') }
])
const openAIWSModeOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.wsModeOff') },
  { value: 'ctx_pool', label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: 'passthrough', label: t('admin.accounts.openai.wsModePassthrough') },
  { value: 'http_bridge', label: t('admin.accounts.openai.wsModeHttpBridge') }
])
const openAIWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openAIWSMode.value)
)
const codexImageToolOptions = computed<Array<{
  value: CodexImageToolMode
  label: string
  description: string
  selectedCardClass: string
  selectedDotClass: string
}>>(() => [
  {
    value: 'inherit',
    label: t('admin.accounts.openai.codexImageToolInherit'),
    description: t('admin.accounts.openai.codexImageToolInheritDesc'),
    selectedCardClass: 'border-sky-300 bg-sky-50 text-sky-900 shadow-sm ring-1 ring-sky-200 dark:border-sky-700 dark:bg-sky-900/25 dark:text-sky-100 dark:ring-sky-800',
    selectedDotClass: 'border-sky-500 bg-sky-500 text-white'
  },
  {
    value: 'enabled',
    label: t('admin.accounts.openai.codexImageToolEnabled'),
    description: t('admin.accounts.openai.codexImageToolEnabledDesc'),
    selectedCardClass: 'border-emerald-300 bg-emerald-50 text-emerald-900 shadow-sm ring-1 ring-emerald-200 dark:border-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-100 dark:ring-emerald-800',
    selectedDotClass: 'border-emerald-500 bg-emerald-500 text-white'
  },
  {
    value: 'disabled',
    label: t('admin.accounts.openai.codexImageToolDisabled'),
    description: t('admin.accounts.openai.codexImageToolDisabledDesc'),
    selectedCardClass: 'border-amber-300 bg-amber-50 text-amber-900 shadow-sm ring-1 ring-amber-200 dark:border-amber-700 dark:bg-amber-900/25 dark:text-amber-100 dark:ring-amber-800',
    selectedDotClass: 'border-amber-500 bg-amber-500 text-white'
  },
  {
    value: 'block',
    label: t('admin.accounts.openai.codexImageToolBlock'),
    description: t('admin.accounts.openai.codexImageToolBlockDesc'),
    selectedCardClass: 'border-rose-300 bg-rose-50 text-rose-900 shadow-sm ring-1 ring-rose-200 dark:border-rose-700 dark:bg-rose-900/25 dark:text-rose-100 dark:ring-rose-800',
    selectedDotClass: 'border-rose-500 bg-rose-500 text-white'
  }
])
const codexImageToolBadgeLabel = computed(() => {
  if (codexImageToolMode.value === 'enabled') return t('admin.accounts.openai.codexImageToolBadgeEnabled')
  if (codexImageToolMode.value === 'disabled') return t('admin.accounts.openai.codexImageToolBadgeDisabled')
  if (codexImageToolMode.value === 'block') return t('admin.accounts.openai.codexImageToolBadgeBlock')
  return t('admin.accounts.openai.codexImageToolBadgeInherit')
})
const codexImageToolBadgeClass = computed(() => {
  if (codexImageToolMode.value === 'enabled') {
    return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
  }
  if (codexImageToolMode.value === 'disabled') {
    return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
  }
  if (codexImageToolMode.value === 'block') {
    return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
  }
  return 'bg-slate-100 text-slate-600 dark:bg-dark-600 dark:text-slate-300'
})
const openAIResponsesStatusKey = computed(() => {
  if (!props.account || !isOpenAI.value || !isApiKey.value) return ''
  if (openAIResponsesMode.value === 'force_responses') {
    return 'admin.accounts.openai.responsesStatusForcedResponses'
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return 'admin.accounts.openai.responsesStatusForcedChatCompletions'
  }
  const extra = (props.account.extra || {}) as Record<string, unknown>
  if (extra.openai_responses_supported === true) {
    return 'admin.accounts.openai.responsesStatusAutoSupported'
  }
  if (extra.openai_responses_supported === false) {
    return 'admin.accounts.openai.responsesStatusAutoUnsupported'
  }
  return 'admin.accounts.openai.responsesStatusAutoUnknown'
})
const openAICompactStatusKey = computed(() => {
  if (!props.account || !isOpenAI.value) return ''
  if (openAICompactMode.value === 'force_on') return 'admin.accounts.openai.compactSupported'
  if (openAICompactMode.value === 'force_off') return 'admin.accounts.openai.compactUnsupported'
  const extra = (props.account.extra || {}) as Record<string, unknown>
  if (extra.openai_compact_supported === true) return 'admin.accounts.openai.compactSupported'
  if (extra.openai_compact_supported === false) return 'admin.accounts.openai.compactUnsupported'
  return 'admin.accounts.openai.compactAuto'
})
const codexFingerprintModeOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.codexFingerprintOff') },
  { value: 'device', label: t('admin.accounts.openai.codexFingerprintDevice') },
  { value: 'session', label: t('admin.accounts.openai.codexFingerprintSession') },
  { value: 'full', label: t('admin.accounts.openai.codexFingerprintFull') }
])
const planTypeOptions = computed(() =>
  buildPlanTypeOptions(openAIPlanType.value, t('admin.accounts.openai.planTypeClear'))
)
const expiresAtInput = computed({
  get: () => formatDateTimeLocalInput(expiresAt.value),
  set: (value: string) => { expiresAt.value = parseDateTimeLocalInput(value) }
})
const openAITextGenerationEnabled = computed(() => openAIEndpointCapabilities.value.includes('chat_completions'))
const openAITextEndpointCapabilityLabel = computed(() => {
  if (openAIResponsesMode.value === 'force_responses') {
    return t('admin.accounts.openai.capabilityResponses')
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return t('admin.accounts.openai.capabilityChatCompletions')
  }
  const extra = (props.account?.extra || {}) as Record<string, unknown>
  if (extra.openai_responses_supported === true) {
    return t('admin.accounts.openai.capabilityResponsesAuto')
  }
  if (extra.openai_responses_supported === false) {
    return t('admin.accounts.openai.capabilityChatCompletionsAuto')
  }
  return t('admin.accounts.openai.capabilityTextAuto')
})
const openAIEndpointCapabilityOptions = computed<Array<{
  value: OpenAIEndpointCapability
  label: string
}>>(() => [
  { value: 'chat_completions', label: openAITextEndpointCapabilityLabel.value },
  { value: 'embeddings', label: t('admin.accounts.openai.capabilityEmbeddings') }
])
const isOpenAIModelRestrictionDisabled = computed(() =>
  isOpenAI.value && openaiPassthroughEnabled.value
)
const presetMappings = computed(() => getPresetMappingsByPlatform(modelPlatform.value || 'anthropic'))
const umqModeOptions = computed(() => [
  { value: '', label: t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
  { value: 'throttle', label: t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
  { value: 'serialize', label: t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') }
])
const tempUnschedPresets = computed(() => [
  {
    label: t('admin.accounts.tempUnschedulable.presets.overloadLabel'),
    rule: {
      error_code: 529,
      keywords: 'overloaded, too many',
      duration_minutes: 60,
      description: t('admin.accounts.tempUnschedulable.presets.overloadDesc')
    }
  },
  {
    label: t('admin.accounts.tempUnschedulable.presets.rateLimitLabel'),
    rule: {
      error_code: 429,
      keywords: 'rate limit, too many requests',
      duration_minutes: 10,
      description: t('admin.accounts.tempUnschedulable.presets.rateLimitDesc')
    }
  },
  {
    label: t('admin.accounts.tempUnschedulable.presets.unavailableLabel'),
    rule: {
      error_code: 503,
      keywords: 'unavailable, maintenance',
      duration_minutes: 30,
      description: t('admin.accounts.tempUnschedulable.presets.unavailableDesc')
    }
  }
])

watch(
  () => [props.show, props.account] as const,
  ([visible]) => {
    if (visible && props.account && supported.value) {
      hydrate()
      void loadTLSProfiles()
      void loadWebSearchEmulationConfig()
    }
    if (visible && props.account && !supported.value) emit('close')
  },
  { immediate: true }
)

async function loadTLSProfiles() {
  try {
    const profiles = await adminAPI.tlsFingerprintProfiles.list()
    tlsFingerprintProfiles.value = profiles.map(profile => ({ id: profile.id, name: profile.name }))
  } catch {
    tlsFingerprintProfiles.value = []
  }
}

async function loadWebSearchEmulationConfig() {
  try {
    const config = await adminAPI.settings.getWebSearchEmulationConfig()
    webSearchGlobalEnabled.value = config?.enabled === true && (config.providers?.length ?? 0) > 0
  } catch {
    webSearchGlobalEnabled.value = false
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {}
}

function readBoolean(value: unknown): boolean {
  return value === true
}

function readPercent(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value * 100 : null
}

function normalizeAccountSchedulingThresholdOverride(value: unknown): number | null {
  if (value === null || value === undefined || value === '') return null
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return null
  const integer = Math.trunc(numeric)
  return integer >= 1 && integer <= 100 ? integer : null
}

function loadAccountSchedulingThresholdOverride(credentials: Record<string, unknown>) {
  if (!supportsAccountSchedulingThresholdOverride.value) {
    accountSchedulingThresholdOverrideEnabled.value = false
    accountSchedulingThresholdOverrideValue.value = 100
    return
  }
  const value = normalizeAccountSchedulingThresholdOverride(
    credentials[ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY]
  )
  accountSchedulingThresholdOverrideEnabled.value = value !== null
  accountSchedulingThresholdOverrideValue.value = value ?? 100
}

function applyAccountSchedulingThresholdOverride(
  credentials: Record<string, unknown>,
  currentCredentials: Record<string, unknown>
) {
  if (!supportsAccountSchedulingThresholdOverride.value) return
  const current = normalizeAccountSchedulingThresholdOverride(
    currentCredentials[ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY]
  )
  if (!accountSchedulingThresholdOverrideEnabled.value) {
    if (current !== null) credentials[ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY] = null
    return
  }
  const numeric = Number(accountSchedulingThresholdOverrideValue.value)
  const next = Number.isFinite(numeric)
    ? Math.min(100, Math.max(1, Math.trunc(numeric)))
    : 100
  if (current !== next) credentials[ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY] = next
}

function hydrate() {
  const account = props.account
  if (!account) return
  const credentials = asRecord(account.credentials)
  const extra = asRecord(account.extra)
  name.value = account.name
  notes.value = account.notes || ''
  status.value = account.status
  newApiKey.value = ''
  baseUrl.value = typeof credentials.base_url === 'string'
    ? credentials.base_url
    : account.platform === 'openai'
      ? 'https://api.openai.com'
      : 'https://api.anthropic.com'
  if (isBedrock.value) {
    bedrockAuthMode.value = credentials.auth_mode === 'apikey' ? 'apikey' : 'sigv4'
    // Secret values are never returned by the API. Keep these fields blank so
    // an omitted value lets the backend preserve the stored credential.
    bedrockAccessKeyId.value = typeof credentials.aws_access_key_id === 'string'
      ? credentials.aws_access_key_id
      : ''
    bedrockSecretAccessKey.value = ''
    bedrockSessionToken.value = ''
    bedrockApiKey.value = ''
    bedrockRegion.value = typeof credentials.aws_region === 'string' && credentials.aws_region.trim()
      ? credentials.aws_region
      : 'us-east-1'
    bedrockForceGlobal.value = credentials.aws_force_global === true || credentials.aws_force_global === 'true'
  } else {
    bedrockAuthMode.value = 'sigv4'
    bedrockAccessKeyId.value = ''
    bedrockSecretAccessKey.value = ''
    bedrockSessionToken.value = ''
    bedrockApiKey.value = ''
    bedrockRegion.value = 'us-east-1'
    bedrockForceGlobal.value = false
  }
  serviceAccountJson.value = typeof credentials.service_account_json === 'string'
    ? credentials.service_account_json
    : ''
  vertexProjectId.value = typeof credentials.project_id === 'string'
    ? credentials.project_id
    : ''
  vertexLocation.value = typeof credentials.location === 'string' ? credentials.location : 'global'
  concurrency.value = account.concurrency || 1
  priority.value = account.priority || 0
  rateMultiplier.value = account.rate_multiplier ?? 1
  proxyId.value = account.proxy_id ?? null
  groupIds.value = [...(account.group_ids || [])]
  loadFactor.value = account.load_factor ?? null
  expiresAt.value = account.expires_at ?? null
  autoPauseOnExpired.value = account.auto_pause_on_expired === true
  loadAccountSchedulingThresholdOverride(credentials)
  loadModelMapping(credentials.model_mapping)
  const compact = asRecord(credentials.compact_model_mapping)
  compactModelMapping.value = Object.keys(compact).length > 0 ? { ...compact } : null
  openAICompactModelMappings.value = Object.entries(compact)
    .filter(([, value]) => typeof value === 'string')
    .map(([from, to]) => ({ from, to: String(to) }))
  poolModeEnabled.value = credentials.pool_mode === true
  poolModeRetryCount.value = Number.isFinite(Number(credentials.pool_mode_retry_count))
    ? Math.max(0, Math.min(10, Math.trunc(Number(credentials.pool_mode_retry_count))))
    : 3
  poolModeRetryStatusCodesInput.value = formatRetryStatusCodes(credentials.pool_mode_retry_status_codes)
  customErrorCodesEnabled.value = credentials.custom_error_codes_enabled === true
  selectedErrorCodes.value = Array.isArray(credentials.custom_error_codes)
    ? credentials.custom_error_codes
      .map(value => Number(value))
      .filter(value => Number.isInteger(value) && value >= 100 && value <= 599)
    : []
  customErrorCodeInput.value = null
  interceptWarmupRequests.value = credentials.intercept_warmup_requests === true
  tempUnschedEnabled.value = credentials.temp_unschedulable_enabled === true
  tempUnschedRules.value = parseTempUnschedRules(credentials.temp_unschedulable_rules)
  headerOverrideEnabled.value = credentials.header_override_enabled === true
  headerOverrideRows.value = splitHeaderOverridesObject(credentials.header_overrides)
  openAILongContextBillingEnabled.value = readBoolean(extra.openai_long_context_billing_enabled)
  openAIFlattenNamespaces.value = readBoolean(extra.openai_responses_flatten_namespaces)
  openAICompactMode.value = extra.openai_compact_mode === 'force_on' || extra.openai_compact_mode === 'force_off'
    ? extra.openai_compact_mode
    : 'auto'
  const storedResponsesMode = extra.openai_responses_mode
  openAIResponsesMode.value = storedResponsesMode === 'force_responses' ||
    storedResponsesMode === 'force_chat_completions'
    ? storedResponsesMode
    : 'auto'
  openAIEndpointCapabilities.value = normalizeEndpointCapabilities(credentials.openai_capabilities)
  const wsKey = account.type === 'apikey'
    ? 'openai_apikey_responses_websockets_v2_mode'
    : 'openai_oauth_responses_websockets_v2_mode'
  const storedWSMode = extra[wsKey] ?? extra.responses_websockets_v2_mode
  openAIWSMode.value = storedWSMode === 'ctx_pool' ||
    storedWSMode === 'passthrough' ||
    storedWSMode === 'http_bridge'
    ? storedWSMode
    : 'off'
  const bridgeValue = typeof extra.codex_image_generation_bridge === 'boolean'
    ? extra.codex_image_generation_bridge
    : extra.codex_image_generation_bridge_enabled
  codexImageToolMode.value = extra.codex_image_generation_explicit_tool_policy === 'strip'
    ? 'block'
    : bridgeValue === true
      ? 'enabled'
      : bridgeValue === false
        ? 'disabled'
        : 'inherit'
  autoPause5hThresholdPercent.value = readPercent(extra.auto_pause_5h_threshold)
  autoPause7dThresholdPercent.value = readPercent(extra.auto_pause_7d_threshold)
  autoPause5hDisabled.value = readBoolean(extra.auto_pause_5h_disabled)
  autoPause7dDisabled.value = readBoolean(extra.auto_pause_7d_disabled)
  autoResetCreditEnabled.value = readBoolean(extra.auto_reset_credit_enabled)
  autoResetCredit5hThreshold.value = readPercent(extra.auto_reset_credit_5h_threshold) ?? 100
  autoResetCredit7dThreshold.value = readPercent(extra.auto_reset_credit_7d_threshold) ?? 100
  upstreamBillingProbeEnabled.value = readBoolean(
    asRecord(account).upstream_billing_probe_enabled ?? extra.upstream_billing_probe_enabled
  )
  upstreamBillingRateSyncEnabled.value = readBoolean(
    asRecord(account).upstream_billing_rate_sync_enabled ?? extra.upstream_billing_rate_sync_enabled
  )

  // OpenAI/Codex account controls.  Keep these settings scoped to the
  // supported platform; values from retired providers are never interpreted.
  openaiPassthroughEnabled.value = isOpenAI.value && (
    extra.openai_passthrough === true || extra.openai_oauth_passthrough === true
  )
  openAICodexCLIOnlyEnabled.value = isOpenAI.value &&
    (account.type === 'oauth' || account.type === 'setup-token') &&
    extra.codex_cli_only === true
  openAICodexCLIOnlyAppServerEnabled.value = openAICodexCLIOnlyEnabled.value &&
    extra.codex_cli_only_allow_app_server === true
  const fingerprint = typeof extra.codex_fingerprint_mode === 'string' ? extra.codex_fingerprint_mode : ''
  codexFingerprintMode.value = isOpenAI.value && account.type === 'oauth' &&
    (fingerprint === 'device' || fingerprint === 'session' || fingerprint === 'full')
    ? fingerprint
    : 'off'
  openAIPlanType.value = isOpenAI.value && account.type === 'oauth' && typeof credentials.plan_type === 'string'
    ? credentials.plan_type
    : ''

  // Anthropic API-key passthrough/auth/search controls.
  anthropicPassthroughEnabled.value = isAnthropicApiKey.value && extra.anthropic_passthrough === true
  anthropicAPIKeyAuthScheme.value = isAnthropicApiKey.value &&
    extra.anthropic_apikey_auth_scheme === 'authorization_bearer'
    ? 'authorization_bearer'
    : 'x_api_key'
  const webSearch = extra.web_search_emulation
  webSearchEmulationMode.value = webSearch === 'enabled' || webSearch === true
    ? 'enabled'
    : webSearch === 'disabled'
      ? 'disabled'
      : 'default'

  // Anthropic OAuth/setup-token quota and session controls are surfaced by
  // account DTO fields; fall back to extra for older API responses.
  windowCostLimit.value = typeof account.window_cost_limit === 'number'
    ? account.window_cost_limit
    : typeof extra.window_cost_limit === 'number' ? extra.window_cost_limit : null
  windowCostEnabled.value = isAnthropicOAuth.value && (windowCostLimit.value ?? 0) > 0
  windowCostStickyReserve.value = typeof account.window_cost_sticky_reserve === 'number'
    ? account.window_cost_sticky_reserve
    : typeof extra.window_cost_sticky_reserve === 'number' ? extra.window_cost_sticky_reserve : null
  maxSessions.value = typeof account.max_sessions === 'number'
    ? account.max_sessions
    : typeof extra.max_sessions === 'number' ? extra.max_sessions : null
  sessionLimitEnabled.value = isAnthropicOAuth.value && (maxSessions.value ?? 0) > 0
  sessionIdleTimeout.value = typeof account.session_idle_timeout_minutes === 'number'
    ? account.session_idle_timeout_minutes
    : typeof extra.session_idle_timeout_minutes === 'number' ? extra.session_idle_timeout_minutes : null
  baseRpm.value = typeof account.base_rpm === 'number'
    ? account.base_rpm
    : typeof extra.base_rpm === 'number' ? extra.base_rpm : null
  rpmLimitEnabled.value = isAnthropicOAuth.value && (baseRpm.value ?? 0) > 0
  rpmStrategy.value = account.rpm_strategy === 'sticky_exempt' || extra.rpm_strategy === 'sticky_exempt'
    ? 'sticky_exempt'
    : 'tiered'
  rpmStickyBuffer.value = typeof account.rpm_sticky_buffer === 'number'
    ? account.rpm_sticky_buffer
    : typeof extra.rpm_sticky_buffer === 'number' ? extra.rpm_sticky_buffer : null
  userMsgQueueMode.value = typeof account.user_msg_queue_mode === 'string'
    ? account.user_msg_queue_mode
    : typeof extra.user_msg_queue_mode === 'string' ? extra.user_msg_queue_mode : ''
  tlsFingerprintEnabled.value = isAnthropicOAuth.value && (
    account.enable_tls_fingerprint === true || extra.enable_tls_fingerprint === true
  )
  tlsFingerprintProfileId.value = typeof account.tls_fingerprint_profile_id === 'number'
    ? account.tls_fingerprint_profile_id
    : typeof extra.tls_fingerprint_profile_id === 'number' ? extra.tls_fingerprint_profile_id : null
  sessionIdMaskingEnabled.value = isAnthropicOAuth.value && (
    account.session_id_masking_enabled === true || extra.session_id_masking_enabled === true
  )
  cacheTTLOverrideEnabled.value = isAnthropicOAuth.value && (
    account.cache_ttl_override_enabled === true || extra.cache_ttl_override_enabled === true
  )
  cacheTTLOverrideTarget.value = typeof account.cache_ttl_override_target === 'string'
    ? account.cache_ttl_override_target
    : typeof extra.cache_ttl_override_target === 'string' ? extra.cache_ttl_override_target : '5m'
  customBaseUrlEnabled.value = isAnthropicOAuth.value && (
    account.custom_base_url_enabled === true || extra.custom_base_url_enabled === true
  )
  customBaseUrl.value = typeof account.custom_base_url === 'string'
    ? account.custom_base_url
    : typeof extra.custom_base_url === 'string' ? extra.custom_base_url : ''

  // API-key quota controls are stored in account.extra.
  quotaLimit.value = typeof extra.quota_limit === 'number' && extra.quota_limit > 0 ? extra.quota_limit : null
  quotaDailyLimit.value = typeof extra.quota_daily_limit === 'number' && extra.quota_daily_limit > 0 ? extra.quota_daily_limit : null
  quotaWeeklyLimit.value = typeof extra.quota_weekly_limit === 'number' && extra.quota_weekly_limit > 0 ? extra.quota_weekly_limit : null
  quotaDailyResetMode.value = extra.quota_daily_reset_mode === 'fixed' ? 'fixed' : 'rolling'
  quotaDailyResetHour.value = typeof extra.quota_daily_reset_hour === 'number' ? extra.quota_daily_reset_hour : 0
  quotaWeeklyResetMode.value = extra.quota_weekly_reset_mode === 'fixed' ? 'fixed' : 'rolling'
  quotaWeeklyResetDay.value = typeof extra.quota_weekly_reset_day === 'number' ? extra.quota_weekly_reset_day : 1
  quotaWeeklyResetHour.value = typeof extra.quota_weekly_reset_hour === 'number' ? extra.quota_weekly_reset_hour : 0
  quotaResetTimezone.value = typeof extra.quota_reset_timezone === 'string' && extra.quota_reset_timezone
    ? extra.quota_reset_timezone
    : 'UTC'
  loadQuotaNotifyFromExtra(extra)
}

function normalizeEndpointCapabilities(value: unknown): OpenAIEndpointCapability[] {
  const rawValues = Array.isArray(value)
    ? value
    : asRecord(value).chat_completions === true || asRecord(value).embeddings === true
      ? Object.entries(asRecord(value)).filter(([, enabled]) => enabled === true).map(([key]) => key)
      : []
  const selected = rawValues.filter(
    (item): item is OpenAIEndpointCapability =>
      item === 'chat_completions' || item === 'embeddings'
  )
  return selected.length > 0 ? [...new Set(selected)] : ['chat_completions', 'embeddings']
}

function parseRetryStatusCodes(value: unknown): number[] {
  if (Array.isArray(value)) {
    return value
      .map(item => Number(item))
      .filter(item => Number.isInteger(item) && item >= 100 && item <= 599)
      .filter((item, index, values) => values.indexOf(item) === index)
      .sort((a, b) => a - b)
  }
  if (typeof value !== 'string') return []
  return value
    .split(/[\s,]+/)
    .map(item => Number(item))
    .filter(item => Number.isInteger(item) && item >= 100 && item <= 599)
    .filter((item, index, values) => values.indexOf(item) === index)
    .sort((a, b) => a - b)
}

function formatRetryStatusCodes(value: unknown): string {
  return parseRetryStatusCodes(value).join(', ')
}

function loadModelMapping(value: unknown) {
  const split = splitModelMappingObject(asRecord(value))
  allowedModels.value = split.allowedModels
  preservedModelMappings.value = split.modelMappings
  modelMappings.value = split.modelMappings.map(mapping => ({ ...mapping }))
  modelRestrictionMode.value = split.modelMappings.length > 0 && split.allowedModels.length === 0
    ? 'mapping'
    : 'whitelist'
}

function buildSelectedModelMapping() {
  const editableMappings = modelRestrictionMode.value === 'mapping'
    ? modelMappings.value
    : preservedModelMappings.value
  return buildModelMappingObject('combined', allowedModels.value, editableMappings)
}

function addModelMapping() {
  modelMappings.value.push({ from: '', to: '' })
}

function addPresetMapping(from: string, to: string) {
  if (modelMappings.value.some(mapping => mapping.from === from)) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  modelMappings.value.push({ from, to })
}

function removeModelMapping(index: number) {
  modelMappings.value.splice(index, 1)
}

function addOpenAICompactModelMapping() {
  openAICompactModelMappings.value.push({ from: '', to: '' })
}

function removeOpenAICompactModelMapping(index: number) {
  openAICompactModelMappings.value.splice(index, 1)
}

function toggleErrorCode(code: number) {
  if (selectedErrorCodes.value.includes(code)) {
    selectedErrorCodes.value = selectedErrorCodes.value.filter(value => value !== code)
  } else if (code >= 100 && code <= 599) {
    if (!confirmCustomErrorCode(code)) return
    selectedErrorCodes.value = [...selectedErrorCodes.value, code].sort((a, b) => a - b)
  }
}

function removeErrorCode(code: number) {
  selectedErrorCodes.value = selectedErrorCodes.value.filter(value => value !== code)
}

function addCustomErrorCode() {
  const code = Number(customErrorCodeInput.value)
  if (!Number.isInteger(code) || code < 100 || code > 599) {
    appStore.showError(t('admin.accounts.invalidErrorCode'))
    return
  }
  if (selectedErrorCodes.value.includes(code)) {
    appStore.showInfo(t('admin.accounts.errorCodeExists'))
    return
  }
  toggleErrorCode(code)
  customErrorCodeInput.value = null
}

function confirmCustomErrorCode(code: number): boolean {
  if (code === 429) return window.confirm(t('admin.accounts.customErrorCodes429Warning'))
  if (code === 529) return window.confirm(t('admin.accounts.customErrorCodes529Warning'))
  return true
}

function addTempUnschedRule(preset?: TempUnschedRuleForm) {
  tempUnschedRules.value.push(preset ? { ...preset } : {
    error_code: null,
    keywords: '',
    duration_minutes: 30,
    description: ''
  })
}

function removeTempUnschedRule(index: number) {
  tempUnschedRules.value.splice(index, 1)
}

function moveTempUnschedRule(index: number, direction: number) {
  const target = index + direction
  if (target < 0 || target >= tempUnschedRules.value.length) return
  const current = tempUnschedRules.value[index]
  tempUnschedRules.value[index] = tempUnschedRules.value[target]
  tempUnschedRules.value[target] = current
}

function parseTempUnschedRules(value: unknown): TempUnschedRuleForm[] {
  if (!Array.isArray(value)) return []
  return value.map(item => {
    const rule = asRecord(item)
    const keywords = Array.isArray(rule.keywords)
      ? rule.keywords.filter(item => typeof item === 'string').join(', ')
      : typeof rule.keywords === 'string' ? rule.keywords : ''
    return {
      error_code: typeof rule.error_code === 'number' ? rule.error_code : Number(rule.error_code) || null,
      keywords,
      duration_minutes: typeof rule.duration_minutes === 'number'
        ? rule.duration_minutes
        : Number(rule.duration_minutes) || null,
      description: typeof rule.description === 'string' ? rule.description : ''
    }
  })
}

function buildTempUnschedRules() {
  return tempUnschedRules.value
    .map(rule => ({
      error_code: Number(rule.error_code),
      keywords: rule.keywords.split(/[,;]/).map(value => value.trim()).filter(Boolean),
      duration_minutes: Number(rule.duration_minutes),
      description: rule.description.trim()
    }))
    .filter(rule =>
      Number.isInteger(rule.error_code) &&
      rule.error_code >= 100 &&
      rule.error_code <= 599 &&
      rule.keywords.length > 0 &&
      Number.isFinite(rule.duration_minutes) &&
      rule.duration_minutes > 0
    )
}

function toggleEndpointCapability(
  capability: OpenAIEndpointCapability,
  event: Event
) {
  const input = event.target as HTMLInputElement
  const checked = input.checked
  if (checked) {
    if (!openAIEndpointCapabilities.value.includes(capability)) {
      openAIEndpointCapabilities.value = [...openAIEndpointCapabilities.value, capability]
    }
    return
  }
  if (openAIEndpointCapabilities.value.length === 1) {
    input.checked = true
    return
  }
  openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(
    value => value !== capability
  )
  if (!openAIEndpointCapabilities.value.includes('chat_completions')) {
    openAIResponsesMode.value = 'auto'
  }
}

function handleUpstreamBillingProbeChange(enabled: boolean) {
  upstreamBillingProbeEnabled.value = enabled
  if (!enabled) upstreamBillingRateSyncEnabled.value = false
}

function handleUpstreamBillingRateSyncChange(enabled: boolean) {
  upstreamBillingRateSyncEnabled.value = enabled
  if (enabled) upstreamBillingProbeEnabled.value = true
}

function applyOptionalPercent(
  target: Record<string, unknown>,
  key: string,
  value: number | null
) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    target[key] = Math.max(0, Math.min(100, value)) / 100
  } else {
    delete target[key]
  }
}

function applyCommonCredentialSettings(credentials: Record<string, unknown>): boolean {
  if (poolModeEnabled.value) {
    credentials.pool_mode = true
    credentials.pool_mode_retry_count = Math.max(0, Math.min(10, Math.trunc(Number(poolModeRetryCount.value) || 0)))
    const statusCodes = parseRetryStatusCodes(poolModeRetryStatusCodesInput.value)
    if (statusCodes.length > 0) credentials.pool_mode_retry_status_codes = statusCodes
    else delete credentials.pool_mode_retry_status_codes
  } else {
    delete credentials.pool_mode
    delete credentials.pool_mode_retry_count
    delete credentials.pool_mode_retry_status_codes
  }

  if (customErrorCodesEnabled.value) {
    credentials.custom_error_codes_enabled = true
    credentials.custom_error_codes = [...selectedErrorCodes.value]
  } else {
    delete credentials.custom_error_codes_enabled
    delete credentials.custom_error_codes
  }

  applyInterceptWarmup(credentials, interceptWarmupRequests.value, 'edit')
  if (tempUnschedEnabled.value) {
    const rules = buildTempUnschedRules()
    if (rules.length > 0) {
      credentials.temp_unschedulable_enabled = true
      credentials.temp_unschedulable_rules = rules
    } else {
      appStore.showError(t('admin.accounts.tempUnschedulable.rulesInvalid', 'At least one valid rule is required'))
      return false
    }
  } else {
    delete credentials.temp_unschedulable_enabled
    delete credentials.temp_unschedulable_rules
  }
  return true
}

function applyAnthropicQuotaExtra(extra: Record<string, unknown>) {
  if (!isAnthropicOAuth.value) return

  if (windowCostEnabled.value && (windowCostLimit.value ?? 0) > 0) {
    extra.window_cost_limit = windowCostLimit.value
    extra.window_cost_sticky_reserve = windowCostStickyReserve.value ?? 10
  } else {
    delete extra.window_cost_limit
    delete extra.window_cost_sticky_reserve
  }

  if (sessionLimitEnabled.value && (maxSessions.value ?? 0) > 0) {
    extra.max_sessions = maxSessions.value
    extra.session_idle_timeout_minutes = sessionIdleTimeout.value ?? 5
  } else {
    delete extra.max_sessions
    delete extra.session_idle_timeout_minutes
  }

  if (rpmLimitEnabled.value) {
    extra.base_rpm = (baseRpm.value ?? 0) > 0 ? baseRpm.value : 15
    extra.rpm_strategy = rpmStrategy.value
    if ((rpmStickyBuffer.value ?? 0) > 0) extra.rpm_sticky_buffer = rpmStickyBuffer.value
    else delete extra.rpm_sticky_buffer
  } else {
    delete extra.base_rpm
    delete extra.rpm_strategy
    delete extra.rpm_sticky_buffer
  }

  if (userMsgQueueMode.value) extra.user_msg_queue_mode = userMsgQueueMode.value
  else delete extra.user_msg_queue_mode
  delete extra.user_msg_queue_enabled

  if (tlsFingerprintEnabled.value) {
    extra.enable_tls_fingerprint = true
    if (tlsFingerprintProfileId.value != null && tlsFingerprintProfileId.value !== 0) extra.tls_fingerprint_profile_id = tlsFingerprintProfileId.value
    else delete extra.tls_fingerprint_profile_id
  } else {
    delete extra.enable_tls_fingerprint
    delete extra.tls_fingerprint_profile_id
  }

  if (sessionIdMaskingEnabled.value) extra.session_id_masking_enabled = true
  else delete extra.session_id_masking_enabled

  if (cacheTTLOverrideEnabled.value) {
    extra.cache_ttl_override_enabled = true
    extra.cache_ttl_override_target = cacheTTLOverrideTarget.value || '5m'
  } else {
    delete extra.cache_ttl_override_enabled
    delete extra.cache_ttl_override_target
  }

  if (customBaseUrlEnabled.value && customBaseUrl.value.trim()) {
    extra.custom_base_url_enabled = true
    extra.custom_base_url = customBaseUrl.value.trim()
  } else {
    delete extra.custom_base_url_enabled
    delete extra.custom_base_url
  }
}

function applyApiKeyQuotaExtra(extra: Record<string, unknown>) {
  if (quotaLimit.value != null && quotaLimit.value > 0) extra.quota_limit = quotaLimit.value
  else delete extra.quota_limit
  if (quotaDailyLimit.value != null && quotaDailyLimit.value > 0) extra.quota_daily_limit = quotaDailyLimit.value
  else {
    delete extra.quota_daily_limit
    delete extra.quota_daily_used
    delete extra.quota_daily_start
  }
  if (quotaWeeklyLimit.value != null && quotaWeeklyLimit.value > 0) extra.quota_weekly_limit = quotaWeeklyLimit.value
  else {
    delete extra.quota_weekly_limit
    delete extra.quota_weekly_used
    delete extra.quota_weekly_start
  }
  if (quotaDailyResetMode.value === 'fixed') {
    extra.quota_daily_reset_mode = 'fixed'
    extra.quota_daily_reset_hour = Math.max(0, Math.min(23, Math.trunc(quotaDailyResetHour.value || 0)))
  } else {
    delete extra.quota_daily_reset_mode
    delete extra.quota_daily_reset_hour
  }
  if (quotaWeeklyResetMode.value === 'fixed') {
    extra.quota_weekly_reset_mode = 'fixed'
    extra.quota_weekly_reset_day = Math.max(0, Math.min(6, Math.trunc(quotaWeeklyResetDay.value || 0)))
    extra.quota_weekly_reset_hour = Math.max(0, Math.min(23, Math.trunc(quotaWeeklyResetHour.value || 0)))
  } else {
    delete extra.quota_weekly_reset_mode
    delete extra.quota_weekly_reset_day
    delete extra.quota_weekly_reset_hour
  }
  if (quotaDailyResetMode.value === 'fixed' || quotaWeeklyResetMode.value === 'fixed') {
    extra.quota_reset_timezone = quotaResetTimezone.value?.trim() || 'UTC'
  } else {
    delete extra.quota_reset_timezone
  }
}

async function handleSubmit() {
  const account = props.account
  if (!account || !supported.value || !name.value.trim()) return
  if (status.value !== 'active' && status.value !== 'inactive' && status.value !== 'error') {
    appStore.showError(t('admin.accounts.pleaseSelectStatus'))
    return
  }
  if (
    account.platform === 'openai' &&
    account.type === 'oauth' &&
    !isSparkShadow.value &&
    autoResetCreditEnabled.value
  ) {
    const thresholds = [autoResetCredit5hThreshold.value, autoResetCredit7dThreshold.value]
    if (thresholds.some(value => !Number.isFinite(value) || value < 0.1 || value > 100)) {
      appStore.showError(t('admin.accounts.autoResetCredit.thresholdInvalid'))
      return
    }
  }
  let credentials: Record<string, unknown>
  const currentCredentials = asRecord(account.credentials)
  const mapping = buildSelectedModelMapping()

  if (isSparkShadow.value) {
    credentials = {}
    if (mapping) credentials.model_mapping = mapping
    const compact = buildModelMappingObject('mapping', [], openAICompactModelMappings.value) || compactModelMapping.value
    if (compact && Object.keys(compact).length > 0) {
      credentials.compact_model_mapping = { ...compact }
    }
  } else {
    credentials = { ...currentCredentials }
    // Passthrough deliberately leaves an existing mapping intact (the
    // upstream controls model names); otherwise apply the edited mapping.
    if (!(isOpenAI.value && openaiPassthroughEnabled.value)) {
      if (mapping) credentials.model_mapping = mapping
      else delete credentials.model_mapping
    }

    if (isApiKey.value) {
      const existingKey = typeof currentCredentials.api_key === 'string' &&
        currentCredentials.api_key.trim().length > 0
      const statusHasKey = account.credentials_status?.has_api_key === true
      if (!newApiKey.value.trim() && !existingKey && !statusHasKey) {
        appStore.showError(t('admin.accounts.apiKeyIsRequired'))
        return
      }
      if (newApiKey.value.trim()) credentials.api_key = newApiKey.value.trim()
      credentials.base_url = baseUrl.value.trim() || (
        account.platform === 'openai' ? 'https://api.openai.com' : 'https://api.anthropic.com'
      )
      if (headerOverrideEnabled.value) {
        const headerError = validateHeaderOverrideRows(headerOverrideRows.value)
        if (headerError) {
          appStore.showError(t(`admin.accounts.headerOverride.${headerError}`))
          return
        }
      }
      applyHeaderOverride(
        credentials,
        headerOverrideEnabled.value,
        headerOverrideRows.value,
        'edit'
      )
    }

    if (isBedrock.value) {
      const credentialStatus = asRecord(account.credentials_status)
      const hasCredential = (key: string) =>
        credentialStatus[`has_${key}`] === true ||
        (typeof currentCredentials[key] === 'string' && String(currentCredentials[key]).trim().length > 0)

      credentials.auth_mode = bedrockAuthMode.value
      credentials.aws_region = bedrockRegion.value.trim() || 'us-east-1'
      if (bedrockForceGlobal.value) credentials.aws_force_global = 'true'
      else delete credentials.aws_force_global

      if (bedrockAuthMode.value === 'sigv4') {
        if (!bedrockAccessKeyId.value.trim() && !hasCredential('aws_access_key_id')) {
          appStore.showError(t('admin.accounts.bedrockAccessKeyIdRequired'))
          return
        }
        if (!bedrockSecretAccessKey.value.trim() && !hasCredential('aws_secret_access_key')) {
          appStore.showError(t('admin.accounts.bedrockSecretAccessKeyRequired'))
          return
        }
        if (bedrockAccessKeyId.value.trim()) {
          credentials.aws_access_key_id = bedrockAccessKeyId.value.trim()
        }
        if (bedrockSecretAccessKey.value.trim()) {
          credentials.aws_secret_access_key = bedrockSecretAccessKey.value.trim()
        }
        if (bedrockSessionToken.value.trim()) {
          credentials.aws_session_token = bedrockSessionToken.value.trim()
        }
        // Do not leave an inactive auth mode's key alongside the selected mode.
        delete credentials.api_key
      } else {
        if (!bedrockApiKey.value.trim() && !hasCredential('api_key')) {
          appStore.showError(t('admin.accounts.bedrockApiKeyRequired'))
          return
        }
        if (bedrockApiKey.value.trim()) credentials.api_key = bedrockApiKey.value.trim()
        delete credentials.aws_access_key_id
        delete credentials.aws_secret_access_key
        delete credentials.aws_session_token
      }
    }

    if (isServiceAccount.value) {
      if (!vertexProjectId.value.trim()) {
        appStore.showError(t('admin.accounts.vertexSaJsonMissingProjectId'))
        return
      }
      if (typeof currentCredentials.client_email !== 'string' || !currentCredentials.client_email.trim()) {
        appStore.showError(t('admin.accounts.vertexSaJsonMissingClientEmail'))
        return
      }
      if (!vertexLocation.value.trim()) {
        appStore.showError(t('admin.accounts.vertexLocationRequired'))
        return
      }
      const existingJson = typeof currentCredentials.service_account_json === 'string' &&
        currentCredentials.service_account_json.trim().length > 0
      const statusHasJson = account.credentials_status?.has_service_account_json === true ||
        account.credentials_status?.has_service_account === true
      const inputJson = serviceAccountJson.value.trim()
      if (!inputJson && !existingJson && !statusHasJson) {
        appStore.showError(t('admin.accounts.vertexSaJsonRequired'))
        return
      }
      if (inputJson) {
        try {
          const parsed = JSON.parse(inputJson) as Record<string, unknown>
          const projectId = typeof parsed.project_id === 'string'
            ? parsed.project_id.trim()
            : typeof currentCredentials.project_id === 'string'
              ? currentCredentials.project_id.trim()
              : ''
          const clientEmail = typeof parsed.client_email === 'string'
            ? parsed.client_email.trim()
            : typeof currentCredentials.client_email === 'string'
              ? currentCredentials.client_email.trim()
              : ''
          const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
          if (!projectId || !clientEmail || !privateKey) throw new Error('missing fields')
          credentials.service_account_json = JSON.stringify(parsed)
          credentials.project_id = projectId
          credentials.client_email = clientEmail
        } catch {
          appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
          return
        }
      }
      credentials.project_id = vertexProjectId.value.trim()
      credentials.client_email = currentCredentials.client_email.trim()
      credentials.location = vertexLocation.value
      credentials.tier_id = 'vertex'
    }

    if (isOpenAI.value && isApiKey.value) {
      credentials.openai_capabilities = [...openAIEndpointCapabilities.value]
    }

    if (isOpenAI.value) {
      const compact = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
      if (compact) credentials.compact_model_mapping = compact
      else delete credentials.compact_model_mapping
      if (account.type === 'oauth' && !isSparkShadow.value) {
        applyPlanType(credentials, openAIPlanType.value)
      }
    }

    if (!applyCommonCredentialSettings(credentials)) return
    applyAccountSchedulingThresholdOverride(credentials, currentCredentials)
  }

  const extra = { ...asRecord(account.extra) }
  // Runtime state belongs to the quota-reset service and must never be
  // written back from an account edit request.
  delete extra.codex_auto_reset_credit_state
  if (isOpenAI.value) {
    if (isSparkShadow.value) {
      delete extra.openai_long_context_billing_enabled
    } else {
      extra.openai_long_context_billing_enabled = openAILongContextBillingEnabled.value
      if (account.type === 'oauth' && openAIFlattenNamespaces.value) {
        extra.openai_responses_flatten_namespaces = true
      } else {
        delete extra.openai_responses_flatten_namespaces
      }
      if (openaiPassthroughEnabled.value) {
        extra.openai_passthrough = true
      } else {
        delete extra.openai_passthrough
        delete extra.openai_oauth_passthrough
      }
      if (openAICompactMode.value === 'auto') delete extra.openai_compact_mode
      else extra.openai_compact_mode = openAICompactMode.value
      const wsPrefix = account.type === 'apikey' ? 'openai_apikey' : 'openai_oauth'
      extra[wsPrefix + '_responses_websockets_v2_mode'] = openAIWSMode.value
      extra[wsPrefix + '_responses_websockets_v2_enabled'] = openAIWSMode.value !== 'off'
      if (
        isApiKey.value &&
        openAIEndpointCapabilities.value.includes('chat_completions') &&
        openAIResponsesMode.value !== 'auto'
      ) {
        extra.openai_responses_mode = openAIResponsesMode.value
      } else {
        delete extra.openai_responses_mode
      }
      if (account.type === 'oauth' || account.type === 'setup-token') {
        if (openAICodexCLIOnlyEnabled.value) {
          extra.codex_cli_only = true
          if (openAICodexCLIOnlyAppServerEnabled.value) extra.codex_cli_only_allow_app_server = true
          else delete extra.codex_cli_only_allow_app_server
        } else {
          // An explicit false clears old values on installations where JSONB
          // merge semantics would otherwise preserve the old flag.
          if (extra.codex_cli_only === true) extra.codex_cli_only = false
          else delete extra.codex_cli_only
          delete extra.codex_cli_only_allow_app_server
        }
      }
      if (account.type === 'oauth') {
        if (codexFingerprintMode.value === 'off') delete extra.codex_fingerprint_mode
        else extra.codex_fingerprint_mode = codexFingerprintMode.value
      }
      delete extra.codex_image_generation_bridge_enabled
      if (codexImageToolMode.value === 'block') {
        extra.codex_image_generation_explicit_tool_policy = 'strip'
        delete extra.codex_image_generation_bridge
      } else {
        delete extra.codex_image_generation_explicit_tool_policy
        if (codexImageToolMode.value === 'enabled') {
          extra.codex_image_generation_bridge = true
        } else if (codexImageToolMode.value === 'disabled') {
          extra.codex_image_generation_bridge = false
        } else {
          delete extra.codex_image_generation_bridge
        }
      }
      applyOptionalPercent(extra, 'auto_pause_5h_threshold', autoPause5hThresholdPercent.value)
      applyOptionalPercent(extra, 'auto_pause_7d_threshold', autoPause7dThresholdPercent.value)
      if (autoPause5hDisabled.value) extra.auto_pause_5h_disabled = true
      else delete extra.auto_pause_5h_disabled
      if (autoPause7dDisabled.value) extra.auto_pause_7d_disabled = true
      else delete extra.auto_pause_7d_disabled
      if (account.type === 'oauth' && !isSparkShadow.value) {
        extra.auto_reset_credit_enabled = autoResetCreditEnabled.value
        extra.auto_reset_credit_5h_threshold = autoResetCredit5hThreshold.value / 100
        extra.auto_reset_credit_7d_threshold = autoResetCredit7dThreshold.value / 100
      }
    }
  }

  if (isAnthropicApiKey.value) {
    if (anthropicPassthroughEnabled.value) extra.anthropic_passthrough = true
    else delete extra.anthropic_passthrough
    if (anthropicAPIKeyAuthScheme.value === 'authorization_bearer') {
      extra.anthropic_apikey_auth_scheme = 'authorization_bearer'
    } else {
      delete extra.anthropic_apikey_auth_scheme
    }
    if (webSearchEmulationMode.value === 'default') delete extra.web_search_emulation
    else extra.web_search_emulation = webSearchEmulationMode.value
  }
  applyAnthropicQuotaExtra(extra)
  if (isApiKey.value || isBedrock.value) {
    applyApiKeyQuotaExtra(extra)
    writeQuotaNotifyToExtra(extra, 'update')
  }
  delete extra.upstream_billing_probe_enabled
  delete extra.upstream_billing_rate_sync_enabled

  const updates: UpdateAccountRequest = {
    name: name.value.trim(),
    notes: notes.value.trim() || null,
    status: status.value,
    credentials,
    extra,
    // UpdateAccount treats nil as "leave unchanged" and 0 as "clear proxy".
    proxy_id: proxyId.value ?? 0,
    concurrency: concurrency.value,
    load_factor: loadFactor.value ?? 0,
    priority: priority.value,
    group_ids: groupIds.value,
    expires_at: expiresAt.value ?? 0,
    auto_pause_on_expired: autoPauseOnExpired.value
  }
  if (isApiKey.value) {
    updates.upstream_billing_probe_enabled = upstreamBillingProbeEnabled.value
    updates.upstream_billing_rate_sync_enabled = upstreamBillingRateSyncEnabled.value
  }
  if (!upstreamBillingRateSyncEnabled.value) {
    updates.rate_multiplier = rateMultiplier.value
  }

  submitting.value = true
  try {
    const updated = await adminAPI.accounts.update(account.id, updates)
    appStore.showSuccess(t('admin.accounts.accountUpdated'))
    emit('updated', updated)
    handleClose()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message ||
      error.response?.data?.detail ||
      t('admin.accounts.failedToUpdate')
    )
  } finally {
    submitting.value = false
  }
}

function handleClose() {
  emit('close')
}
</script>
