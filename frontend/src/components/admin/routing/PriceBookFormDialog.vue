<template>
  <BaseDialog :show="show" :title="book ? t('admin.routingPolicies.priceBookEditTitle') : t('admin.routingPolicies.priceBookCreateTitle')" width="wide" @close="emit('close')">
    <form id="price-book-form" class="space-y-5" @submit.prevent="submit">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div><label class="input-label">{{ t('admin.routingPolicies.name') }}</label><input v-model="form.name" class="input" required /></div>
        <div><label class="input-label">{{ t('admin.routingPolicies.status') }}</label><Select v-model="form.status" :options="statusOptions" /></div>
        <div><label class="input-label">{{ t('admin.routingPolicies.source') }}</label><Select v-model="form.source" :options="sourceOptions" /></div>
        <div><label class="input-label">{{ t('admin.routingPolicies.currency') }}</label><input v-model="form.currency" class="input uppercase" maxlength="3" /></div>
      </div>
      <template v-if="form.source === 'http_json'">
        <div><label class="input-label">{{ t('admin.routingPolicies.sourceUrl') }}</label><input v-model="form.source_config.url" class="input" type="url" placeholder="https://provider.example/prices.json" required /></div>
        <div><label class="input-label">{{ t('admin.routingPolicies.headers') }}</label><KeyValueEditor v-model="form.source_config.headers" /></div>
      </template>
    </form>
    <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" type="button" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="price-book-form" :disabled="saving"><Icon name="check" size="sm" class="mr-2" />{{ t('common.save') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyValueEditor from './KeyValueEditor.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type PriceBook, type PriceBookInput, type PriceSourceConfig, type RoutingPolicyStatus } from '@/api/admin/routingPolicy'

const props = defineProps<{ show: boolean; book?: PriceBook | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const saving = ref(false)
const form = reactive<PriceBookInput & { source_config: PriceSourceConfig & { url: string; headers: Record<string, string> } }>({ name: '', source: 'manual', status: 'active', currency: 'USD', source_config: { url: '', headers: {} } })
const statusOptions = computed(() => ['active', 'disabled', 'archived'].map(value => ({ value, label: t(`admin.routingPolicies.statuses.${value}`) })))
const sourceOptions = [{ value: 'manual', label: 'Manual' }, { value: 'http_json', label: 'HTTP JSON' }]

watch(() => props.show, show => {
  if (!show) return
  form.name = props.book?.name || ''
  form.source = props.book?.source || 'manual'
  form.status = (props.book?.status || 'active') as RoutingPolicyStatus
  form.currency = props.book?.currency || 'USD'
  form.source_config = { url: props.book?.source_config?.url || '', headers: { ...(props.book?.source_config?.headers || {}) } }
})

async function submit() {
  if (!form.name.trim()) return
  saving.value = true
  try {
    const payload: PriceBookInput = { name: form.name.trim(), source: form.source, status: form.status, currency: form.currency.trim().toUpperCase(), ...(form.source === 'http_json' ? { source_config: { url: form.source_config?.url?.trim(), headers: form.source_config?.headers || {} } } : { source_config: {} }) }
    if (props.book) await routingPolicyApi.updatePriceBook(props.book.id, payload)
    else await routingPolicyApi.createPriceBook(payload)
    appStore.showSuccess(t('admin.routingPolicies.priceBookSaved'))
    emit('saved')
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.priceBookSaveFailed'))) }
  finally { saving.value = false }
}
</script>
