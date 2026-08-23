<template>
  <header class="glass sticky top-0 z-30 border-b border-gray-200/50 dark:border-dark-700/50">
    <div class="flex h-16 items-center justify-between gap-2 px-2 sm:px-4 md:px-6">
      <div class="flex shrink-0 items-center gap-2 sm:gap-4">
        <button
          class="btn-ghost btn-icon lg:hidden"
          :aria-label="t('common.toggleMenu')"
          @click="appStore.toggleMobileSidebar()"
        >
          <Icon name="menu" size="md" />
        </button>
        <div class="hidden lg:block">
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ pageTitle }}</h1>
          <p v-if="pageDescription" class="text-xs text-gray-500 dark:text-dark-400">
            {{ pageDescription }}
          </p>
        </div>
      </div>

      <div class="flex min-w-0 items-center gap-1 sm:gap-3">
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="hidden items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white sm:flex"
        >
          <Icon name="book" size="sm" />
          <span>{{ t('nav.docs') }}</span>
        </a>
        <LocaleSwitcher />

        <div v-if="user" ref="dropdownRef" class="relative">
          <button
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
            :aria-label="t('common.userMenu')"
            @click="dropdownOpen = !dropdownOpen"
          >
            <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
              <img v-if="avatarUrl" :src="avatarUrl" :alt="displayName" class="h-full w-full object-cover" />
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="hidden text-left md:block">
              <div class="text-sm font-medium text-gray-900 dark:text-white">{{ displayName }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.users.roles.admin') }}</div>
            </div>
            <Icon name="chevronDown" size="sm" class="hidden text-gray-400 md:block" />
          </button>

          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56">
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-medium text-gray-900 dark:text-white">{{ displayName }}</div>
                <div class="text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
              </div>
              <div class="py-1">
                <router-link to="/keys" class="dropdown-item" @click="closeDropdown">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>
                <a
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="dropdown-item"
                  @click="closeDropdown"
                >
                  <Icon name="externalLink" size="sm" />
                  {{ t('nav.github') }}
                </a>
              </div>
              <div v-if="showOnboardingButton" class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button class="dropdown-item w-full" @click="handleReplayGuide">
                  <Icon name="questionCircle" size="sm" />
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>
              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="handleLogout"
                >
                  <Icon name="login" size="sm" />
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()

const user = computed(() => authStore.user)
const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const displayName = computed(() => user.value?.username || user.value?.email?.split('@')[0] || '')
const userInitials = computed(() => displayName.value.substring(0, 2).toUpperCase())
const showOnboardingButton = computed(() => !authStore.isSimpleMode)

const pageTitle = computed(() => {
  const titleKey = route.meta.titleKey as string
  return titleKey ? t(titleKey) : (route.meta.title as string) || ''
})

const pageDescription = computed(() => {
  const descriptionKey = route.meta.descriptionKey as string
  return descriptionKey ? t(descriptionKey) : (route.meta.description as string) || ''
})

function closeDropdown() {
  dropdownOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } catch (error) {
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) closeDropdown()
}

onMounted(() => document.addEventListener('click', handleClickOutside))
onBeforeUnmount(() => document.removeEventListener('click', handleClickOutside))
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
