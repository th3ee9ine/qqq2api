<template>
  <div
    class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-700 dark:bg-blue-900/30"
  >
    <div class="flex items-start gap-4">
      <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-blue-500">
        <Icon name="link" size="md" class="text-white" />
      </div>
      <div class="flex-1">
        <h4 class="mb-3 font-semibold text-blue-900 dark:text-blue-200">{{ oauthTitle }}</h4>

        <!-- Auth Method Selection -->
        <div v-if="showMethodSelection" class="mb-4">
          <label class="mb-2 block text-sm font-medium text-blue-800 dark:text-blue-300">
            {{ methodLabel }}
          </label>
          <div class="flex flex-wrap gap-4">
            <label v-if="showManualOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="manual" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.manualAuth') }}</span>
            </label>
            <label v-if="showCookieOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="cookie" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.cookieAutoAuth') }}</span>
            </label>
            <label v-if="showRefreshTokenOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="refresh_token" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t(oauthKey('refreshTokenAuth')) }}</span>
            </label>
            <label v-if="showMobileRefreshTokenOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="mobile_refresh_token" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.openai.mobileRefreshTokenAuth') }}</span>
            </label>
            <label v-if="showSessionTokenOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="session_token" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t(oauthKey('sessionTokenAuth')) }}</span>
            </label>
            <label v-if="showAccessTokenOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="access_token" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.openai.accessTokenAuth') }}</span>
            </label>
            <label v-if="showCodexSessionImportOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="codex_session" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.openai.codexSessionAuth') }}</span>
            </label>
            <label v-if="showAgentIdentityOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="agent_identity" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.openai.agentIdentityAuth') }}</span>
            </label>
            <label v-if="showCodexPatOption" class="flex cursor-pointer items-center gap-2">
              <input v-model="inputMethod" type="radio" value="codex_pat" class="text-blue-600 focus:ring-blue-500" />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ t('admin.accounts.oauth.openai.codexPatAuth') }}</span>
            </label>
          </div>
        </div>

        <!-- Refresh Token Input (OpenAI / Mobile RT) -->
        <div v-if="isRefreshTokenInput" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ t(oauthKey('refreshTokenDesc')) }}</p>
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                Refresh Token
                <span v-if="parsedRefreshTokenCount > 1" class="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                  {{ t('admin.accounts.oauth.keysCount', { count: parsedRefreshTokenCount }) }}
                </span>
              </label>
              <textarea
                v-model="refreshTokenInput"
                rows="3"
                class="input w-full resize-y font-mono text-sm"
                :placeholder="t(oauthKey('refreshTokenPlaceholder'))"
              ></textarea>
              <p v-if="parsedRefreshTokenCount > 1" class="mt-1 text-xs text-blue-600 dark:text-blue-400">
                {{ t('admin.accounts.oauth.batchCreateAccounts', { count: parsedRefreshTokenCount }) }}
              </p>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !refreshTokenInput.trim()" @click="handleValidateRefreshToken">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t(oauthKey('validating')) : t(oauthKey('validateAndCreate')) }}
            </button>
          </div>
        </div>

        <!-- Session Token Input -->
        <div v-if="inputMethod === 'session_token'" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ t(oauthKey('sessionTokenDesc')) }}</p>
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                {{ t(oauthKey('sessionTokenAuth')) }}
              </label>
              <textarea v-model="sessionTokenInput" rows="4" class="input w-full resize-y font-mono text-sm" :placeholder="t(oauthKey('sessionTokenPlaceholder'))" spellcheck="false"></textarea>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !sessionTokenInput.trim()" @click="emit('validate-session-token', sessionTokenInput.trim())">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t(oauthKey('validating')) : t(oauthKey('validateAndCreate')) }}
            </button>
          </div>
        </div>

        <!-- Access Token Input -->
        <div v-if="inputMethod === 'access_token'" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                {{ t('admin.accounts.oauth.openai.accessTokenAuth') }}
              </label>
              <textarea v-model="accessTokenInput" rows="4" class="input w-full resize-y font-mono text-sm" spellcheck="false"></textarea>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !accessTokenInput.trim()" @click="emit('import-access-token', accessTokenInput.trim())">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t(oauthKey('validating')) : t(oauthKey('validateAndCreate')) }}
            </button>
          </div>
        </div>

        <!-- Codex auth.json / session credential batch import -->
        <div v-if="inputMethod === 'codex_session' || inputMethod === 'agent_identity'" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ t(codexSessionDescriptionKey) }}</p>
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                {{ t(codexSessionInputLabelKey) }}
                <span v-if="parsedCodexSessionCount > 1" class="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                  {{ t('admin.accounts.oauth.keysCount', { count: parsedCodexSessionCount }) }}
                </span>
              </label>
              <textarea v-model="codexSessionInput" rows="8" class="input w-full resize-y font-mono text-sm" :placeholder="t(codexSessionPlaceholderKey)" spellcheck="false"></textarea>
              <p class="mt-1 text-xs text-blue-600 dark:text-blue-400">{{ t(codexSessionHintKey) }}</p>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !codexSessionInput.trim()" @click="emit('import-codex-session', codexSessionInput.trim())">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t('admin.accounts.oauth.openai.validating') : t('admin.accounts.oauth.openai.codexSessionImportAndCreate') }}
            </button>
          </div>
        </div>

        <!-- Codex Personal Access Token -->
        <div v-if="inputMethod === 'codex_pat'" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ t('admin.accounts.oauth.openai.codexPatDesc') }}</p>
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                {{ t('admin.accounts.oauth.openai.codexPatInputLabel') }}
              </label>
              <textarea v-model="codexPATInput" rows="3" class="input w-full resize-y font-mono text-sm" :placeholder="t('admin.accounts.oauth.openai.codexPatPlaceholder')" spellcheck="false"></textarea>
              <p class="mt-1 text-xs text-blue-600 dark:text-blue-400">{{ t('admin.accounts.oauth.openai.codexPatHint') }}</p>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !codexPATInput.trim()" @click="emit('import-codex-pat', codexPATInput.trim())">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t('admin.accounts.oauth.openai.validating') : t('admin.accounts.oauth.openai.codexPatImportAndCreate') }}
            </button>
          </div>
        </div>

        <!-- Cookie Auto-Auth Form -->
        <div v-if="inputMethod === 'cookie'" class="space-y-4">
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-3 text-sm text-blue-700 dark:text-blue-300">{{ t('admin.accounts.oauth.cookieAutoAuthDesc') }}</p>
            <div class="mb-4">
              <label class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-700 dark:text-gray-300">
                <Icon name="key" size="sm" class="text-blue-500" />
                {{ t('admin.accounts.oauth.sessionKey') }}
                <span v-if="parsedSessionKeyCount > 1 && allowMultiple" class="rounded-full bg-blue-500 px-2 py-0.5 text-xs text-white">
                  {{ t('admin.accounts.oauth.keysCount', { count: parsedSessionKeyCount }) }}
                </span>
                <button
                  v-if="showHelp"
                  type="button"
                  class="text-blue-500 hover:text-blue-600"
                  data-testid="session-key-help"
                  :aria-expanded="showHelpDialog"
                  @click="showHelpDialog = !showHelpDialog"
                >
                  <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z" />
                  </svg>
                </button>
              </label>
              <textarea
                v-model="sessionKeyInput"
                rows="3"
                class="input w-full resize-y font-mono text-sm"
                :placeholder="allowMultiple ? t('admin.accounts.oauth.sessionKeyPlaceholder') : t('admin.accounts.oauth.sessionKeyPlaceholderSingle')"
              ></textarea>
              <p v-if="parsedSessionKeyCount > 1 && allowMultiple" class="mt-1 text-xs text-blue-600 dark:text-blue-400">
                {{ t('admin.accounts.oauth.batchCreateAccounts', { count: parsedSessionKeyCount }) }}
              </p>
            </div>
            <div v-if="showHelpDialog && showHelp" class="mb-4 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-900/30">
              <h5 class="mb-2 font-semibold text-amber-800 dark:text-amber-200">{{ t('admin.accounts.oauth.howToGetSessionKey') }}</h5>
              <ol class="list-inside list-decimal space-y-1 text-xs text-amber-700 dark:text-amber-300">
                <li>{{ t('admin.accounts.oauth.step1') }}</li>
                <li>{{ t('admin.accounts.oauth.step2') }}</li>
                <li>{{ t('admin.accounts.oauth.step3') }}</li>
                <li>{{ t('admin.accounts.oauth.step4') }}</li>
                <li>{{ t('admin.accounts.oauth.step5') }}</li>
                <li>{{ t('admin.accounts.oauth.step6') }}</li>
              </ol>
              <p class="mt-2 text-xs text-amber-600 dark:text-amber-400" v-text="t('admin.accounts.oauth.sessionKeyFormat')"></p>
            </div>
            <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
              <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            </div>
            <button type="button" class="btn btn-primary w-full" :disabled="loading || !sessionKeyInput.trim()" @click="handleCookieAuth">
              <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <Icon v-else name="sparkles" size="sm" class="mr-2" />
              {{ loading ? t('admin.accounts.oauth.authorizing') : t('admin.accounts.oauth.startAutoAuth') }}
            </button>
          </div>
        </div>

        <!-- Manual Authorization Flow -->
        <div v-if="inputMethod === 'manual'" class="space-y-4">
          <p class="mb-4 text-sm text-blue-800 dark:text-blue-300">{{ t(oauthKey('followSteps')) }}</p>
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <div class="flex items-start gap-3">
              <div class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">1</div>
              <div class="flex-1">
                <p class="mb-2 font-medium text-blue-900 dark:text-blue-200">{{ t(oauthKey('step1GenerateUrl')) }}</p>
                <button v-if="!authUrl" type="button" :disabled="loading" class="btn btn-primary text-sm" @click="emit('generate-url')">
                  <svg v-if="loading" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  <Icon v-else name="link" size="sm" class="mr-2" />
                  {{ loading ? t('admin.accounts.oauth.generating') : t(oauthKey('generateAuthUrl')) }}
                </button>
                <div v-else class="space-y-3">
                  <div class="flex items-center gap-2">
                    <input :value="authUrl" readonly type="text" class="input flex-1 bg-gray-50 font-mono text-xs dark:bg-gray-700" />
                    <button type="button" class="btn btn-secondary p-2" title="Copy URL" @click="copyAuthorizationUrl">
                      <svg v-if="!copied" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" />
                      </svg>
                      <Icon v-else name="check" size="sm" class="text-green-500" :stroke-width="2" />
                    </button>
                  </div>
                  <button type="button" class="text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400" data-testid="regenerate-oauth-url" @click="regenerateAuthorizationUrl">
                    <Icon name="refresh" size="xs" class="mr-1 inline" />
                    {{ t('admin.accounts.oauth.regenerate') }}
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <div class="flex items-start gap-3">
              <div class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">2</div>
              <div class="flex-1">
                <p class="mb-2 font-medium text-blue-900 dark:text-blue-200">{{ t(oauthKey('step2OpenUrl')) }}</p>
                <p class="text-sm text-blue-700 dark:text-blue-300">{{ t(oauthKey('openUrlDesc')) }}</p>
                <div v-if="platform === 'openai'" class="mt-2 rounded border border-amber-300 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-900/30">
                  <p class="text-xs text-amber-800 dark:text-amber-300" v-text="t('admin.accounts.oauth.openai.importantNotice')"></p>
                </div>
                <div v-else-if="showProxyWarning" class="mt-2 rounded border border-yellow-300 bg-yellow-50 p-3 dark:border-yellow-700 dark:bg-yellow-900/30">
                  <p class="text-xs text-yellow-800 dark:text-yellow-300" v-text="t('admin.accounts.oauth.proxyWarning')"></p>
                </div>
              </div>
            </div>
          </div>

          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <div class="flex items-start gap-3">
              <div class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">3</div>
              <div class="flex-1">
                <p class="mb-2 font-medium text-blue-900 dark:text-blue-200">{{ t(oauthKey('step3EnterCode')) }}</p>
                <p class="mb-3 text-sm text-blue-700 dark:text-blue-300" v-text="t(oauthKey('authCodeDesc'))"></p>
                <div>
                  <label class="input-label">
                    <Icon name="key" size="sm" class="mr-1 inline text-blue-500" />
                    {{ t(oauthKey('authCode')) }}
                  </label>
                  <textarea v-model="authCodeInput" rows="3" class="input w-full resize-none font-mono text-sm" :placeholder="t(oauthKey('authCodePlaceholder'))"></textarea>
                  <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                    <Icon name="infoCircle" size="xs" class="mr-1 inline" />
                    {{ t(oauthKey('authCodeHint')) }}
                  </p>
                </div>
                <div v-if="error" class="mt-3 rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-700 dark:bg-red-900/30">
                  <p class="whitespace-pre-line text-sm text-red-600 dark:text-red-400">{{ error }}</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { AddMethod, AuthInputMethod } from '@/composables/useAccountOAuth'
import Icon from '@/components/icons/Icon.vue'

type SupportedOAuthPlatform = 'anthropic' | 'openai'

const props = withDefaults(defineProps<{
  addMethod: AddMethod
  authUrl?: string
  sessionId?: string
  loading?: boolean
  error?: string
  showHelp?: boolean
  showProxyWarning?: boolean
  allowMultiple?: boolean
  methodLabel?: string
  showCookieOption?: boolean
  showRefreshTokenOption?: boolean
  showMobileRefreshTokenOption?: boolean
  showSessionTokenOption?: boolean
  showAccessTokenOption?: boolean
  showCodexSessionImportOption?: boolean
  showAgentIdentityOption?: boolean
  showCodexPatOption?: boolean
  showManualOption?: boolean
  initialInputMethod?: AuthInputMethod
  platform?: SupportedOAuthPlatform
}>(), {
  authUrl: '',
  sessionId: '',
  loading: false,
  error: '',
  showHelp: true,
  showProxyWarning: true,
  allowMultiple: false,
  methodLabel: 'Authorization Method',
  showCookieOption: true,
  showRefreshTokenOption: false,
  showMobileRefreshTokenOption: false,
  showSessionTokenOption: false,
  showAccessTokenOption: false,
  showCodexSessionImportOption: false,
  showAgentIdentityOption: false,
  showCodexPatOption: false,
  showManualOption: true,
  initialInputMethod: 'manual',
  platform: 'anthropic'
})

const emit = defineEmits<{
  'generate-url': []
  'cookie-auth': [sessionKey: string]
  'validate-refresh-token': [refreshToken: string]
  'validate-mobile-refresh-token': [refreshToken: string]
  'validate-session-token': [sessionToken: string]
  'import-access-token': [accessToken: string]
  'import-codex-session': [content: string]
  'import-codex-pat': [accessToken: string]
  'update:inputMethod': [method: AuthInputMethod]
}>()

const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const inputMethod = ref<AuthInputMethod>(props.initialInputMethod)
const authCodeInput = ref('')
const oauthState = ref('')
const sessionKeyInput = ref('')
const refreshTokenInput = ref('')
const sessionTokenInput = ref('')
const accessTokenInput = ref('')
const codexSessionInput = ref('')
const codexPATInput = ref('')
const showHelpDialog = ref(false)

const isRefreshTokenInput = computed(() =>
  inputMethod.value === 'refresh_token' || inputMethod.value === 'mobile_refresh_token'
)
const parsedSessionKeyCount = computed(() => countNonBlankLines(sessionKeyInput.value))
const parsedRefreshTokenCount = computed(() => countNonBlankLines(refreshTokenInput.value))
const parsedCodexSessionCount = computed(() => {
  const value = codexSessionInput.value.trim()
  if (!value) return 0
  if (value.startsWith('{') || value.startsWith('[')) return 1
  return countNonBlankLines(value)
})
const isAgentIdentityInput = computed(() => inputMethod.value === 'agent_identity')
const codexSessionDescriptionKey = computed(() => isAgentIdentityInput.value
  ? 'admin.accounts.oauth.openai.agentIdentityDesc'
  : 'admin.accounts.oauth.openai.codexSessionDesc')
const codexSessionInputLabelKey = computed(() => isAgentIdentityInput.value
  ? 'admin.accounts.oauth.openai.agentIdentityInputLabel'
  : 'admin.accounts.oauth.openai.codexSessionInputLabel')
const codexSessionPlaceholderKey = computed(() => isAgentIdentityInput.value
  ? 'admin.accounts.oauth.openai.agentIdentityPlaceholder'
  : 'admin.accounts.oauth.openai.codexSessionPlaceholder')
const codexSessionHintKey = computed(() => isAgentIdentityInput.value
  ? 'admin.accounts.oauth.openai.agentIdentityHint'
  : 'admin.accounts.oauth.openai.codexSessionHint')

function countNonBlankLines(value: string) {
  return value.split('\n').map(item => item.trim()).filter(Boolean).length
}

const oauthKey = (key: string) =>
  props.platform === 'openai' ? 'admin.accounts.oauth.openai.' + key : 'admin.accounts.oauth.' + key
const oauthTitle = computed(() => t(oauthKey('title')))
const methodOptionCount = computed(() => [
  props.showManualOption,
  props.showCookieOption,
  props.showRefreshTokenOption,
  props.showMobileRefreshTokenOption,
  props.showSessionTokenOption,
  props.showAccessTokenOption,
  props.showCodexSessionImportOption,
  props.showAgentIdentityOption,
  props.showCodexPatOption
].filter(Boolean).length)
const showMethodSelection = computed(() => methodOptionCount.value > 1)

watch(() => props.initialInputMethod, value => { inputMethod.value = value })
watch(inputMethod, value => emit('update:inputMethod', value))
watch(authCodeInput, value => {
  if (props.platform !== 'openai' || !value.includes('code=')) return
  try {
    const parsed = new URL(value.includes('?') ? value : 'http://localhost/callback?' + value.replace(/^\?/, ''))
    const code = parsed.searchParams.get('code')
    oauthState.value = parsed.searchParams.get('state') || oauthState.value
    if (code) authCodeInput.value = code
  } catch {
    const code = value.match(/[?&]code=([^&]+)/)?.[1]
    const state = value.match(/[?&]state=([^&]+)/)?.[1]
    if (state) oauthState.value = state
    if (code) authCodeInput.value = code
  }
})

function handleValidateRefreshToken() {
  const token = refreshTokenInput.value.trim()
  if (!token) return
  if (inputMethod.value === 'mobile_refresh_token') {
    emit('validate-mobile-refresh-token', token)
  } else {
    emit('validate-refresh-token', token)
  }
}

function handleCookieAuth() {
  if (sessionKeyInput.value.trim()) emit('cookie-auth', sessionKeyInput.value)
}

function copyAuthorizationUrl() {
  if (props.authUrl) copyToClipboard(props.authUrl, 'URL copied to clipboard')
}

function regenerateAuthorizationUrl() {
  authCodeInput.value = ''
  emit('generate-url')
}

function reset() {
  authCodeInput.value = ''
  oauthState.value = ''
  sessionKeyInput.value = ''
  refreshTokenInput.value = ''
  sessionTokenInput.value = ''
  accessTokenInput.value = ''
  codexSessionInput.value = ''
  codexPATInput.value = ''
  showHelpDialog.value = false
  inputMethod.value = props.initialInputMethod
}

defineExpose({
  authCode: authCodeInput,
  oauthState,
  sessionKey: sessionKeyInput,
  refreshToken: refreshTokenInput,
  sessionToken: sessionTokenInput,
  accessToken: accessTokenInput,
  codexSession: codexSessionInput,
  codexPAT: codexPATInput,
  inputMethod,
  reset
})
</script>
