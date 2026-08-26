<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-72">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="filters.search"
                type="search"
                class="input pl-10"
                :placeholder="t('admin.accountAdmins.searchPlaceholder')"
                @keyup.enter="applyFilters"
              />
            </div>
            <div class="w-full sm:w-36">
              <Select
                v-model="filters.status"
                :options="statusFilterOptions"
                :searchable="false"
                @change="applyFilters"
              />
            </div>
          </div>

          <div class="flex items-center gap-2">
            <button
              type="button"
              class="btn btn-secondary px-3"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadAccountAdmins"
            >
              <Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreate">
              <Icon name="userPlus" size="md" class="mr-2" />
              {{ t('admin.accountAdmins.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="accountAdmins" :loading="loading" row-key="id">
          <template #cell-identity="{ row }">
            <div class="min-w-0">
              <div class="truncate font-medium text-gray-900 dark:text-white">
                {{ row.username || row.email.split('@')[0] }}
              </div>
              <div class="truncate text-xs text-gray-500 dark:text-dark-400">{{ row.email }}</div>
            </div>
          </template>

          <template #cell-role>
            <span class="badge badge-primary">
              {{ t('admin.users.roles.account_admin') }}
            </span>
          </template>

          <template #cell-status="{ row }">
            <span
              class="badge"
              :class="row.status === 'active' ? 'badge-success' : 'badge-gray'"
            >
              {{ row.status === 'active' ? t('common.active') : t('common.disabled') }}
            </span>
          </template>

          <template #cell-last_active_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center justify-end gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="openEdit(row)">
                {{ t('common.edit') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="statusChangingId === row.id"
                @click="toggleStatus(row)"
              >
                {{ row.status === 'active'
                  ? t('admin.accountAdmins.disable')
                  : t('admin.accountAdmins.enable') }}
              </button>
              <button
                type="button"
                class="btn btn-sm bg-red-600 text-white hover:bg-red-700"
                @click="deletingAccountAdmin = row"
              >
                {{ t('common.delete') }}
              </button>
            </div>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-8">
              <Icon name="users" size="xl" class="mb-3 h-12 w-12 text-gray-300 dark:text-dark-600" />
              <p class="font-medium text-gray-700 dark:text-gray-200">
                {{ t('admin.accountAdmins.empty') }}
              </p>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ t('admin.accountAdmins.emptyHint') }}
              </p>
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
        <div>
          <label class="input-label">{{ t('admin.accountAdmins.email') }}</label>
          <input
            v-model.trim="form.email"
            type="email"
            required
            class="input"
            autocomplete="off"
            :placeholder="t('admin.accountAdmins.emailPlaceholder')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accountAdmins.username') }}</label>
          <input
            v-model.trim="form.username"
            type="text"
            maxlength="100"
            class="input"
            :placeholder="t('admin.accountAdmins.usernamePlaceholder')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accountAdmins.password') }}</label>
          <div class="flex gap-2">
            <input
              v-model="form.password"
              :type="passwordVisible ? 'text' : 'password'"
              :required="!editingAccountAdmin"
              minlength="6"
              maxlength="72"
              class="input flex-1"
              autocomplete="new-password"
              :placeholder="editingAccountAdmin
                ? t('admin.accountAdmins.passwordEditPlaceholder')
                : t('admin.accountAdmins.passwordPlaceholder')"
            />
            <button type="button" class="btn btn-secondary px-3" @click="passwordVisible = !passwordVisible">
              <Icon :name="passwordVisible ? 'eyeOff' : 'eye'" size="md" />
            </button>
            <button type="button" class="btn btn-secondary px-3" @click="generatePassword">
              <Icon name="refresh" size="md" />
            </button>
          </div>
          <p class="input-hint">{{ t('admin.accountAdmins.passwordHint') }}</p>
        </div>
        <div v-if="editingAccountAdmin">
          <label class="input-label">{{ t('common.status') }}</label>
          <Select v-model="form.status" :options="statusOptions" :searchable="false" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accountAdmins.notes') }}</label>
          <textarea
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
