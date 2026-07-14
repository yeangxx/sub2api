<template>
  <BaseDialog :show="show" :title="t('admin.routingPolicies.priceVersionsTitle')" width="wide" @close="closeDialog">
    <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700"><thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-700/50"><tr><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.version') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.status') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.comment') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.priceRows') }}</th><th class="px-4 py-3 text-left">{{ t('admin.routingPolicies.createdAt') }}</th><th class="px-4 py-3 text-right">{{ t('common.actions') }}</th></tr></thead><tbody class="divide-y divide-gray-200 dark:divide-dark-700"><tr v-for="revision in revisions" :key="revision.id"><td class="px-4 py-3 font-medium">v{{ revision.version }}</td><td class="px-4 py-3"><span class="badge" :class="revision.state === 'published' ? 'badge-success' : revision.state === 'draft' ? 'badge-warning' : 'badge-gray'">{{ t(`admin.routingPolicies.revisionStates.${revision.state}`) }}</span></td><td class="max-w-64 px-4 py-3 text-gray-500"><span class="block truncate" :title="revision.comment">{{ revision.comment || '-' }}</span></td><td class="px-4 py-3 text-gray-500">{{ revision.prices.length }}</td><td class="px-4 py-3 text-gray-500">{{ new Date(revision.created_at).toLocaleString() }}</td><td class="px-4 py-3 text-right"><button v-if="revision.state === 'draft'" :data-test="`publish-price-revision-${revision.version}`" class="btn btn-sm btn-primary" :disabled="publishingVersion === revision.version" @click="requestPublish(revision)">{{ t('admin.routingPolicies.publish') }}</button></td></tr></tbody></table>
    </div>
  </BaseDialog>
  <ConfirmDialog
    :show="!!publishTarget"
    :title="t('admin.routingPolicies.publishPriceRevisionTitle')"
    :message="publishTarget ? t('admin.routingPolicies.publishPriceRevisionConfirm', { book: book?.name, version: publishTarget.version }) : ''"
    :confirm-text="t('admin.routingPolicies.publish')"
    :loading="publishingVersion !== null"
    @confirm="publish"
    @cancel="cancelPublish"
  />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type PriceBook, type PriceBookRevision } from '@/api/admin/routingPolicy'
const props = defineProps<{ show: boolean; book: PriceBook | null; revisions: PriceBookRevision[] }>()
const emit = defineEmits<{ close: []; published: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const publishTarget = ref<PriceBookRevision | null>(null)
const publishingVersion = ref<number | null>(null)
watch(() => props.show, show => { if (!show) publishTarget.value = null })
function requestPublish(revision: PriceBookRevision) { publishTarget.value = revision }
function cancelPublish() {
  if (publishingVersion.value !== null) return
  publishTarget.value = null
}
function closeDialog() {
  if (publishTarget.value) {
    cancelPublish()
    return
  }
  if (publishingVersion.value === null) emit('close')
}
async function publish() {
  if (!props.book || !publishTarget.value || publishingVersion.value !== null) return
  const revision = publishTarget.value
  publishingVersion.value = revision.version
  try {
    await routingPolicyApi.publishPriceBookRevision(props.book.id, revision.version)
    appStore.showSuccess(t('admin.routingPolicies.published'))
    publishTarget.value = null
    emit('published')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.publishFailed')))
  } finally {
    publishingVersion.value = null
  }
}
</script>
