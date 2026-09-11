<template>
  <AppLayout>
    <TablePageLayout class="admin-workspace account-admins-page">
      <template #actions>
        <AdminPageHeader
          :eyebrow="t('admin.accountAdmins.eyebrow')"
          :title="t('admin.accountAdmins.title')"
          :description="t('admin.accountAdmins.description')"
        >
          <template #actions>
            <button
              type="button"
              class="admin-icon-button"
              :disabled="loading"
              :title="t('common.refresh')"
              :aria-label="t('common.refresh')"
              @click="loadAccountAdmins"
            >
              <Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreate">
              <Icon name="userPlus" size="md" class="mr-2" />
              {{ t('admin.accountAdmins.create') }}
            </button>
          </template>
        </AdminPageHeader>
        <AdminOverviewStrip :items="overviewItems" :loading="loading" class="mt-5" />
      </template>

      <template #filters>
        <div class="admin-toolbar account-admins-toolbar">
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.accountAdmins.directory') }}</h2>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ t('admin.accountAdmins.directoryHint') }}
            </p>
          </div>
          <form class="flex w-full min-w-0 flex-wrap items-center gap-2 xl:w-auto" @submit.prevent="applyFilters">
            <div class="relative min-w-0 flex-1 sm:w-64 sm:flex-none">
              <Icon
                name="search"
                size="md"
                class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="filters.search"
                type="search"
                class="input pl-10"
                :aria-label="t('admin.accountAdmins.searchPlaceholder')"
                :placeholder="t('admin.accountAdmins.searchPlaceholder')"
              />
            </div>
            <button type="submit" class="btn btn-secondary" :disabled="loading">
              {{ t('common.search') }}
            </button>
            <div class="w-full sm:w-36">
              <Select
                v-model="filters.status"
                :options="statusFilterOptions"
                :searchable="false"
                :aria-label="t('admin.accountAdmins.statusFilter')"
                @change="applyFilters"
              />
            </div>
            <button
              v-if="hasFilters"
              type="button"
              class="admin-row-action px-3"
              @click="resetFilters"
            >
              {{ t('admin.accountAdmins.clearFilters') }}
            </button>
          </form>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="accountAdmins" :loading="loading" row-key="id">
          <template #cell-identity="{ row }">
            <div class="flex min-w-0 items-center gap-3 text-left">
              <div class="admin-avatar" aria-hidden="true">
                {{ (row.username || row.email).slice(0, 2).toUpperCase() }}
              </div>
              <div class="min-w-0 max-w-[15rem]">
                <div class="truncate font-semibold text-gray-900 dark:text-white" :title="row.username || row.email.split('@')[0]">
                  {{ row.username || row.email.split('@')[0] }}
                </div>
                <div class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400" :title="row.email">{{ row.email }}</div>
                <div v-if="row.notes" class="mt-1 truncate text-xs text-gray-400 dark:text-dark-500" :title="row.notes">
                  {{ row.notes }}
                </div>
              </div>
            </div>
          </template>

          <template #cell-role>
            <div>
              <span class="inline-flex items-center gap-1.5 text-xs font-medium text-primary-700 dark:text-primary-300">
                <Icon name="shield" size="sm" />
                {{ t('admin.users.roles.account_admin') }}
              </span>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAdmins.permissionScope') }}</p>
            </div>
          </template>

          <template #cell-status="{ row }">
            <span
              class="badge"
              :class="row.status === 'active' ? 'badge-success' : 'badge-gray'"
            >
              <span class="mr-1.5 h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true"></span>
              {{ row.status === 'active' ? t('common.active') : t('common.disabled') }}
            </span>
          </template>

          <template #cell-last_active_at="{ value }">
            <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ value ? formatDateTime(value) : t('admin.accountAdmins.neverActive') }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center justify-end gap-1">
              <button type="button" class="admin-row-action" @click="openEdit(row)">
                <Icon name="edit" size="sm" />
                {{ t('common.edit') }}
              </button>
              <button
                type="button"
                class="admin-row-action"
                :disabled="statusChangingId === row.id"
                @click="toggleStatus(row)"
              >
                {{ row.status === 'active'
                  ? t('admin.accountAdmins.disable')
                  : t('admin.accountAdmins.enable') }}
              </button>
              <button
                type="button"
                class="admin-icon-button admin-delete-action"
                :title="t('admin.accountAdmins.deleteFor', { email: row.email })"
                :aria-label="t('admin.accountAdmins.deleteFor', { email: row.email })"
                @click="deletingAccountAdmin = row"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <div class="admin-empty-state">
              <div class="mb-5 flex h-14 w-14 items-center justify-center rounded-2xl border border-primary-100 bg-primary-50 text-primary-600 dark:border-primary-900/60 dark:bg-primary-900/20 dark:text-primary-400">
                <Icon :name="hasFilters ? 'search' : 'users'" size="xl" />
              </div>
              <p class="font-semibold text-gray-800 dark:text-gray-100">
                {{ t(hasFilters ? 'admin.accountAdmins.noMatches' : 'admin.accountAdmins.empty') }}
              </p>
              <p class="mt-2 max-w-sm whitespace-normal text-sm leading-6 text-gray-500 dark:text-dark-400">
                {{ t(hasFilters ? 'admin.accountAdmins.noMatchesHint' : 'admin.accountAdmins.emptyHint') }}
              </p>
              <button v-if="hasFilters" type="button" class="btn btn-secondary mt-5" @click="resetFilters">
                {{ t('admin.accountAdmins.clearFilters') }}
              </button>
              <button v-else type="button" class="btn btn-primary mt-5" @click="openCreate">
                <Icon name="userPlus" size="md" class="mr-2" />
                {{ t('admin.accountAdmins.create') }}
              </button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          @update:page="handlePageChange"
          @update:page-size="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="formDialogOpen"
      :title="editingAccountAdmin
        ? t('admin.accountAdmins.edit')
        : t('admin.accountAdmins.create')"
      width="normal"
      @close="closeFormDialog"
    >
      <form id="account-admin-form" class="space-y-5" @submit.prevent="submitForm">
        <div class="flex items-start gap-3 rounded-xl border border-primary-100 bg-primary-50/60 p-4 dark:border-primary-900/50 dark:bg-primary-900/15">
          <Icon name="shield" size="md" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-400" />
          <div>
            <p class="text-sm font-medium text-primary-900 dark:text-primary-200">{{ t('admin.accountAdmins.permissionTitle') }}</p>
            <p class="mt-1 text-xs leading-5 text-primary-700 dark:text-primary-300">{{ t('admin.accountAdmins.passwordHint') }}</p>
          </div>
        </div>
        <div>
          <label for="account-admin-email" class="input-label">{{ t('admin.accountAdmins.email') }} <span class="text-primary-600" aria-hidden="true">*</span></label>
          <input
            id="account-admin-email"
            v-model.trim="form.email"
            type="email"
            required
            class="input"
            autocomplete="off"
            :placeholder="t('admin.accountAdmins.emailPlaceholder')"
          />
        </div>
        <div>
          <label for="account-admin-username" class="input-label">{{ t('admin.accountAdmins.username') }}</label>
          <input
            id="account-admin-username"
            v-model.trim="form.username"
            type="text"
            maxlength="100"
            class="input"
            :placeholder="t('admin.accountAdmins.usernamePlaceholder')"
          />
        </div>
        <div>
          <label for="account-admin-password" class="input-label">{{ t('admin.accountAdmins.password') }} <span v-if="!editingAccountAdmin" class="text-primary-600" aria-hidden="true">*</span></label>
          <div class="flex gap-2">
            <input
              id="account-admin-password"
              v-model="form.password"
              :type="passwordVisible ? 'text' : 'password'"
              :required="!editingAccountAdmin"
              minlength="6"
              maxlength="72"
              class="input min-w-0 flex-1"
              autocomplete="new-password"
              aria-describedby="account-admin-password-hint"
              :placeholder="editingAccountAdmin
                ? t('admin.accountAdmins.passwordEditPlaceholder')
                : t('admin.accountAdmins.passwordPlaceholder')"
            />
            <button
              type="button"
              class="admin-icon-button shrink-0"
              :title="t(passwordVisible ? 'admin.accountAdmins.hidePassword' : 'admin.accountAdmins.showPassword')"
              :aria-label="t(passwordVisible ? 'admin.accountAdmins.hidePassword' : 'admin.accountAdmins.showPassword')"
              :aria-pressed="passwordVisible"
              @click="passwordVisible = !passwordVisible"
            >
              <Icon :name="passwordVisible ? 'eyeOff' : 'eye'" size="md" />
            </button>
            <button
              type="button"
              class="admin-icon-button shrink-0"
              :title="t('admin.accountAdmins.generatePassword')"
              :aria-label="t('admin.accountAdmins.generatePassword')"
              @click="generatePassword"
            >
              <Icon name="refresh" size="md" />
            </button>
          </div>
          <p id="account-admin-password-hint" class="input-hint">{{ t(editingAccountAdmin ? 'admin.accountAdmins.passwordEditPlaceholder' : 'admin.accountAdmins.passwordRequirement') }}</p>
        </div>
        <div v-if="editingAccountAdmin">
          <label for="account-admin-status" class="input-label">{{ t('common.status') }}</label>
          <Select id="account-admin-status" v-model="form.status" :options="statusOptions" :searchable="false" :aria-label="t('common.status')" />
        </div>
        <div>
          <label for="account-admin-notes" class="input-label">{{ t('admin.accountAdmins.notes') }}</label>
          <textarea
            id="account-admin-notes"
            v-model="form.notes"
            rows="3"
            class="input"
            :placeholder="t('admin.accountAdmins.notesPlaceholder')"
          ></textarea>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeFormDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="account-admin-form"
            class="btn btn-primary"
            :disabled="submitting"
          >
            {{ submitting ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="Boolean(deletingAccountAdmin)"
      :title="t('admin.accountAdmins.deleteTitle')"
      :message="t('admin.accountAdmins.deleteConfirm', { email: deletingAccountAdmin?.email })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="deletingAccountAdmin = null"
    />

    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp,
} from '@/composables/useStepUp'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()

const accountAdmins = ref<AdminUser[]>([])
const loading = ref(false)
const submitting = ref(false)
const statusChangingId = ref<number | null>(null)
const formDialogOpen = ref(false)
const editingAccountAdmin = ref<AdminUser | null>(null)
const deletingAccountAdmin = ref<AdminUser | null>(null)
const passwordVisible = ref(false)
let latestListRequestId = 0

const filters = reactive({
  search: '',
  status: '' as '' | 'active' | 'disabled',
})

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0,
})

const form = reactive({
  email: '',
  username: '',
  password: '',
  notes: '',
  status: 'active' as 'active' | 'disabled',
})

const columns = computed<Column[]>(() => [
  { key: 'identity', label: t('admin.accountAdmins.identity') },
  { key: 'role', label: t('admin.accountAdmins.role') },
  { key: 'status', label: t('common.status') },
  { key: 'last_active_at', label: t('admin.accountAdmins.lastActive') },
  { key: 'created_at', label: t('admin.accountAdmins.createdAt') },
  { key: 'actions', label: t('common.actions') },
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.accountAdmins.allStatuses') },
  { value: 'active', label: t('common.active') },
  { value: 'disabled', label: t('common.disabled') },
])

const statusOptions = computed(() => statusFilterOptions.value.slice(1))
const hasFilters = computed(() => Boolean(filters.search || filters.status))
const overviewItems = computed(() => [
  {
    label: t('admin.accountAdmins.matchingAdmins'),
    value: pagination.total,
    hint: t('admin.accountAdmins.matchingAdminsHint'),
  },
  {
    label: t('admin.accountAdmins.activeOnPage'),
    value: accountAdmins.value.filter(admin => admin.status === 'active').length,
    hint: t('admin.accountAdmins.activeOnPageHint'),
    tone: 'positive' as const,
  },
  {
    label: t('admin.accountAdmins.disabledOnPage'),
    value: accountAdmins.value.filter(admin => admin.status === 'disabled').length,
    hint: t('admin.accountAdmins.disabledOnPageHint'),
  },
])

function resetFilters(): void {
  filters.search = ''
  filters.status = ''
  applyFilters()
}

function resetForm(): void {
  Object.assign(form, {
    email: '',
    username: '',
    password: '',
    notes: '',
    status: 'active',
  })
  passwordVisible.value = false
}

function openCreate(): void {
  editingAccountAdmin.value = null
  resetForm()
  formDialogOpen.value = true
}

function openEdit(accountAdmin: AdminUser): void {
  editingAccountAdmin.value = accountAdmin
  Object.assign(form, {
    email: accountAdmin.email,
    username: accountAdmin.username || '',
    password: '',
    notes: accountAdmin.notes || '',
    status: accountAdmin.status,
  })
  passwordVisible.value = false
  formDialogOpen.value = true
}

function closeFormDialog(): void {
  if (submitting.value) return
  formDialogOpen.value = false
  editingAccountAdmin.value = null
  resetForm()
}

function generatePassword(): void {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  const maxUnbiasedByte = 256 - (256 % chars.length)
  const byte = new Uint8Array(1)
  const password: string[] = []
  while (password.length < 16) {
    globalThis.crypto.getRandomValues(byte)
    if (byte[0] < maxUnbiasedByte) password.push(chars[byte[0] % chars.length])
  }
  form.password = password.join('')
  passwordVisible.value = true
}

function showActionError(error: unknown, fallbackKey: string): void {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    appStore.showError(
      stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
        ? t('stepUp.adminApiKeyForbidden')
        : t('stepUp.notEnabled'),
    )
    return
  }
  const message = (error as { message?: string })?.message
  appStore.showError(message || t(fallbackKey))
}

async function loadAccountAdmins(): Promise<void> {
  const requestId = ++latestListRequestId
  loading.value = true
  try {
    const response = await adminAPI.accountAdmins.list(
      pagination.page,
      pagination.pageSize,
      filters,
    )
    if (requestId !== latestListRequestId) return
    accountAdmins.value = response.items
    pagination.total = response.total
  } catch (error) {
    if (requestId === latestListRequestId) {
      showActionError(error, 'admin.accountAdmins.loadFailed')
    }
  } finally {
    if (requestId === latestListRequestId) loading.value = false
  }
}

function applyFilters(): void {
  pagination.page = 1
  void loadAccountAdmins()
}

function handlePageChange(page: number): void {
  pagination.page = page
  void loadAccountAdmins()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.pageSize = pageSize
  pagination.page = 1
  void loadAccountAdmins()
}

async function submitForm(): Promise<void> {
  if (submitting.value) return
  submitting.value = true
  try {
    if (editingAccountAdmin.value) {
      const payload = {
        email: form.email,
        username: form.username,
        notes: form.notes,
        status: form.status,
        ...(form.password ? { password: form.password } : {}),
      }
      await stepUp.run(() => adminAPI.accountAdmins.update(editingAccountAdmin.value!.id, payload))
      appStore.showSuccess(t('admin.accountAdmins.updated'))
    } else {
      await stepUp.run(() => adminAPI.accountAdmins.create({
        email: form.email,
        password: form.password,
        username: form.username || undefined,
        notes: form.notes || undefined,
      }))
      appStore.showSuccess(t('admin.accountAdmins.created'))
    }
    formDialogOpen.value = false
    editingAccountAdmin.value = null
    resetForm()
    await loadAccountAdmins()
  } catch (error) {
    showActionError(error, editingAccountAdmin.value
      ? 'admin.accountAdmins.updateFailed'
      : 'admin.accountAdmins.createFailed')
  } finally {
    submitting.value = false
  }
}

async function toggleStatus(accountAdmin: AdminUser): Promise<void> {
  if (statusChangingId.value !== null) return
  statusChangingId.value = accountAdmin.id
  try {
    const status = accountAdmin.status === 'active' ? 'disabled' : 'active'
    await stepUp.run(() => adminAPI.accountAdmins.update(accountAdmin.id, { status }))
    appStore.showSuccess(t(status === 'active'
      ? 'admin.accountAdmins.enabled'
      : 'admin.accountAdmins.disabled'))
    await loadAccountAdmins()
  } catch (error) {
    showActionError(error, 'admin.accountAdmins.updateFailed')
  } finally {
    statusChangingId.value = null
  }
}

async function confirmDelete(): Promise<void> {
  if (!deletingAccountAdmin.value) return
  const target = deletingAccountAdmin.value
  try {
    await stepUp.run(() => adminAPI.accountAdmins.remove(target.id))
    deletingAccountAdmin.value = null
    appStore.showSuccess(t('admin.accountAdmins.deleted'))
    if (accountAdmins.value.length === 1 && pagination.page > 1) pagination.page -= 1
    await loadAccountAdmins()
  } catch (error) {
    showActionError(error, 'admin.accountAdmins.deleteFailed')
  }
}

onMounted(() => {
  void loadAccountAdmins()
})
</script>

<style scoped>
.account-admins-toolbar {
  @apply flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between;
}

.admin-avatar {
  @apply flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-primary-100 bg-primary-50 text-xs font-semibold tracking-wide text-primary-700 dark:border-primary-900/60 dark:bg-primary-900/20 dark:text-primary-300;
}

.admin-row-action {
  @apply inline-flex min-h-9 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white;
}

.admin-delete-action {
  @apply h-9 w-9 border-transparent bg-transparent text-gray-400 shadow-none hover:border-red-100 hover:bg-red-50 hover:text-red-600 dark:hover:border-red-900/50 dark:hover:bg-red-900/20 dark:hover:text-red-400;
}

@media (max-width: 640px) {
  .account-admins-page :deep([data-field='identity']) {
    @apply flex-col gap-2;
  }

  .account-admins-page :deep([data-field='identity'] > div) {
    @apply w-full text-left;
  }
}
</style>
