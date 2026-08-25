<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.createAccount')"
    width="wide"
    @close="handleClose"
  >
    <!-- Step Indicator for OAuth accounts -->
    <div v-if="isOAuthFlow" class="mb-6 flex items-center justify-center" data-testid="create-account-step-indicator">
      <div class="flex items-center space-x-4">
        <div class="flex items-center">
          <div
            :class="[
              'flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold',
              step >= 1 ? 'bg-primary-500 text-white' : 'bg-gray-200 text-gray-500 dark:bg-dark-600'
            ]"
          >
            1
          </div>
          <span class="ml-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{
            t('admin.accounts.oauth.authMethod')
          }}</span>
        </div>
        <div class="h-0.5 w-8 bg-gray-300 dark:bg-dark-600" />
        <div class="flex items-center">
          <div
            :class="[
              'flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold',
              step >= 2 ? 'bg-primary-500 text-white' : 'bg-gray-200 text-gray-500 dark:bg-dark-600'
            ]"
          >
            2
          </div>
          <span class="ml-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{
            oauthStepTitle
          }}</span>
        </div>
      </div>
    </div>

    <!-- Step 1: Basic Info -->
    <form
      v-if="step === 1"
      id="create-account-form"
      @submit.prevent="handleSubmit"
      class="space-y-5"
    >
      <div>
        <label class="input-label">{{ t('admin.accounts.accountName') }}</label>
        <input
          v-model="form.name"
          type="text"
          required
          class="input"
          :placeholder="t('admin.accounts.enterAccountName')"
          data-tour="account-form-name"
        />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.notes') }}</label>
        <textarea
          v-model="form.notes"
          rows="3"
          class="input"
          :placeholder="t('admin.accounts.notesPlaceholder')"
        ></textarea>
        <p class="input-hint">{{ t('admin.accounts.notesHint') }}</p>
      </div>

      <!-- Platform Selection - Segmented Control Style -->
      <div>
        <label class="input-label">{{ t('admin.accounts.platform') }}</label>
        <div class="mt-2 flex flex-wrap rounded-lg bg-gray-100 p-1 dark:bg-dark-700" data-tour="account-form-platform">
          <button
            type="button"
            data-testid="create-platform-anthropic"
            @click="selectPlatform('anthropic')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'anthropic'
                ? 'bg-white text-orange-600 shadow-sm dark:bg-dark-600 dark:text-orange-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <Icon name="sparkles" size="sm" />
            Anthropic
          </button>
          <button
            type="button"
            data-testid="create-platform-openai"
            @click="selectPlatform('openai')"
            :class="[
              'flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all',
              form.platform === 'openai'
                ? 'bg-white text-green-600 shadow-sm dark:bg-dark-600 dark:text-green-400'
                : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'
            ]"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"
              />
            </svg>
            OpenAI
          </button>
        </div>
      </div>

      <!-- Account Type Selection (Anthropic) -->
      <div v-if="form.platform === 'anthropic'">
        <label class="input-label">{{ t('admin.accounts.accountType') }}</label>
        <div class="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-4" data-tour="account-form-type">
          <button
            type="button"
            data-testid="create-account-type-oauth"
            @click="accountCategory = 'oauth'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              isOAuthFlow
                ? 'border-orange-500 bg-orange-50 dark:bg-orange-900/20'
                : 'border-gray-200 hover:border-orange-300 dark:border-dark-600 dark:hover:border-orange-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                isOAuthFlow
                  ? 'bg-orange-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="sparkles" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{
                t('admin.accounts.claudeCode')
              }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{
                t('admin.accounts.oauthSetupToken')
              }}</span>
            </div>
          </button>

          <button
            type="button"
            data-testid="create-account-type-apikey"
            @click="accountCategory = 'apikey'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              accountCategory === 'apikey'
                ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                : 'border-gray-200 hover:border-purple-300 dark:border-dark-600 dark:hover:border-purple-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                accountCategory === 'apikey'
                  ? 'bg-purple-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="key" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{
                t('admin.accounts.claudeConsole')
              }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{
                t('admin.accounts.apiKey')
              }}</span>
            </div>
          </button>

          <button
            type="button"
            data-testid="create-account-type-bedrock"
            @click="accountCategory = 'bedrock'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              accountCategory === 'bedrock'
                ? 'border-amber-500 bg-amber-50 dark:bg-amber-900/20'
                : 'border-gray-200 hover:border-amber-300 dark:border-dark-600 dark:hover:border-amber-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                accountCategory === 'bedrock'
                  ? 'bg-amber-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="cloud" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{
                t('admin.accounts.bedrockLabel')
              }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{
                t('admin.accounts.bedrockDesc')
              }}</span>
            </div>
          </button>

          <button
            type="button"
            data-testid="create-account-type-service-account"
            @click="accountCategory = 'service_account'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              accountCategory === 'service_account'
                ? 'border-sky-500 bg-sky-50 dark:bg-sky-900/20'
                : 'border-gray-200 hover:border-sky-300 dark:border-dark-600 dark:hover:border-sky-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                accountCategory === 'service_account'
                  ? 'bg-sky-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="cloud" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">Vertex</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">Service Account</span>
            </div>
          </button>

        </div>

        <div
          v-if="accountCategory === 'service_account'"
          class="mt-3 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-800 dark:border-sky-800/40 dark:bg-sky-900/20 dark:text-sky-200"
        >
          <p>{{ t('admin.accounts.vertexAnthropicHint') }}</p>
        </div>
      </div>

      <!-- Account Type Selection (OpenAI) -->
      <div v-if="form.platform === 'openai'">
        <label class="input-label">{{ t('admin.accounts.accountType') }}</label>
        <div class="mt-2 grid grid-cols-2 gap-3" data-tour="account-form-type">
          <button
            type="button"
            data-testid="create-openai-account-type-oauth"
            @click="accountCategory = 'oauth'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              isOAuthFlow
                ? 'border-green-500 bg-green-50 dark:bg-green-900/20'
                : 'border-gray-200 hover:border-green-300 dark:border-dark-600 dark:hover:border-green-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                isOAuthFlow
                  ? 'bg-green-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="key" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">OAuth</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.types.chatgptOauth') }}</span>
            </div>
          </button>

          <button
            type="button"
            data-testid="create-openai-account-type-apikey"
            @click="accountCategory = 'apikey'"
            :class="[
              'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all',
              accountCategory === 'apikey'
                ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                : 'border-gray-200 hover:border-purple-300 dark:border-dark-600 dark:hover:border-purple-700'
            ]"
          >
            <div
              :class="[
                'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                accountCategory === 'apikey'
                  ? 'bg-purple-500 text-white'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
              ]"
            >
              <Icon name="key" size="sm" />
            </div>
            <div>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">API Key</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.types.responsesApi') }}</span>
            </div>
          </button>

        </div>
      </div>

      <!-- Vertex Service Account -->
      <div v-if="form.platform === 'anthropic' && accountCategory === 'service_account'" class="space-y-4" data-testid="create-vertex-section">
        <div>
          <label class="input-label">Service Account JSON</label>
          <input
            ref="vertexServiceAccountFileInput"
            type="file"
            accept="application/json,.json"
            class="hidden"
            data-testid="create-vertex-service-account-file"
            @change="handleVertexServiceAccountFile"
          />
          <div
            :class="[
              'rounded-lg border-2 border-dashed px-4 py-5 transition-colors',
              vertexServiceAccountDragActive
                ? 'border-sky-500 bg-sky-50 dark:border-sky-500 dark:bg-sky-900/20'
                : 'border-gray-300 bg-gray-50 hover:border-sky-400 hover:bg-sky-50/60 dark:border-dark-500 dark:bg-dark-700/40 dark:hover:border-sky-600 dark:hover:bg-sky-900/10'
            ]"
            data-testid="create-vertex-service-account-dropzone"
            @dragenter.prevent="vertexServiceAccountDragActive = true"
            @dragover.prevent="vertexServiceAccountDragActive = true"
            @dragleave.prevent="vertexServiceAccountDragActive = false"
            @drop.prevent="handleVertexServiceAccountDrop"
          >
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="min-w-0">
                <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                  <Icon name="upload" size="sm" />
                  <span>{{ vertexClientEmail ? t('admin.accounts.vertexSaJsonLoaded') : t('admin.accounts.vertexSaJsonDrop') }}</span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ vertexClientEmail ? t('admin.accounts.vertexSaJsonKeyHidden') : t('admin.accounts.vertexSaJsonDropHint') }}
                </p>
              </div>
              <button
                type="button"
                class="btn btn-secondary shrink-0"
                data-testid="create-vertex-service-account-select"
                @click="vertexServiceAccountFileInput?.click()"
              >
                <Icon name="upload" size="sm" />
                {{ t('admin.accounts.vertexSaJsonSelectBtn') }}
              </button>
            </div>
            <div
              v-if="vertexClientEmail"
              class="mt-3 rounded-md border border-sky-200 bg-white px-3 py-2 text-xs text-sky-900 dark:border-sky-800/50 dark:bg-dark-800 dark:text-sky-200"
              data-testid="create-vertex-service-account-preview"
            >
              <div class="truncate">Project ID: <span class="font-mono">{{ vertexProjectId }}</span></div>
              <div class="truncate">Client Email: <span class="font-mono">{{ vertexClientEmail }}</span></div>
            </div>
          </div>
          <p class="input-hint">{{ t('admin.accounts.vertexSaJsonUploadHint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">Project ID</label>
            <input
              :value="vertexProjectId"
              type="text"
              class="input font-mono"
              readonly
              :placeholder="t('admin.accounts.vertexProjectIdPlaceholder')"
            />
          </div>
          <div>
            <label class="input-label">Location</label>
            <select
              v-model="vertexLocation"
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
      </div>

      <!-- Add Method (only for Anthropic OAuth-based type) -->
      <div v-if="form.platform === 'anthropic' && isOAuthFlow">
        <label class="input-label">{{ t('admin.accounts.addMethod') }}</label>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="oauth"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.types.oauth') }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token"
              class="mr-2 text-primary-600 focus:ring-primary-500"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{
              t('admin.accounts.setupTokenLongLived')
            }}</span>
          </label>
        </div>
      </div>

      <!-- API Key input -->
      <div v-if="accountCategory === 'apikey'" class="space-y-4" data-testid="create-api-key-section">
        <div>
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input
            v-model="apiKeyBaseUrl"
            type="text"
            class="input"
            :placeholder="apiKeyBaseUrlPlaceholder"
          />
          <p v-if="baseUrlHint" class="input-hint">{{ baseUrlHint }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKeyRequired') }}</label>
          <input
            v-model="apiKeyValue"
            type="password"
            required
            class="input font-mono"
            :placeholder="apiKeyValuePlaceholder"
          />
          <p v-if="apiKeyHint" class="input-hint">{{ apiKeyHint }}</p>
        </div>

        <div
          class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600"
        >
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
            </p>
          </div>
          <Toggle
            v-model="upstreamBillingProbeEnabled"
            data-testid="upstream-billing-auto-probe"
            :aria-label="t('admin.accounts.upstreamBilling.autoProbe')"
          />
        </div>

        <!-- Model Restriction Section -->
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
                data-testid="create-model-restriction-whitelist"
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
                data-testid="create-model-restriction-mapping"
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
              <ModelWhitelistSelector v-model="allowedModels" :platform="form.platform" :sync-credentials="syncPreviewCredentials" />
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
                <span v-if="allowedModels.length === 0">{{
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
                  :data-testid="`create-model-preset-${preset.from}`"
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

        <!-- Header Override Section (eligible API-key platforms) -->
        <div
          v-if="headerOverrideCapable"
          class="border-t border-gray-200 pt-4 dark:border-dark-600"
        >
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

      </div>

      <!-- Bedrock credentials (only for Anthropic Bedrock type) -->
      <div v-if="form.platform === 'anthropic' && accountCategory === 'bedrock'" class="space-y-4" data-testid="create-bedrock-section">
        <!-- Auth Mode Radio -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockAuthMode') }}</label>
          <div class="mt-2 flex gap-4">
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="sigv4"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeSigv4') }}</span>
            </label>
            <label class="flex cursor-pointer items-center">
              <input
                v-model="bedrockAuthMode"
                type="radio"
                value="apikey"
                class="mr-2 text-primary-600 focus:ring-primary-500"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockAuthModeApikey') }}</span>
            </label>
          </div>
        </div>

        <!-- SigV4 fields -->
        <template v-if="bedrockAuthMode === 'sigv4'">
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockAccessKeyId') }}</label>
            <input
              v-model="bedrockAccessKeyId"
              type="text"
              required
              class="input font-mono"
              placeholder="AKIA..."
              data-testid="create-bedrock-access-key-id"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSecretAccessKey') }}</label>
            <input
              v-model="bedrockSecretAccessKey"
              type="password"
              required
              class="input font-mono"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.bedrockSessionToken') }}</label>
            <input
              v-model="bedrockSessionToken"
              type="password"
              class="input font-mono"
            />
            <p class="input-hint">{{ t('admin.accounts.bedrockSessionTokenHint') }}</p>
          </div>
        </template>

        <!-- API Key field -->
        <div v-if="bedrockAuthMode === 'apikey'">
          <label class="input-label">{{ t('admin.accounts.bedrockApiKeyInput') }}</label>
          <input
            v-model="bedrockApiKey"
            type="password"
            required
            class="input font-mono"
          />
        </div>

        <!-- Shared: Region -->
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockRegion') }}</label>
          <select v-model="bedrockRegion" class="input">
            <optgroup label="US">
              <option value="us-east-1">us-east-1 (N. Virginia)</option>
              <option value="us-east-2">us-east-2 (Ohio)</option>
              <option value="us-west-1">us-west-1 (N. California)</option>
              <option value="us-west-2">us-west-2 (Oregon)</option>
              <option value="us-gov-east-1">us-gov-east-1 (GovCloud US-East)</option>
              <option value="us-gov-west-1">us-gov-west-1 (GovCloud US-West)</option>
            </optgroup>
            <optgroup label="Europe">
              <option value="eu-west-1">eu-west-1 (Ireland)</option>
              <option value="eu-west-2">eu-west-2 (London)</option>
              <option value="eu-west-3">eu-west-3 (Paris)</option>
              <option value="eu-central-1">eu-central-1 (Frankfurt)</option>
              <option value="eu-central-2">eu-central-2 (Zurich)</option>
              <option value="eu-south-1">eu-south-1 (Milan)</option>
              <option value="eu-south-2">eu-south-2 (Spain)</option>
              <option value="eu-north-1">eu-north-1 (Stockholm)</option>
            </optgroup>
            <optgroup label="Asia Pacific">
              <option value="ap-northeast-1">ap-northeast-1 (Tokyo)</option>
              <option value="ap-northeast-2">ap-northeast-2 (Seoul)</option>
              <option value="ap-northeast-3">ap-northeast-3 (Osaka)</option>
              <option value="ap-south-1">ap-south-1 (Mumbai)</option>
              <option value="ap-south-2">ap-south-2 (Hyderabad)</option>
              <option value="ap-southeast-1">ap-southeast-1 (Singapore)</option>
              <option value="ap-southeast-2">ap-southeast-2 (Sydney)</option>
            </optgroup>
            <optgroup label="Canada">
              <option value="ca-central-1">ca-central-1 (Canada)</option>
            </optgroup>
            <optgroup label="South America">
              <option value="sa-east-1">sa-east-1 (São Paulo)</option>
            </optgroup>
          </select>
          <p class="input-hint">{{ t('admin.accounts.bedrockRegionHint') }}</p>
        </div>

        <!-- Shared: Force Global -->
        <div>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              v-model="bedrockForceGlobal"
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
              data-testid="create-bedrock-force-global"
            />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.bedrockForceGlobal') }}</span>
          </label>
          <p class="input-hint mt-1">{{ t('admin.accounts.bedrockForceGlobalHint') }}</p>
        </div>

        <!-- Model Restriction Section for Bedrock -->
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
            <ModelWhitelistSelector v-model="allowedModels" platform="anthropic" :sync-credentials="syncPreviewCredentials" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0">{{ t('admin.accounts.supportsAllModels') }}</span>
            </p>
          </div>

          <!-- Mapping Mode -->
          <div v-else class="space-y-3">
            <div v-for="(mapping, index) in modelMappings" :key="index" class="flex items-center gap-2">
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
                v-for="preset in bedrockPresets"
                :key="preset.from"
                type="button"
                @click="addPresetMapping(preset.from, preset.to)"
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

      <!-- 配额控制 (Anthropic apikey/bedrock: 配额限制 + 亲和) -->
      <div
        v-if="form.platform === 'anthropic' && (accountCategory === 'apikey' || accountCategory === 'bedrock')"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaControl.hint') }}
          </p>
        </div>
        <QuotaLimitCard
          :totalLimit="quotaLimit"
          :dailyLimit="quotaDailyLimit"
          :weeklyLimit="quotaWeeklyLimit"
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
          :dailyResetMode="quotaDailyResetMode"
          :dailyResetHour="quotaDailyResetHour"
          :weeklyResetMode="quotaWeeklyResetMode"
          :weeklyResetDay="quotaWeeklyResetDay"
          :weeklyResetHour="quotaWeeklyResetHour"
          :resetTimezone="quotaResetTimezone"
          @update:totalLimit="quotaLimit = $event"
          @update:dailyLimit="quotaDailyLimit = $event"
          @update:weeklyLimit="quotaWeeklyLimit = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
          @update:dailyResetMode="quotaDailyResetMode = $event"
          @update:dailyResetHour="quotaDailyResetHour = $event"
          @update:weeklyResetMode="quotaWeeklyResetMode = $event"
          @update:weeklyResetDay="quotaWeeklyResetDay = $event"
          @update:weeklyResetHour="quotaWeeklyResetHour = $event"
          @update:resetTimezone="quotaResetTimezone = $event"
        />
      </div>

      <!-- 配额控制 (非 Anthropic apikey/bedrock) -->
      <div
        v-else-if="accountCategory === 'apikey' || accountCategory === 'bedrock'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
      >
        <div class="mb-3">
          <h3 class="input-label mb-0 text-base font-semibold">{{ t('admin.accounts.quotaControl.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.quotaLimitHint') }}
          </p>
        </div>
        <QuotaLimitCard
          :totalLimit="quotaLimit"
          :dailyLimit="quotaDailyLimit"
          :weeklyLimit="quotaWeeklyLimit"
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
          :dailyResetMode="quotaDailyResetMode"
          :dailyResetHour="quotaDailyResetHour"
          :weeklyResetMode="quotaWeeklyResetMode"
          :weeklyResetDay="quotaWeeklyResetDay"
          :weeklyResetHour="quotaWeeklyResetHour"
          :resetTimezone="quotaResetTimezone"
          @update:totalLimit="quotaLimit = $event"
          @update:dailyLimit="quotaDailyLimit = $event"
          @update:weeklyLimit="quotaWeeklyLimit = $event"
          @update:quotaNotifyDailyEnabled="quotaNotifyState.daily.enabled = $event"
          @update:quotaNotifyDailyThreshold="quotaNotifyState.daily.threshold = $event"
          @update:quotaNotifyDailyThresholdType="quotaNotifyState.daily.thresholdType = $event"
          @update:quotaNotifyWeeklyEnabled="quotaNotifyState.weekly.enabled = $event"
          @update:quotaNotifyWeeklyThreshold="quotaNotifyState.weekly.threshold = $event"
          @update:quotaNotifyWeeklyThresholdType="quotaNotifyState.weekly.thresholdType = $event"
          @update:quotaNotifyTotalEnabled="quotaNotifyState.total.enabled = $event"
          @update:quotaNotifyTotalThreshold="quotaNotifyState.total.threshold = $event"
          @update:quotaNotifyTotalThresholdType="quotaNotifyState.total.thresholdType = $event"
          @update:dailyResetMode="quotaDailyResetMode = $event"
          @update:dailyResetHour="quotaDailyResetHour = $event"
          @update:weeklyResetMode="quotaWeeklyResetMode = $event"
          @update:weeklyResetDay="quotaWeeklyResetDay = $event"
          @update:weeklyResetHour="quotaWeeklyResetHour = $event"
          @update:resetTimezone="quotaResetTimezone = $event"
        />
      </div>

      <!-- OpenAI OAuth Model Mapping (OAuth 类型没有 apikey 容器，需要独立的模型映射区域) -->
      <div
        v-if="form.platform === 'openai' && isOAuthFlow"
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
              data-testid="create-oauth-model-restriction-whitelist"
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
              data-testid="create-oauth-model-restriction-mapping"
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
            <ModelWhitelistSelector v-model="allowedModels" :platform="form.platform" :sync-credentials="syncPreviewCredentials" />
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
              <span v-if="allowedModels.length === 0">{{
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
            data-testid="create-temp-unschedulable"
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

      <!-- Intercept Warmup Requests (Anthropic) -->
      <div
        v-if="form.platform === 'anthropic'"
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
            data-testid="create-intercept-warmup"
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

      <!-- 配额控制 (Anthropic OAuth/SetupToken: 亲和 + 窗口费用 + 会话 + RPM 等) -->
      <div
        v-if="form.platform === 'anthropic' && isOAuthFlow"
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
              data-testid="create-window-cost-enabled"
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
                  data-testid="create-window-cost-limit"
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
                  data-testid="create-window-cost-sticky-reserve"
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
              data-testid="create-session-limit-enabled"
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
                data-testid="create-max-sessions"
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
                  data-testid="create-session-idle-timeout"
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
              data-testid="create-rpm-limit-enabled"
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
              <button type="button" v-for="opt in umqModeOptions"
                :data-testid="`create-user-message-queue-mode-${opt.value || 'off'}`" :key="opt.value"
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
              data-testid="create-tls-fingerprint-enabled"
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
            <select :key="tlsFingerprintProfiles.length" v-model="tlsFingerprintProfileId" class="input" data-testid="create-tls-fingerprint-profile-select">
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

      <div>
        <div class="mb-1 flex items-center gap-2">
          <label class="input-label mb-0">{{ t('admin.accounts.proxy') }}</label>
          <ProxyAdBanner />
        </div>
        <ProxySelector v-model="form.proxy_id" :proxies="proxies" />
      </div>

      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" min="1" class="input"
            @input="form.concurrency = Math.max(1, form.concurrency || 1)" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.loadFactor') }}</label>
          <input v-model.number="loadFactor" type="number" min="1"
            class="input" :placeholder="String(form.concurrency || 1)"
            @input="loadFactor = (loadFactor &amp;&amp; loadFactor >= 1) ? loadFactor : null" />
          <p class="input-hint">{{ t('admin.accounts.loadFactorHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.priority') }}</label>
          <input
            v-model.number="form.priority"
            type="number"
            min="1"
            class="input"
            data-tour="account-form-priority"
          />
          <p class="input-hint">{{ t('admin.accounts.priorityHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
          <input v-model.number="form.rate_multiplier" type="number" min="0" step="0.001" class="input" data-testid="create-rate-multiplier" />
          <p class="input-hint">{{ t('admin.accounts.billingRateMultiplierHint') }}</p>
        </div>
      </div>
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <label class="input-label">{{ t('admin.accounts.expiresAt') }}</label>
        <input v-model="expiresAtInput" type="datetime-local" class="input" />
        <p class="input-hint">{{ t('admin.accounts.expiresAtHint') }}</p>
      </div>

      <!-- OpenAI 自动透传开关（OAuth/API Key） -->
      <div
        v-if="form.platform === 'openai'"
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
            data-testid="create-openai-passthrough-toggle"
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
        v-if="form.platform === 'openai' && accountCategory === 'oauth'"
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
            data-testid="create-openai-flatten-namespaces-toggle"
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

      <!-- OpenAI WS Mode 三态（off/ctx_pool/passthrough） -->
      <div
        v-if="form.platform === 'openai' && (isOAuthFlow || accountCategory === 'apikey')"
        data-testid="create-openai-ws-mode"
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
            <Select v-model="openAIWSMode" :options="openAIWSModeOptions" />
          </div>
        </div>
      </div>

      <!-- Anthropic API Key 自动透传开关 -->
      <div
        v-if="form.platform === 'anthropic' && accountCategory === 'apikey'"
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
            data-testid="create-anthropic-passthrough-toggle"
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
        v-if="form.platform === 'anthropic' && accountCategory === 'apikey'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.apiKeyAuthScheme') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.apiKeyAuthSchemeDesc') }}
            </p>
          </div>
          <select v-model="anthropicAPIKeyAuthScheme" class="input w-52 text-sm" data-testid="create-anthropic-auth-scheme-select">
            <option value="x_api_key">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeXApiKey') }}</option>
            <option value="authorization_bearer">{{ t('admin.accounts.anthropic.apiKeyAuthSchemeBearer') }}</option>
          </select>
        </div>
      </div>

      <!-- Anthropic API Key: Web Search Emulation (hidden when global disabled) -->
      <div
        v-if="form.platform === 'anthropic' && accountCategory === 'apikey' && webSearchGlobalEnabled"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="flex items-center justify-between">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.anthropic.webSearchEmulation') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.anthropic.webSearchEmulationDesc') }}
            </p>
          </div>
          <select v-model="webSearchEmulationMode" class="input w-24 text-sm" data-testid="create-anthropic-web-search-select">
            <option value="default">{{ t('admin.accounts.anthropic.webSearchDefault') }}</option>
            <option value="enabled">{{ t('admin.accounts.anthropic.webSearchEnabled') }}</option>
            <option value="disabled">{{ t('admin.accounts.anthropic.webSearchDisabled') }}</option>
          </select>
        </div>
      </div>

      <!-- OpenAI API 长上下文计费开关 -->
      <div
        v-if="form.platform === 'openai' && !hideOpenAILongContextToggle && (isOAuthFlow || accountCategory === 'apikey')"
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
            @click="toggleOpenAILongContextBilling"
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
        v-if="form.platform === 'openai' && isOAuthFlow"
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
        v-if="form.platform === 'openai' && isOAuthFlow"
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
            <Select v-model="openAICodexFingerprintMode" data-testid="create-codex-fingerprint-mode-select" :options="codexFingerprintModeOptions" />
          </div>
        </div>
      </div>

      <!-- OpenAI Compact 能力配置 -->
      <div
        v-if="form.platform === 'openai' && (isOAuthFlow || accountCategory === 'apikey')"
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
            <Select v-model="openAICompactMode" :options="openAICompactModeOptions" data-testid="create-openai-compact-mode-select" />
          </div>
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
              <input v-model="mapping.from" type="text" class="input flex-1" data-testid="create-openai-compact-model-from" :placeholder="t('admin.accounts.fromModel')" />
              <span class="text-gray-400">→</span>
              <input v-model="mapping.to" type="text" class="input flex-1" data-testid="create-openai-compact-model-to" :placeholder="t('admin.accounts.toModel')" />
              <button type="button" data-testid="create-openai-compact-model-remove" @click="removeOpenAICompactModelMapping(index)" class="text-red-500 hover:text-red-700">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <button type="button" data-testid="create-openai-compact-model-add" @click="addOpenAICompactModelMapping" class="btn btn-secondary text-sm">
            + {{ t('admin.accounts.addMapping') }}
          </button>
        </div>
      </div>

      <!-- OpenAI APIKey Responses API support mode -->
      <div
        v-if="form.platform === 'openai' && accountCategory === 'apikey'"
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
              data-testid="create-openai-responses-mode-select"
            />
          </div>
        </div>
        <p
          v-if="!openAITextGenerationEnabled"
          class="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          data-testid="openai-responses-mode-not-applicable"
        >
          {{ t('admin.accounts.openai.responsesModeTextDisabledHint') }}
        </p>
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
                :data-testid="`create-openai-endpoint-capability-${option.value}`"
                :checked="openAIEndpointCapabilities.includes(option.value)"
                @change="toggleEndpointCapability(option.value, $event)"
              />
              <span class="text-gray-700 dark:text-gray-200">{{ option.label }}</span>
            </label>
          </div>
          <p class="input-hint">{{ t('admin.accounts.openai.endpointCapabilitiesDesc') }}</p>
        </div>
      </div>


      <!-- OpenAI plan tier override (newer capability, styled like the baseline sections) -->
      <div
        v-if="form.platform === 'openai' && accountCategory === 'oauth'"
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
            <Select
              v-model="openAIPlanType"
              :options="planTypeOptions"
              data-testid="create-openai-plan-type"
            />
          </div>
        </div>
      </div>

      <!-- Codex image bridge policy (newer capability, styled like the baseline sections) -->
      <div
        v-if="form.platform === 'openai' && (isOAuthFlow || accountCategory === 'apikey')"
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
                :data-testid="`create-codex-image-tool-${option.value}`"
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
            data-testid="create-auto-pause-expired"
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
        v-if="form.platform === 'openai'"
        class="border-t border-gray-200 pt-4 dark:border-dark-600 space-y-4"
        data-testid="create-openai-auto-pause-section"
      >
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause5hDisabled') }}</label>
            <button
              type="button"
              data-testid="create-auto-pause-5h-disabled"
              @click="autoPause5hDisabled = !autoPause5hDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause5hDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
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
            data-testid="create-auto-pause-5h-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('admin.accounts.autoPause7dDisabled') }}</label>
            <button
              type="button"
              data-testid="create-auto-pause-7d-disabled"
              @click="autoPause7dDisabled = !autoPause7dDisabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                autoPause7dDisabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
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
            data-testid="create-auto-pause-7d-threshold"
          />
          <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
        </div>
      </div>
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <!-- Group Selection - only shown outside simple mode -->
        <GroupSelector
          v-if="!authStore.isSimpleMode"
          v-model="form.group_ids"
          :groups="groups"
          :platform="form.platform"
          data-tour="account-form-groups"
        />
      </div>

    </form>

    <!-- Step 2: OAuth Authorization -->
    <div v-else class="space-y-5">
      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentOAuthLoading"
        :error="currentOAuthError"
        :show-help="form.platform === 'anthropic'"
        :show-proxy-warning="form.platform === 'anthropic' && !!form.proxy_id"
        :allow-multiple="form.platform === 'anthropic'"
        :show-cookie-option="form.platform === 'anthropic'"
        :show-refresh-token-option="form.platform === 'openai'"
        :show-mobile-refresh-token-option="form.platform === 'openai'"
        :show-session-token-option="false"
        :show-access-token-option="false"
        :show-codex-session-import-option="form.platform === 'openai'"
        :show-agent-identity-option="form.platform === 'openai'"
        :show-codex-pat-option="form.platform === 'openai'"
        :show-manual-option="true"
        :initial-input-method="'manual'"
        :platform="form.platform"
        @generate-url="handleGenerateAuthUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleOpenAIRefreshTokens"
        @validate-mobile-refresh-token="handleOpenAIMobileRefreshTokens"
        @import-codex-session="handleOpenAIImportCodexSession"
        @import-codex-pat="handleOpenAIImportCodexPAT"
      />

    </div>

    <template #footer>
      <div v-if="step === 1" class="flex justify-end gap-3">
        <button @click="handleClose" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="create-account-form"
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
          {{
            isOAuthFlow
              ? t('common.next')
              : submitting
                ? t('admin.accounts.creating')
                : t('common.create')
          }}
        </button>
      </div>
      <div v-else class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="goBackToBasicInfo">
          {{ t('common.back') }}
        </button>
        <button
          v-if="oauthFlowRef?.inputMethod === 'manual'"
          type="button"
          :disabled="!canExchangeOAuthCode"
          class="btn btn-primary"
          @click="handleExchangeOAuthCode"
        >
          <svg
            v-if="currentOAuthLoading"
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
          {{
            currentOAuthLoading
              ? t('admin.accounts.oauth.verifying')
              : t('admin.accounts.oauth.completeAuth')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>

</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import { useAccountOAuth, type AddMethod, type AuthInputMethod } from '@/composables/useAccountOAuth'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import {
  buildModelMappingObject,
  commonErrorCodes,
  getPresetMappingsByPlatform
} from '@/composables/useModelWhitelist'
import { useQuotaNotifyState } from '@/composables/useQuotaNotifyState'
import { allSelectedGroupsEnableLongContextPricing } from './longContextBilling'
import {
  applyHeaderOverride,
  applyInterceptWarmup,
  applyPlanType,
  buildPlanTypeOptions,
  isHeaderOverrideCapable,
  validateHeaderOverrideRows,
  type HeaderOverrideRow
} from './credentialsBuilder'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import type {
  AccountType,
  AdminGroup,
  CodexSessionImportMessage,
  CreateAccountRequest,
  OpenAICompactMode,
  OpenAIEndpointCapability,
  OpenAIResponsesMode,
  Proxy
} from '@/types'
import {
  resolveOpenAIWSModeConcurrencyHintKey,
  type OpenAIWSMode
} from '@/utils/openaiWsMode'
import { formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
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
import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'
import QuotaLimitCard from './QuotaLimitCard.vue'

type SupportedPlatform = 'anthropic' | 'openai'
type AccountCategory = 'oauth' | 'setup-token' | 'apikey' | 'bedrock' | 'service_account'
type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'
type CodexImageToolMode = 'inherit' | 'enabled' | 'disabled' | 'block'
type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'

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

interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  inputMethod: AuthInputMethod
  reset: () => void
}

const props = defineProps<{
  show: boolean
  proxies: Proxy[]
  groups: AdminGroup[]
}>()
const emit = defineEmits<{ close: []; created: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const anthropicOAuth = useAccountOAuth()
const openaiOAuth = useOpenAIOAuth()
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)
const step = ref<1 | 2>(1)
const submitting = ref(false)
const accountCategory = ref<AccountCategory>('oauth')

const form = reactive({
  name: '',
  notes: '',
  platform: 'anthropic' as SupportedPlatform,
  proxy_id: null as number | null,
  concurrency: 10,
  priority: 1,
  rate_multiplier: 1,
  group_ids: [] as number[]
})

const apiKeyValue = ref('')
const apiKeyBaseUrl = ref('https://api.anthropic.com')
const allowedModels = ref<string[]>([])
const upstreamBillingProbeEnabled = ref(true)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const bedrockAuthMode = ref<'sigv4' | 'apikey'>('sigv4')
const bedrockAccessKeyId = ref('')
const bedrockSecretAccessKey = ref('')
const bedrockSessionToken = ref('')
const bedrockApiKey = ref('')
const bedrockRegion = ref('us-east-1')
const bedrockForceGlobal = ref(false)
const vertexServiceAccountJson = ref('')
const vertexServiceAccountFileInput = ref<HTMLInputElement | null>(null)
const vertexProjectId = ref('')
const vertexClientEmail = ref('')
const vertexServiceAccountDragActive = ref(false)
const vertexLocation = ref('global')
const openAILongContextBillingEnabled = ref(false)
const openAILongContextBillingTouched = ref(false)
const openAIFlattenNamespaces = ref(false)
const openAIWSMode = ref<OpenAIWSMode>('off')

// Advanced account controls retained for Claude Code/OpenAI compatibility.
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const modelMappings = ref<ModelMapping[]>([])
const openAICompactModelMappings = ref<ModelMapping[]>([])
const DEFAULT_POOL_MODE_RETRY_COUNT = 3
const MAX_POOL_MODE_RETRY_COUNT = 10
const DEFAULT_POOL_MODE_RETRY_STATUS_CODES = [401, 403, 429]
const poolModeEnabled = ref(false)
const poolModeRetryCount = ref(DEFAULT_POOL_MODE_RETRY_COUNT)
const poolModeRetryStatusCodesInput = ref('')
const customErrorCodesEnabled = ref(false)
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | null>(null)
const interceptWarmupRequests = ref(false)
const tempUnschedEnabled = ref(false)
const tempUnschedRules = ref<TempUnschedRuleForm[]>([])
const getModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-model-mapping')
const getOpenAICompactModelMappingKey = createStableObjectKeyResolver<ModelMapping>('create-openai-compact-model-mapping')
const getTempUnschedRuleKey = createStableObjectKeyResolver<TempUnschedRuleForm>('create-temp-unsched-rule')
const autoPauseOnExpired = ref(true)
const autoPause5hThresholdPercent = ref<number | null>(null)
const autoPause7dThresholdPercent = ref<number | null>(null)
const autoPause5hDisabled = ref(false)
const autoPause7dDisabled = ref(false)
const loadFactor = ref<number | null>(null)
const expiresAt = ref<number | null>(null)
const openaiPassthroughEnabled = ref(false)
const openAICodexCLIOnlyEnabled = ref(false)
const openAICodexCLIOnlyAppServerEnabled = ref(false)
const openAICodexFingerprintMode = ref<CodexFingerprintMode>('off')
const openAICompactMode = ref<OpenAICompactMode>('auto')
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>(['chat_completions', 'embeddings'])
const codexImageToolMode = ref<CodexImageToolMode>('inherit')
const openAIPlanType = ref('')
const anthropicPassthroughEnabled = ref(false)
const anthropicAPIKeyAuthScheme = ref<AnthropicAPIKeyAuthScheme>('x_api_key')
const webSearchEmulationMode = ref<'default' | 'enabled' | 'disabled'>('default')
const webSearchGlobalEnabled = ref(false)
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
const quotaDailyResetMode = ref<'rolling' | 'fixed' | null>('rolling')
const quotaDailyResetHour = ref<number | null>(0)
const quotaWeeklyResetMode = ref<'rolling' | 'fixed' | null>('rolling')
const quotaWeeklyResetDay = ref<number | null>(1)
const quotaWeeklyResetHour = ref<number | null>(0)
const quotaResetTimezone = ref<string | null>('UTC')
const {
  globalEnabled: quotaNotifyGlobalEnabled,
  state: quotaNotifyState,
  loadGlobalState: loadQuotaNotifyGlobal,
  writeToExtra: writeQuotaNotifyToExtra,
} = useQuotaNotifyState()

loadQuotaNotifyGlobal()

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
const isOAuthFlow = computed(() =>
  accountCategory.value === 'oauth' || accountCategory.value === 'setup-token'
)
const oauthStepTitle = computed(() =>
  form.platform === 'openai'
    ? t('admin.accounts.oauth.openai.title')
    : t('admin.accounts.oauth.title')
)
const addMethod = computed<AddMethod>({
  get: () => accountCategory.value === 'setup-token' ? 'setup-token' : 'oauth',
  set: method => { accountCategory.value = method }
})
const baseUrlHint = computed(() =>
  form.platform === 'openai'
    ? t('admin.accounts.openai.baseUrlHint')
    : t('admin.accounts.baseUrlHint')
)
const apiKeyHint = computed(() =>
  form.platform === 'openai'
    ? t('admin.accounts.openai.apiKeyHint')
    : t('admin.accounts.apiKeyHint')
)
const apiKeyBaseUrlPlaceholder = computed(() =>
  form.platform === 'openai' ? 'https://api.openai.com' : 'https://api.anthropic.com'
)
const apiKeyValuePlaceholder = computed(() =>
  form.platform === 'openai' ? 'sk-proj-...' : 'sk-ant-...'
)
const currentAuthUrl = computed(() =>
  form.platform === 'openai' ? openaiOAuth.authUrl.value : anthropicOAuth.authUrl.value
)
const currentSessionId = computed(() =>
  form.platform === 'openai' ? openaiOAuth.sessionId.value : anthropicOAuth.sessionId.value
)
const currentOAuthLoading = computed(() =>
  form.platform === 'openai' ? openaiOAuth.loading.value : anthropicOAuth.loading.value
)
const currentOAuthError = computed(() =>
  form.platform === 'openai' ? openaiOAuth.error.value : anthropicOAuth.error.value
)
const canExchangeOAuthCode = computed(() =>
  Boolean(oauthFlowRef.value?.authCode?.trim() && currentSessionId.value && !currentOAuthLoading.value)
)
const headerOverrideCapable = computed(() =>
  isHeaderOverrideCapable(form.platform, 'apikey')
)
const isOpenAIModelRestrictionDisabled = computed(() =>
  form.platform === 'openai' && openaiPassthroughEnabled.value
)
const syncPreviewCredentials = computed(() => ({
  platform: form.platform,
  type: 'apikey',
  base_url: apiKeyBaseUrl.value.trim(),
  api_key: apiKeyValue.value.trim()
}))
const hideOpenAILongContextToggle = computed(() =>
  !authStore.isSimpleMode &&
  allSelectedGroupsEnableLongContextPricing(form.group_ids, props.groups)
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
const openAITextEndpointCapabilityLabel = computed(() => {
  if (openAIResponsesMode.value === 'force_responses') {
    return t('admin.accounts.openai.capabilityResponses')
  }
  if (openAIResponsesMode.value === 'force_chat_completions') {
    return t('admin.accounts.openai.capabilityChatCompletions')
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
const openAIWSModeOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.openai.wsModeOff') },
  { value: 'ctx_pool', label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: 'passthrough', label: t('admin.accounts.openai.wsModePassthrough') },
  { value: 'http_bridge', label: t('admin.accounts.openai.wsModeHttpBridge') }
])
const openAIWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openAIWSMode.value)
)
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
const openAITextGenerationEnabled = computed(() =>
  openAIEndpointCapabilities.value.includes('chat_completions')
)
const presetMappings = computed(() =>
  getPresetMappingsByPlatform(accountCategory.value === 'bedrock' ? 'bedrock' : form.platform)
)
const bedrockPresets = computed(() => getPresetMappingsByPlatform('bedrock'))
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

watch(() => props.show, visible => {
  if (visible) {
    void loadTLSProfiles()
    void loadWebSearchEmulationConfig()
  } else {
    resetForm()
  }
}, { immediate: true })
watch(accountCategory, category => {
  if (form.platform === 'openai' && !['oauth', 'apikey'].includes(category)) {
    accountCategory.value = 'oauth'
  }
})

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

function addPresetMapping(from: string, to: string) {
  if (modelMappings.value.some(mapping => mapping.from === from)) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  modelMappings.value.push({ from, to })
}

function addModelMapping() {
  modelMappings.value.push({ from: '', to: '' })
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

function applyVertexServiceAccountJson(value: string): boolean {
  const raw = value.trim()
  if (!raw) {
    vertexProjectId.value = ''
    vertexClientEmail.value = ''
    appStore.showError(t('admin.accounts.vertexSaJsonRequired'))
    return false
  }
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>
    const projectId = typeof parsed.project_id === 'string' ? parsed.project_id.trim() : ''
    const clientEmail = typeof parsed.client_email === 'string' ? parsed.client_email.trim() : ''
    const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
    if (!projectId || !clientEmail || !privateKey) {
      appStore.showError(t('admin.accounts.vertexSaJsonMissingFields'))
      return false
    }
    vertexProjectId.value = projectId
    vertexClientEmail.value = clientEmail
    vertexServiceAccountJson.value = JSON.stringify(parsed)
    return true
  } catch {
    appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
    return false
  }
}

async function handleVertexServiceAccountFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    applyVertexServiceAccountJson(await file.text())
  } finally {
    input.value = ''
  }
}

async function handleVertexServiceAccountDrop(event: DragEvent) {
  vertexServiceAccountDragActive.value = false
  const file = event.dataTransfer?.files?.[0]
  if (!file) return
  applyVertexServiceAccountJson(await file.text())
}

function toggleEndpointCapability(capability: OpenAIEndpointCapability, event: Event) {
  const target = event.target as HTMLInputElement
  const checked = target.checked
  if (checked) {
    if (!openAIEndpointCapabilities.value.includes(capability)) {
      openAIEndpointCapabilities.value = [...openAIEndpointCapabilities.value, capability]
    }
    return
  }
  if (openAIEndpointCapabilities.value.length <= 1) {
    target.checked = true
    return
  }
  openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(value => value !== capability)
  if (!openAIEndpointCapabilities.value.includes('chat_completions')) openAIResponsesMode.value = 'auto'
}

// The backend treats an omitted `openai_capabilities` value as the legacy
// all-endpoints default.  Persist a narrowed selection only when the operator
// explicitly disables one of the two supported API-key endpoints.
function applyOpenAIEndpointCapabilities(credentials: Record<string, unknown>) {
  if (form.platform !== 'openai' || accountCategory.value !== 'apikey') return
  const capabilities = ['chat_completions', 'embeddings']
    .filter(value => openAIEndpointCapabilities.value.includes(value as OpenAIEndpointCapability))
  if (capabilities.length === 2 || capabilities.length === 0) {
    delete credentials.openai_capabilities
  } else {
    credentials.openai_capabilities = capabilities
  }
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
  if (!confirmCustomErrorCode(code)) return
  selectedErrorCodes.value.push(code)
  customErrorCodeInput.value = null
}

function confirmCustomErrorCode(code: number): boolean {
  if (code === 429) return window.confirm(t('admin.accounts.customErrorCodes429Warning'))
  if (code === 529) return window.confirm(t('admin.accounts.customErrorCodes529Warning'))
  return true
}

function toggleErrorCode(code: number) {
  const index = selectedErrorCodes.value.indexOf(code)
  if (index >= 0) {
    selectedErrorCodes.value.splice(index, 1)
    return
  }
  if (confirmCustomErrorCode(code)) selectedErrorCodes.value.push(code)
}

function removeErrorCode(code: number) {
  selectedErrorCodes.value = selectedErrorCodes.value.filter(value => value !== code)
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
  const replacement = tempUnschedRules.value[target]
  if (!current || !replacement) return
  tempUnschedRules.value[index] = replacement
  tempUnschedRules.value[target] = current
}

function parseRetryStatusCodes(value: string): number[] {
  return value.split(/[,\s]+/).map(Number).filter(code => Number.isInteger(code) && code >= 100 && code <= 599)
}

function buildTempUnschedRules() {
  return tempUnschedRules.value.map(rule => ({
    error_code: Number(rule.error_code),
    keywords: rule.keywords.split(/[,;]/).map(value => value.trim()).filter(Boolean),
    duration_minutes: Number(rule.duration_minutes),
    description: rule.description.trim()
  })).filter(rule => Number.isInteger(rule.error_code) && rule.error_code >= 100 && rule.error_code <= 599 && rule.keywords.length > 0 && rule.duration_minutes > 0)
}

function selectPlatform(platform: SupportedPlatform) {
  form.platform = platform
  if (platform === 'openai' && !['oauth', 'apikey'].includes(accountCategory.value)) {
    accountCategory.value = 'oauth'
  }
  apiKeyBaseUrl.value = platform === 'openai'
    ? 'https://api.openai.com'
    : 'https://api.anthropic.com'
  allowedModels.value = []
  modelRestrictionMode.value = 'whitelist'
  modelMappings.value = []
  openAICompactModelMappings.value = []
  headerOverrideEnabled.value = false
  headerOverrideRows.value = []
  openaiPassthroughEnabled.value = false
  openAICodexCLIOnlyEnabled.value = false
  openAICodexCLIOnlyAppServerEnabled.value = false
  openAICodexFingerprintMode.value = 'off'
  openAICompactMode.value = 'auto'
  openAIResponsesMode.value = 'auto'
  openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
  codexImageToolMode.value = 'inherit'
  openAIPlanType.value = ''
  anthropicPassthroughEnabled.value = false
  anthropicAPIKeyAuthScheme.value = 'x_api_key'
  webSearchEmulationMode.value = 'default'
  vertexServiceAccountJson.value = ''
  vertexProjectId.value = ''
  vertexClientEmail.value = ''
  vertexServiceAccountDragActive.value = false
}

function toggleOpenAILongContextBilling() {
  openAILongContextBillingEnabled.value = !openAILongContextBillingEnabled.value
  openAILongContextBillingTouched.value = true
}

function buildOpenAIExtra(forImport = false): Record<string, unknown> | undefined {
  if (form.platform !== 'openai') return undefined
  const extra: Record<string, unknown> = {}
  const modePrefix = accountCategory.value === 'apikey' ? 'openai_apikey' : 'openai_oauth'
  extra[modePrefix + '_responses_websockets_v2_mode'] = openAIWSMode.value
  extra[modePrefix + '_responses_websockets_v2_enabled'] = openAIWSMode.value !== 'off'
  if (openaiPassthroughEnabled.value) {
    extra.openai_passthrough = true
  }
  if (accountCategory.value === 'oauth' && openAIFlattenNamespaces.value) {
    extra.openai_responses_flatten_namespaces = true
  }
  if (!forImport || openAILongContextBillingTouched.value) {
    extra.openai_long_context_billing_enabled = openAILongContextBillingEnabled.value
  }
  if (openAICompactMode.value !== 'auto') extra.openai_compact_mode = openAICompactMode.value
  if (accountCategory.value === 'apikey' && openAITextGenerationEnabled.value && openAIResponsesMode.value !== 'auto') {
    extra.openai_responses_mode = openAIResponsesMode.value
  }
  if ((accountCategory.value === 'oauth' || accountCategory.value === 'setup-token') && openAICodexCLIOnlyEnabled.value) {
    extra.codex_cli_only = true
    if (openAICodexCLIOnlyAppServerEnabled.value) extra.codex_cli_only_allow_app_server = true
  }
  if (accountCategory.value === 'oauth' && openAICodexFingerprintMode.value !== 'off') {
    extra.codex_fingerprint_mode = openAICodexFingerprintMode.value
  }
  if (codexImageToolMode.value === 'enabled' || codexImageToolMode.value === 'disabled') {
    extra.codex_image_generation_bridge = codexImageToolMode.value === 'enabled'
  } else if (codexImageToolMode.value === 'block') {
    extra.codex_image_generation_explicit_tool_policy = 'strip'
  }
  if (accountCategory.value === 'oauth' && openAIPlanType.value.trim()) {
    // plan_type is a credential field; this key is intentionally not copied to extra.
  }
  if (autoPause5hThresholdPercent.value != null && autoPause5hThresholdPercent.value > 0) {
    extra.auto_pause_5h_threshold = Math.min(100, autoPause5hThresholdPercent.value) / 100
  }
  if (autoPause7dThresholdPercent.value != null && autoPause7dThresholdPercent.value > 0) {
    extra.auto_pause_7d_threshold = Math.min(100, autoPause7dThresholdPercent.value) / 100
  }
  if (autoPause5hDisabled.value) extra.auto_pause_5h_disabled = true
  if (autoPause7dDisabled.value) extra.auto_pause_7d_disabled = true
  return Object.keys(extra).length > 0 ? extra : undefined
}

function buildAnthropicExtra(base?: Record<string, unknown>): Record<string, unknown> | undefined {
  const extra: Record<string, unknown> = { ...(base || {}) }
  if (form.platform !== 'anthropic') return Object.keys(extra).length ? extra : undefined
  if (accountCategory.value === 'apikey') {
    if (anthropicPassthroughEnabled.value) extra.anthropic_passthrough = true
    if (anthropicAPIKeyAuthScheme.value === 'authorization_bearer') {
      extra.anthropic_apikey_auth_scheme = 'authorization_bearer'
    }
    if (webSearchEmulationMode.value !== 'default') extra.web_search_emulation = webSearchEmulationMode.value
  }
  if (accountCategory.value === 'oauth' || accountCategory.value === 'setup-token') {
    if (windowCostEnabled.value && windowCostLimit.value && windowCostLimit.value > 0) {
      extra.window_cost_limit = windowCostLimit.value
      extra.window_cost_sticky_reserve = windowCostStickyReserve.value ?? 10
    }
    if (sessionLimitEnabled.value && maxSessions.value && maxSessions.value > 0) {
      extra.max_sessions = maxSessions.value
      extra.session_idle_timeout_minutes = sessionIdleTimeout.value ?? 5
    }
    if (rpmLimitEnabled.value) {
      extra.base_rpm = baseRpm.value && baseRpm.value > 0 ? baseRpm.value : 15
      extra.rpm_strategy = rpmStrategy.value
      if (rpmStickyBuffer.value && rpmStickyBuffer.value > 0) extra.rpm_sticky_buffer = rpmStickyBuffer.value
    }
    if (userMsgQueueMode.value) extra.user_msg_queue_mode = userMsgQueueMode.value
    if (tlsFingerprintEnabled.value) {
      extra.enable_tls_fingerprint = true
      if (tlsFingerprintProfileId.value) extra.tls_fingerprint_profile_id = tlsFingerprintProfileId.value
    }
    if (sessionIdMaskingEnabled.value) extra.session_id_masking_enabled = true
    if (cacheTTLOverrideEnabled.value) {
      extra.cache_ttl_override_enabled = true
      extra.cache_ttl_override_target = cacheTTLOverrideTarget.value
    }
    if (customBaseUrlEnabled.value && customBaseUrl.value.trim()) {
      extra.custom_base_url_enabled = true
      extra.custom_base_url = customBaseUrl.value.trim()
    }
  }
  return Object.keys(extra).length > 0 ? extra : undefined
}

function buildCommonCredentials(credentials: Record<string, unknown>, mode: 'create' | 'edit' = 'create') {
  const mapping = buildModelMappingObject(modelRestrictionMode.value, allowedModels.value, modelMappings.value)
  if (mapping && !(form.platform === 'openai' && openaiPassthroughEnabled.value)) credentials.model_mapping = mapping
  const compactMapping = buildModelMappingObject('mapping', [], openAICompactModelMappings.value)
  if (form.platform === 'openai' && compactMapping) credentials.compact_model_mapping = compactMapping
  if (poolModeEnabled.value) {
    credentials.pool_mode = true
    credentials.pool_mode_retry_count = Math.max(0, Math.min(10, Math.trunc(poolModeRetryCount.value || 0)))
    const codes = parseRetryStatusCodes(poolModeRetryStatusCodesInput.value)
    if (codes.length) credentials.pool_mode_retry_status_codes = codes
  } else if (mode === 'edit') {
    delete credentials.pool_mode
    delete credentials.pool_mode_retry_count
    delete credentials.pool_mode_retry_status_codes
  }
  if (customErrorCodesEnabled.value) {
    credentials.custom_error_codes_enabled = true
    credentials.custom_error_codes = [...selectedErrorCodes.value]
  } else if (mode === 'edit') {
    delete credentials.custom_error_codes_enabled
    delete credentials.custom_error_codes
  }
  applyInterceptWarmup(credentials, interceptWarmupRequests.value, mode)
  if (tempUnschedEnabled.value) {
    const rules = buildTempUnschedRules()
    if (rules.length) {
      credentials.temp_unschedulable_enabled = true
      credentials.temp_unschedulable_rules = rules
    }
  } else if (mode === 'edit') {
    delete credentials.temp_unschedulable_enabled
    delete credentials.temp_unschedulable_rules
  }
  return credentials
}

// Codex session/PAT imports carry account-wide credential settings separately
// from the token material. Keep the same administrator-selected controls used
// by ordinary account creation, while letting the backend protect token fields.
function buildOpenAICodexImportCredentialExtras(): Record<string, unknown> | null {
  if (tempUnschedEnabled.value && buildTempUnschedRules().length === 0) {
    appStore.showError(t('admin.accounts.tempUnschedulable.rulesInvalid'))
    return null
  }
  return buildCommonCredentials({})
}

function buildOpenAICodexImportExtra(): Record<string, unknown> | undefined {
  return buildOpenAIExtra(true)
}

function formatCodexImportMessages(messages?: CodexSessionImportMessage[]): string {
  return (messages || []).map(item => {
    const name = item.name ? ` ${item.name}` : ''
    return `#${item.index}${name}: ${item.message}`
  }).join('\n')
}

function buildQuotaExtra(base?: Record<string, unknown>) {
  const extra: Record<string, unknown> = { ...(base || {}) }
  if (accountCategory.value !== 'apikey' && accountCategory.value !== 'bedrock') {
    return Object.keys(extra).length ? extra : undefined
  }
  if (quotaLimit.value != null && quotaLimit.value > 0) extra.quota_limit = quotaLimit.value
  if (quotaDailyLimit.value != null && quotaDailyLimit.value > 0) extra.quota_daily_limit = quotaDailyLimit.value
  if (quotaWeeklyLimit.value != null && quotaWeeklyLimit.value > 0) extra.quota_weekly_limit = quotaWeeklyLimit.value
  if (quotaDailyResetMode.value === 'fixed') {
    extra.quota_daily_reset_mode = 'fixed'
    extra.quota_daily_reset_hour = quotaDailyResetHour.value
  }
  if (quotaWeeklyResetMode.value === 'fixed') {
    extra.quota_weekly_reset_mode = 'fixed'
    extra.quota_weekly_reset_day = quotaWeeklyResetDay.value
    extra.quota_weekly_reset_hour = quotaWeeklyResetHour.value
  }
  if (quotaDailyResetMode.value === 'fixed' || quotaWeeklyResetMode.value === 'fixed') extra.quota_reset_timezone = quotaResetTimezone.value || 'UTC'
  writeQuotaNotifyToExtra(extra, 'create')
  return Object.keys(extra).length ? extra : undefined
}

function buildBasePayload(
  platform: SupportedPlatform,
  type: AccountType,
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>
): CreateAccountRequest {
  return {
    name: form.name.trim(),
    notes: form.notes.trim() || null,
    platform,
    type,
    credentials,
    extra,
    proxy_id: form.proxy_id,
    concurrency: form.concurrency,
    load_factor: loadFactor.value,
    priority: form.priority,
    rate_multiplier: form.rate_multiplier,
    group_ids: form.group_ids,
    expires_at: expiresAt.value,
    auto_pause_on_expired: autoPauseOnExpired.value,
    upstream_billing_probe_enabled: type === 'apikey'
      ? upstreamBillingProbeEnabled.value
      : undefined
  }
}

async function createAndFinish(payload: CreateAccountRequest) {
  submitting.value = true
  try {
    const account = await adminAPI.accounts.create(payload)
    if (payload.type === 'apikey' && payload.upstream_billing_probe_enabled === true) {
      try {
        await adminAPI.accounts.probeUpstreamBilling(account.id)
      } catch {
        appStore.showWarning(t('admin.accounts.upstreamBilling.probeFailed'))
      }
    }
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message ||
      error.response?.data?.detail ||
      t('admin.accounts.failedToCreate')
    )
  } finally {
    submitting.value = false
  }
}

async function handleSubmit() {
  if (!form.name.trim()) {
    appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
    return
  }
  if (tempUnschedEnabled.value && buildTempUnschedRules().length === 0) {
    appStore.showError(t('admin.accounts.tempUnschedulable.rulesInvalid'))
    return
  }
  if (accountCategory.value === 'oauth' || accountCategory.value === 'setup-token') {
    step.value = 2
    return
  }

  if (accountCategory.value === 'apikey') {
    if (!apiKeyValue.value.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterApiKey'))
      return
    }
    const credentials: Record<string, unknown> = {
      api_key: apiKeyValue.value.trim(),
      base_url: apiKeyBaseUrl.value.trim() || (
        form.platform === 'openai' ? 'https://api.openai.com' : 'https://api.anthropic.com'
      )
    }
    const mapping = buildModelMappingObject('whitelist', allowedModels.value, [])
    if (mapping) credentials.model_mapping = mapping
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
      'create'
    )
    buildCommonCredentials(credentials)
    applyOpenAIEndpointCapabilities(credentials)
    const extra = buildQuotaExtra(buildAnthropicExtra(
      form.platform === 'openai' ? buildOpenAIExtra() : undefined
    ))
    await createAndFinish(buildBasePayload(
      form.platform,
      'apikey',
      credentials,
      extra
    ))
    return
  }

  if (accountCategory.value === 'bedrock') {
    const credentials: Record<string, unknown> = {
      auth_mode: bedrockAuthMode.value,
      aws_region: bedrockRegion.value.trim() || 'us-east-1'
    }
    if (bedrockForceGlobal.value) credentials.aws_force_global = 'true'
    if (bedrockAuthMode.value === 'sigv4') {
      if (!bedrockAccessKeyId.value.trim() || !bedrockSecretAccessKey.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockAccessKeyIdRequired'))
        return
      }
      credentials.aws_access_key_id = bedrockAccessKeyId.value.trim()
      credentials.aws_secret_access_key = bedrockSecretAccessKey.value.trim()
      if (bedrockSessionToken.value.trim()) {
        credentials.aws_session_token = bedrockSessionToken.value.trim()
      }
    } else {
      if (!bedrockApiKey.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockApiKeyRequired'))
        return
      }
      credentials.api_key = bedrockApiKey.value.trim()
    }
    const mapping = buildModelMappingObject('whitelist', allowedModels.value, [])
    if (mapping) credentials.model_mapping = mapping
    buildCommonCredentials(credentials)
    const extra = buildQuotaExtra()
    await createAndFinish(buildBasePayload('anthropic', 'bedrock', credentials, extra))
    return
  }

  if (!applyVertexServiceAccountJson(vertexServiceAccountJson.value)) return
  const credentials = buildCommonCredentials({
    service_account_json: vertexServiceAccountJson.value,
    project_id: vertexProjectId.value,
    client_email: vertexClientEmail.value,
    location: vertexLocation.value,
    tier_id: 'vertex'
  })
  await createAndFinish(buildBasePayload('anthropic', 'service_account', credentials, buildAnthropicExtra()))
}

async function handleGenerateAuthUrl() {
  if (form.platform === 'openai') {
    await openaiOAuth.generateAuthUrl(form.proxy_id)
  } else {
    await anthropicOAuth.generateAuthUrl(addMethod.value, form.proxy_id)
  }
}

async function createOAuthAccount(
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>,
  name?: string
) {
  buildCommonCredentials(credentials)
  if (form.platform === 'openai' && accountCategory.value === 'oauth') {
    applyPlanType(credentials, openAIPlanType.value)
  }
  const mergedExtra = form.platform === 'openai'
    ? buildQuotaExtra({ ...(extra || {}), ...(buildOpenAIExtra() || {}) })
    : buildQuotaExtra(buildAnthropicExtra(extra))
  const payload = buildBasePayload(
    form.platform,
    form.platform === 'anthropic' ? addMethod.value : 'oauth',
    credentials,
    mergedExtra
  )
  if (name) payload.name = name
  await createAndFinish(payload)
}

async function handleExchangeOAuthCode() {
  const code = oauthFlowRef.value?.authCode.trim()
  if (!code) return
  if (form.platform === 'openai') {
    const state = (oauthFlowRef.value?.oauthState || openaiOAuth.oauthState.value).trim()
    const tokenInfo = await openaiOAuth.exchangeAuthCode(
      code,
      openaiOAuth.sessionId.value,
      state,
      form.proxy_id
    )
    if (tokenInfo) {
      await createOAuthAccount(
        openaiOAuth.buildCredentials(tokenInfo),
        openaiOAuth.buildExtraInfo(tokenInfo)
      )
    }
    return
  }
  anthropicOAuth.authCode.value = code
  const tokenInfo = await anthropicOAuth.exchangeAuthCode(addMethod.value, form.proxy_id)
  if (tokenInfo) {
    await createOAuthAccount(
      { ...tokenInfo },
      anthropicOAuth.buildExtraInfo(tokenInfo)
    )
  }
}

async function handleCookieAuth(input: string) {
  if (form.platform !== 'anthropic') return
  const keys = anthropicOAuth.parseSessionKeys(input)
  if (keys.length === 0) return
  submitting.value = true
  anthropicOAuth.error.value = ''
  let successCount = 0
  let failedCount = 0
  const errors: string[] = []
  try {
    for (let index = 0; index < keys.length; index += 1) {
      try {
        const tokenInfo = await anthropicOAuth.cookieAuth(addMethod.value, keys[index], form.proxy_id)
        if (!tokenInfo) {
          failedCount += 1
          errors.push(`#${index + 1}: ${anthropicOAuth.error.value || t('admin.accounts.oauth.authFailed')}`)
          continue
        }
        const payload = buildBasePayload(
          'anthropic',
          addMethod.value,
          buildCommonCredentials({ ...tokenInfo }),
          buildQuotaExtra(buildAnthropicExtra(anthropicOAuth.buildExtraInfo(tokenInfo)))
        )
        if (keys.length > 1) payload.name = form.name.trim() + ' #' + (index + 1)
        await adminAPI.accounts.create(payload)
        successCount += 1
      } catch (error: any) {
        failedCount += 1
        errors.push(`#${index + 1}: ${error.response?.data?.detail || error.response?.data?.message || error.message || t('admin.accounts.oauth.authFailed')}`)
      }
    }

    if (successCount > 0 && failedCount === 0) {
      appStore.showSuccess(
        keys.length > 1
          ? t('admin.accounts.oauth.batchSuccess', { count: successCount })
          : t('admin.accounts.accountCreated')
      )
      emit('created')
      handleClose()
    } else if (successCount > 0) {
      anthropicOAuth.error.value = errors.join('\n')
      appStore.showWarning(t('admin.accounts.oauth.batchPartialSuccess', {
        success: successCount,
        failed: failedCount
      }))
      emit('created')
    } else {
      anthropicOAuth.error.value = errors.join('\n')
      appStore.showError(t('admin.accounts.oauth.batchFailed'))
    }
  } finally {
    submitting.value = false
  }
}

const OPENAI_MOBILE_RT_CLIENT_ID = 'app_LlGpXReQgckcGGUo2JrYvtJK'

async function handleOpenAIRefreshTokens(input: string, clientId?: string) {
  if (form.platform !== 'openai') return
  const tokens = input.split('\n').map(value => value.trim()).filter(Boolean)
  if (tokens.length === 0) return
  openaiOAuth.loading.value = true
  openaiOAuth.error.value = ''
  let successCount = 0
  let failedCount = 0
  const errors: string[] = []
  try {
    for (let index = 0; index < tokens.length; index += 1) {
      try {
        const tokenInfo = await openaiOAuth.validateRefreshToken(tokens[index], form.proxy_id, clientId)
        if (!tokenInfo) {
          failedCount += 1
          errors.push(`#${index + 1}: ${openaiOAuth.error.value || t('admin.accounts.oauth.authFailed')}`)
          openaiOAuth.error.value = ''
          continue
        }
        const payload = buildBasePayload(
          'openai',
          'oauth',
          (() => {
            const credentials = buildCommonCredentials(openaiOAuth.buildCredentials(tokenInfo))
            if (clientId) credentials.client_id = clientId
            applyPlanType(credentials, openAIPlanType.value)
            return credentials
          })(),
          buildQuotaExtra({
            ...(openaiOAuth.buildExtraInfo(tokenInfo) || {}),
            ...(buildOpenAIExtra() || {})
          })
        )
        if (tokens.length > 1) payload.name = form.name.trim() + ' #' + (index + 1)
        await adminAPI.accounts.create(payload)
        successCount += 1
      } catch (error: any) {
        failedCount += 1
        errors.push(`#${index + 1}: ${error.response?.data?.detail || error.message || t('admin.accounts.oauth.authFailed')}`)
      }
    }

    if (successCount > 0 && failedCount === 0) {
      appStore.showSuccess(
        tokens.length > 1
          ? t('admin.accounts.oauth.batchSuccess', { count: successCount })
          : t('admin.accounts.accountCreated')
      )
      emit('created')
      handleClose()
    } else if (successCount > 0) {
      appStore.showWarning(t('admin.accounts.oauth.batchPartialSuccess', {
        success: successCount,
        failed: failedCount
      }))
      openaiOAuth.error.value = errors.join('\n')
      emit('created')
    } else {
      openaiOAuth.error.value = errors.join('\n')
      appStore.showError(t('admin.accounts.oauth.batchFailed'))
    }
  } finally {
    openaiOAuth.loading.value = false
  }
}

const handleOpenAIMobileRefreshTokens = (input: string) =>
  handleOpenAIRefreshTokens(input, OPENAI_MOBILE_RT_CLIENT_ID)

function isAgentIdentityImport(content: string): boolean {
  const isAgentIdentityValue = (value: unknown): boolean => {
    if (Array.isArray(value)) return value.length > 0 && value.every(isAgentIdentityValue)
    if (!value || typeof value !== 'object') return false
    const record = value as Record<string, unknown>
    const authMode = record.auth_mode ?? record.authMode
    const agentIdentity = record.agent_identity ?? record.agentIdentity
    return (typeof authMode === 'string' && authMode.toLowerCase().replace(/[_-]/g, '') === 'agentidentity')
      || (!!agentIdentity && typeof agentIdentity === 'object')
  }

  try {
    return isAgentIdentityValue(JSON.parse(content))
  } catch {
    const lines = content.split('\n').map(line => line.trim()).filter(Boolean)
    if (lines.length === 0) return false
    try {
      return lines.every(line => isAgentIdentityValue(JSON.parse(line)))
    } catch {
      return false
    }
  }
}

async function handleOpenAIImportCodexSession(content: string) {
  if (form.platform !== 'openai') return
  const trimmed = content.trim()
  if (!trimmed) {
    openaiOAuth.error.value = t('admin.accounts.oauth.openai.codexSessionEmpty')
    return
  }
  if (oauthFlowRef.value?.inputMethod === 'agent_identity' && !isAgentIdentityImport(trimmed)) {
    openaiOAuth.error.value = t('admin.accounts.oauth.openai.agentIdentityInvalid')
    return
  }
  const credentialExtras = buildOpenAICodexImportCredentialExtras()
  if (credentialExtras === null) return
  openaiOAuth.loading.value = true
  openaiOAuth.error.value = ''
  try {
    const result = await adminAPI.accounts.importCodexSession({
      content: trimmed,
      name: form.name.trim(),
      notes: form.notes.trim() || null,
      proxy_id: form.proxy_id,
      concurrency: form.concurrency,
      priority: form.priority,
      rate_multiplier: form.rate_multiplier,
      group_ids: form.group_ids,
      load_factor: loadFactor.value,
      expires_at: expiresAt.value,
      auto_pause_on_expired: autoPauseOnExpired.value,
      credential_extras: Object.keys(credentialExtras).length ? credentialExtras : undefined,
      extra: buildQuotaExtra(buildOpenAICodexImportExtra()),
      update_existing: true
    })
    const successCount = result.created + result.updated
    const params = {
      created: result.created,
      updated: result.updated,
      skipped: result.skipped,
      failed: result.failed
    }
    if (successCount > 0 && result.failed === 0) {
      appStore.showSuccess(t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
      emit('created')
      handleClose()
      return
    }

    openaiOAuth.error.value = [
      formatCodexImportMessages(result.errors),
      formatCodexImportMessages(result.warnings)
    ].filter(Boolean).join('\n')
    if (successCount > 0) {
      appStore.showWarning(t('admin.accounts.oauth.openai.codexSessionImportPartial', params))
      emit('created')
    } else if (result.failed === 0) {
      appStore.showWarning(t('admin.accounts.oauth.openai.codexSessionImportSuccess', params))
    } else {
      appStore.showError(t('admin.accounts.oauth.openai.codexSessionImportFailed'))
    }
  } catch (error: any) {
    openaiOAuth.error.value = error.response?.data?.detail || error.response?.data?.message || error.message
    appStore.showError(openaiOAuth.error.value || t('admin.accounts.oauth.openai.codexSessionImportFailed'))
  } finally {
    openaiOAuth.loading.value = false
  }
}

async function handleOpenAIImportCodexPAT(accessToken: string) {
  if (form.platform !== 'openai') return
  const trimmed = accessToken.trim()
  if (!trimmed) {
    openaiOAuth.error.value = t('admin.accounts.oauth.openai.codexPatEmpty')
    return
  }
  const credentialExtras = buildOpenAICodexImportCredentialExtras()
  if (credentialExtras === null) return
  openaiOAuth.loading.value = true
  openaiOAuth.error.value = ''
  try {
    await adminAPI.accounts.createOpenAICodexPAT({
      access_token: trimmed,
      name: form.name.trim(),
      notes: form.notes.trim() || null,
      proxy_id: form.proxy_id,
      concurrency: form.concurrency,
      priority: form.priority,
      rate_multiplier: form.rate_multiplier,
      group_ids: form.group_ids,
      load_factor: loadFactor.value,
      expires_at: expiresAt.value,
      auto_pause_on_expired: autoPauseOnExpired.value,
      credential_extras: Object.keys(credentialExtras).length ? credentialExtras : undefined,
      extra: buildQuotaExtra(buildOpenAICodexImportExtra())
    })
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } catch (error: any) {
    openaiOAuth.error.value = error.response?.data?.detail || error.response?.data?.message || error.message
    appStore.showError(openaiOAuth.error.value || t('admin.accounts.oauth.openai.codexPatImportFailed'))
  } finally {
    openaiOAuth.loading.value = false
  }
}

function resetForm() {
  step.value = 1
  form.name = ''
  form.notes = ''
  form.platform = 'anthropic'
  form.proxy_id = null
  form.concurrency = 10
  form.priority = 1
  form.rate_multiplier = 1
  form.group_ids = []
  accountCategory.value = 'oauth'
  apiKeyValue.value = ''
  apiKeyBaseUrl.value = 'https://api.anthropic.com'
  allowedModels.value = []
  upstreamBillingProbeEnabled.value = true
  headerOverrideEnabled.value = false
  headerOverrideRows.value = []
  bedrockAuthMode.value = 'sigv4'
  bedrockAccessKeyId.value = ''
  bedrockSecretAccessKey.value = ''
  bedrockSessionToken.value = ''
  bedrockApiKey.value = ''
  bedrockRegion.value = 'us-east-1'
  bedrockForceGlobal.value = false
  vertexServiceAccountJson.value = ''
  vertexProjectId.value = ''
  vertexClientEmail.value = ''
  vertexServiceAccountDragActive.value = false
  vertexLocation.value = 'global'
  openAILongContextBillingEnabled.value = false
  openAILongContextBillingTouched.value = false
  openAIFlattenNamespaces.value = false
  openAIWSMode.value = 'off'
  modelRestrictionMode.value = 'whitelist'
  modelMappings.value = []
  openAICompactModelMappings.value = []
  poolModeEnabled.value = false
  poolModeRetryCount.value = 3
  poolModeRetryStatusCodesInput.value = ''
  customErrorCodesEnabled.value = false
  selectedErrorCodes.value = []
  customErrorCodeInput.value = null
  interceptWarmupRequests.value = false
  tempUnschedEnabled.value = false
  tempUnschedRules.value = []
  autoPauseOnExpired.value = true
  autoPause5hThresholdPercent.value = null
  autoPause7dThresholdPercent.value = null
  autoPause5hDisabled.value = false
  autoPause7dDisabled.value = false
  loadFactor.value = null
  expiresAt.value = null
  openaiPassthroughEnabled.value = false
  openAICodexCLIOnlyEnabled.value = false
  openAICodexCLIOnlyAppServerEnabled.value = false
  openAICodexFingerprintMode.value = 'off'
  openAICompactMode.value = 'auto'
  openAIResponsesMode.value = 'auto'
  openAIEndpointCapabilities.value = ['chat_completions', 'embeddings']
  codexImageToolMode.value = 'inherit'
  openAIPlanType.value = ''
  anthropicPassthroughEnabled.value = false
  anthropicAPIKeyAuthScheme.value = 'x_api_key'
  webSearchEmulationMode.value = 'default'
  windowCostEnabled.value = false
  windowCostLimit.value = null
  windowCostStickyReserve.value = null
  sessionLimitEnabled.value = false
  maxSessions.value = null
  sessionIdleTimeout.value = null
  rpmLimitEnabled.value = false
  baseRpm.value = null
  rpmStrategy.value = 'tiered'
  rpmStickyBuffer.value = null
  userMsgQueueMode.value = ''
  tlsFingerprintEnabled.value = false
  tlsFingerprintProfileId.value = null
  sessionIdMaskingEnabled.value = false
  cacheTTLOverrideEnabled.value = false
  cacheTTLOverrideTarget.value = '5m'
  customBaseUrlEnabled.value = false
  customBaseUrl.value = ''
  quotaLimit.value = null
  quotaDailyLimit.value = null
  quotaWeeklyLimit.value = null
  quotaDailyResetMode.value = 'rolling'
  quotaDailyResetHour.value = 0
  quotaWeeklyResetMode.value = 'rolling'
  quotaWeeklyResetDay.value = 1
  quotaWeeklyResetHour.value = 0
  quotaResetTimezone.value = 'UTC'
  anthropicOAuth.resetState()
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

function goBackToBasicInfo() {
  step.value = 1
  anthropicOAuth.resetState()
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

function handleClose() {
  emit('close')
}
</script>
