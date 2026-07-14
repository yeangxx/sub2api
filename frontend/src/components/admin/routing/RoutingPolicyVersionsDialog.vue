<template>
  <BaseDialog :show="show" :title="t('admin.routingPolicies.versionsTitle')" width="wide" @close="emit('close')">
    <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
        <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-700/50"><tr><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.version') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.status') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.comment') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.createdAt') }}</th><th class="px-4 py-3 text-right">{{ t('common.actions') }}</th></tr></thead>
        <tbody class="divide-y divide-gray-200 dark:divide-dark-700"><tr v-for="version in versions" :key="version.id"><td class="px-4 py-3 font-medium">v{{ version.version }}</td><td class="px-4 py-3"><span class="badge" :class="version.state === 'published' ? 'badge-success' : version.state === 'draft' ? 'badge-warning' : 'badge-gray'">{{ version.state }}</span></td><td class="px-4 py-3 text-gray-500">{{ version.comment || '-' }}</td><td class="px-4 py-3 text-gray-500">{{ formatDate(version.created_at) }}</td><td class="px-4 py-3 text-right"><button v-if="version.state === 'draft'" class="btn btn-sm btn-primary mr-2" @click="emit('publish', version)">{{ t('admin.routingPolicies.publish') }}</button><button class="btn btn-sm btn-secondary" @click="requestRestore(version)">{{ t('admin.routingPolicies.restore') }}</button></td></tr></tbody>
      </table>
    </div>
    <ConfirmDialog :show="!!restoreTarget" :title="t('admin.routingPolicies.restore')" :message="t('admin.routingPolicies.restoreConfirm', { version: restoreTarget?.version })" @confirm="restore" @cancel="restoreTarget = null" />
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type RoutingPolicy, type RoutingRevision } from '@/api/admin/routingPolicy'

const props = defineProps<{ show: boolean; policy: RoutingPolicy | null; versions: RoutingRevision[] }>()
const emit = defineEmits<{ close: []; restored: []; publish: [version: RoutingRevision] }>()
const { t } = useI18n()
const appStore = useAppStore()
const restoreTarget = ref<RoutingRevision | null>(null)
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
function requestRestore(version: RoutingRevision) { restoreTarget.value = version }
async function restore() {
  if (!props.policy || !restoreTarget.value) return
  try { await routingPolicyApi.restore(props.policy.id, restoreTarget.value.version); appStore.showSuccess(t('admin.routingPolicies.restored')); restoreTarget.value = null; emit('restored') }
  catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.restoreFailed'))) }
}
</script>
