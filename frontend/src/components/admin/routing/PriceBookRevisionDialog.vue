<template>
  <BaseDialog :show="show" :title="t('admin.routingPolicies.priceRevisionTitle')" width="full" @close="emit('close')">
    <form id="price-revision-form" class="space-y-5" @submit.prevent="submit">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div><label class="input-label">{{ t('admin.routingPolicies.comment') }}</label><input v-model="comment" class="input" /></div>
        <div><label class="input-label">{{ t('admin.routingPolicies.effectiveAt') }}</label><input v-model="effectiveAt" class="input" type="datetime-local" /></div>
      </div>
      <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
        <table class="min-w-[1080px] w-full divide-y divide-gray-200 text-xs dark:divide-dark-700">
          <thead class="bg-gray-50 text-gray-500 dark:bg-dark-700/50"><tr><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.modelPattern') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.inputPrice') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.outputPrice') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.cacheReadPrice') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.cacheWritePrice') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.requestPrice') }}</th><th class="w-24 px-3 py-2 text-right">{{ t('common.actions') }}</th></tr></thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <template v-for="(row, index) in rows" :key="row.localId">
              <tr><td class="p-2"><input v-model="row.model_pattern" class="input" placeholder="gpt-4o*" required /></td><td class="p-2"><input v-model="row.input_price_per_million" class="input" type="number" min="0" step="0.000001" /></td><td class="p-2"><input v-model="row.output_price_per_million" class="input" type="number" min="0" step="0.000001" /></td><td class="p-2"><input v-model="row.cache_read_price_per_million" class="input" type="number" min="0" step="0.000001" /></td><td class="p-2"><input v-model="row.cache_write_price_per_million" class="input" type="number" min="0" step="0.000001" /></td><td class="p-2"><input v-model="row.request_price" class="input" type="number" min="0" step="0.000001" /></td><td class="p-2 text-right"><button type="button" class="p-2 text-gray-500 hover:text-primary-600" :title="t('admin.routingPolicies.metadata')" @click="metadataIndex = metadataIndex === index ? null : index"><Icon name="cog" size="sm" /></button><button type="button" class="p-2 text-gray-500 hover:text-red-600" :title="t('common.delete')" @click="removeRow(index)"><Icon name="trash" size="sm" /></button></td></tr>
              <tr v-if="metadataIndex === index"><td colspan="7" class="bg-gray-50 p-3 dark:bg-dark-900"><label class="input-label">{{ t('admin.routingPolicies.metadata') }}</label><KeyValueEditor v-model="row.metadata" /></td></tr>
            </template>
          </tbody>
        </table>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" @click="addRow"><Icon name="plus" size="sm" class="mr-1" />{{ t('admin.routingPolicies.addPriceRow') }}</button>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" type="button" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="price-revision-form" :disabled="saving || rows.length === 0"><Icon name="check" size="sm" class="mr-2" />{{ t('admin.routingPolicies.saveRevision') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyValueEditor from './KeyValueEditor.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type PriceBook, type UpstreamModelPrice } from '@/api/admin/routingPolicy'

interface PriceRow extends UpstreamModelPrice { localId: string; metadata: Record<string, string> }
const props = defineProps<{ show: boolean; book: PriceBook | null; initialPrices?: UpstreamModelPrice[] }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const rows = ref<PriceRow[]>([])
const comment = ref('')
const effectiveAt = ref('')
const metadataIndex = ref<number | null>(null)
const saving = ref(false)
let sequence = 0
function newRow(source?: UpstreamModelPrice): PriceRow { sequence += 1; return { localId: `price-${sequence}`, model_pattern: source?.model_pattern || '', input_price_per_million: source?.input_price_per_million || '0', output_price_per_million: source?.output_price_per_million || '0', cache_read_price_per_million: source?.cache_read_price_per_million || '0', cache_write_price_per_million: source?.cache_write_price_per_million || '0', request_price: source?.request_price || '0', metadata: Object.fromEntries(Object.entries(source?.metadata || {}).map(([key, value]) => [key, String(value)])) } }
watch(() => props.show, show => { if (show) { rows.value = (props.initialPrices || []).map(newRow); if (!rows.value.length) rows.value = [newRow()]; comment.value = ''; effectiveAt.value = ''; metadataIndex.value = null } })
function addRow() { rows.value.push(newRow()) }
function removeRow(index: number) { rows.value.splice(index, 1); metadataIndex.value = null }
async function submit() {
  if (!props.book) return
  const patterns = rows.value.map(row => row.model_pattern.trim())
  if (patterns.some(pattern => !pattern) || new Set(patterns).size !== patterns.length) { appStore.showError(t('admin.routingPolicies.invalidPriceRows')); return }
  saving.value = true
  try {
    await routingPolicyApi.createPriceBookRevision(props.book.id, { comment: comment.value.trim(), ...(effectiveAt.value ? { effective_at: new Date(effectiveAt.value).toISOString() } : {}), prices: rows.value.map(({ localId: _, ...row }) => row) })
    appStore.showSuccess(t('admin.routingPolicies.revisionSaved'))
    emit('saved')
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.revisionSaveFailed'))) }
  finally { saving.value = false }
}
</script>
