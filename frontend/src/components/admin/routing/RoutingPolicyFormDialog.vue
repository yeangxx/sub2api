<template>
  <BaseDialog :show="show" :title="policy ? t('admin.routingPolicies.editTitle') : t('admin.routingPolicies.createTitle')" width="extra-wide" @close="emit('close')">
    <div class="-mx-4 -mt-3 flex max-w-[calc(100%+2rem)] min-w-0 overflow-x-auto border-b border-gray-200 px-4 dark:border-dark-700 sm:-mx-6 sm:max-w-[calc(100%+3rem)] sm:px-6">
      <button v-for="tab in tabs" :key="tab.value" type="button" class="whitespace-nowrap border-b-2 px-3 py-3 text-sm font-medium" :class="activeTab === tab.value ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'" @click="activeTab = tab.value">
        {{ tab.label }}
      </button>
    </div>

    <form id="routing-policy-form" class="max-h-[68vh] overflow-y-auto pt-5" novalidate @submit.prevent="submit">
      <div v-if="errors.length" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300">
        {{ t('admin.routingPolicies.validationFailed') }}: {{ errors.map(errorLabel).join(', ') }}
      </div>

      <div v-show="activeTab === 'basic'" class="space-y-5">
        <div v-if="!policy">
          <label class="input-label">{{ t('admin.routingPolicies.preset') }}</label>
          <Select v-model="preset" :options="presetOptions" @change="applyPreset" />
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.routingPolicies.name') }} <span class="text-red-500">*</span></label>
            <input v-model="name" class="input" :class="hasError('name') && 'input-error'" required />
            <p v-if="hasError('name')" data-test="field-error-name" class="input-error-text">{{ errorLabel('name') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.routingPolicies.status') }}</label>
            <Select v-model="status" :options="statusOptions" />
          </div>
          <div class="md:col-span-2">
            <label class="input-label">{{ t('admin.routingPolicies.form.description') }}</label>
            <textarea v-model="description" class="input" rows="2" />
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('admin.routingPolicies.form.platforms') }}</label>
          <div class="flex flex-wrap gap-2">
            <label v-for="platform in platformOptions" :key="platform.value" class="inline-flex cursor-pointer items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-600">
              <input v-model="config.candidate_filters.platforms" type="checkbox" :value="platform.value" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              {{ platform.label }}
            </label>
          </div>
          <p class="input-hint">{{ t('admin.routingPolicies.form.platformsHint') }}</p>
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <ToggleField v-model="config.candidate_filters.require_known_price" :label="t('admin.routingPolicies.form.requireKnownPrice')" />
          <ToggleField v-model="config.candidate_filters.require_trusted_upstream" :label="t('admin.routingPolicies.form.requireTrusted')" />
        </div>
      </div>

      <div v-show="activeTab === 'scoring'" class="space-y-5">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.routingPolicies.form.scoringHint') }}</p>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <NumberField v-for="field in scoringFields" :key="field.key" v-model="config.scoring[field.key]" :label="field.label" :min="0" :error="hasError('scoring', 'scoring.total')" />
        </div>
        <div v-if="hasError('scoring', 'scoring.total')" data-test="field-error-scoring" class="input-error-text space-y-1"><p v-for="error in matchingErrors('scoring', 'scoring.total')" :key="error">{{ errorLabel(error) }}</p></div>
        <div class="border-t border-gray-200 pt-4 dark:border-dark-700">
          <label class="input-label">{{ t('admin.routingPolicies.form.reliabilityClasses') }}</label>
          <div class="flex flex-wrap gap-2">
            <label v-for="item in reliabilityOptions" :key="item.value" class="inline-flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-600">
              <input v-model="config.candidate_filters.reliability_classes" type="checkbox" :value="item.value" class="h-4 w-4 rounded text-primary-600" />
              {{ item.label }}
            </label>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.routingPolicies.form.requiredLabels') }}</label>
            <KeyValueEditor v-model="config.candidate_filters.required_labels" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.routingPolicies.form.excludedLabels') }}</label>
            <KeyValueEditor v-model="config.candidate_filters.excluded_labels" />
          </div>
        </div>
      </div>

      <div v-show="activeTab === 'timeouts'" class="space-y-6">
        <div>
          <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.routingPolicies.form.timeouts') }}</h4>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <NumberField v-model="config.timeouts.request_timeout_ms" :label="t('admin.routingPolicies.form.requestTimeout')" :min="0" suffix="ms" :error="hasError('timeouts')" />
            <NumberField v-model="config.timeouts.soft_ttft_ms" :label="t('admin.routingPolicies.form.softTtft')" :min="0" suffix="ms" :error="hasError('timeouts')" />
            <NumberField v-model="config.timeouts.stream_idle_ms" :label="t('admin.routingPolicies.form.streamIdle')" :min="0" suffix="ms" :error="hasError('timeouts')" />
            <NumberField v-model="config.timeouts.soft_ttft_min_ms" :label="t('admin.routingPolicies.form.softTtftMin')" :min="0" suffix="ms" :error="hasError('timeouts', 'timeouts.soft_ttft_range')" />
            <NumberField v-model="config.timeouts.soft_ttft_max_ms" :label="t('admin.routingPolicies.form.softTtftMax')" :min="0" suffix="ms" :error="hasError('timeouts', 'timeouts.soft_ttft_range')" />
            <NumberField v-model="config.timeouts.soft_ttft_factor" :label="t('admin.routingPolicies.form.softTtftFactor')" :min="0" :step="0.05" :error="hasError('timeouts.soft_ttft_factor')" />
          </div>
          <div v-if="hasError('timeouts', 'timeouts.soft_ttft_range', 'timeouts.soft_ttft_factor')" data-test="field-error-timeouts" class="input-error-text mt-2 space-y-1"><p v-for="error in matchingErrors('timeouts', 'timeouts.soft_ttft_range', 'timeouts.soft_ttft_factor')" :key="error">{{ errorLabel(error) }}</p></div>
          <div class="mt-4"><ToggleField v-model="config.timeouts.adaptive_soft_ttft" :label="t('admin.routingPolicies.form.adaptiveSoftTtft')" /></div>
        </div>
        <div class="border-t border-gray-200 pt-5 dark:border-dark-700">
          <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.routingPolicies.form.retry') }}</h4>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <NumberField v-model="config.retry.max_attempts" :label="t('admin.routingPolicies.form.maxAttempts')" :min="0" :step="1" :error="hasError('retry')" />
            <NumberField v-model="config.retry.max_switches" :label="t('admin.routingPolicies.form.maxSwitches')" :min="0" :step="1" :error="hasError('retry')" />
            <div>
              <label class="input-label">{{ t('admin.routingPolicies.form.retryStatusCodes') }}</label>
              <input v-model="retryCodesText" class="input" placeholder="408, 429, 500, 502" />
            </div>
          </div>
          <p v-if="hasError('retry')" data-test="field-error-retry" class="input-error-text mt-2">{{ errorLabel('retry') }}</p>
          <div class="mt-4"><ToggleField v-model="config.retry.retry_transport_errors" :label="t('admin.routingPolicies.form.retryTransportErrors')" /></div>
        </div>
      </div>

      <div v-show="activeTab === 'resilience'" class="space-y-6">
        <div>
          <div class="mb-4 flex items-center justify-between"><h4 data-test="hedge-heading" class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.routingPolicies.form.hedge') }}</h4><Toggle v-model="config.hedge.enabled" /></div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3" :class="!config.hedge.enabled && 'opacity-50'">
            <NumberField v-model="config.hedge.delay_ms" :label="t('admin.routingPolicies.form.hedgeDelay')" :min="0" suffix="ms" :error="hasError('hedge.delay_ms')" />
            <NumberField v-model="config.hedge.max_concurrent" :label="t('admin.routingPolicies.form.hedgeConcurrent')" :min="1" :step="1" :error="hasError('hedge.max_concurrent')" />
            <ToggleField v-model="config.hedge.require_different_failure_domain" :label="t('admin.routingPolicies.form.differentDomain')" />
            <ToggleField v-model="config.hedge.require_no_semantic_output" :label="t('admin.routingPolicies.form.noSemanticOutput')" />
          </div>
          <div v-if="hasError('hedge.delay_ms', 'hedge.max_concurrent')" data-test="field-error-hedge" class="input-error-text mt-2 space-y-1"><p v-for="error in matchingErrors('hedge.delay_ms', 'hedge.max_concurrent')" :key="error">{{ errorLabel(error) }}</p></div>
        </div>
        <div class="border-t border-gray-200 pt-5 dark:border-dark-700">
          <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.routingPolicies.form.circuitBreaker') }}</h4>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <NumberField v-model="config.circuit_breaker.consecutive_failures" :label="t('admin.routingPolicies.form.consecutiveFailures')" :min="0" :step="1" :error="hasError('circuit_breaker.limits')" />
            <NumberField v-model="config.circuit_breaker.min_samples" :label="t('admin.routingPolicies.form.minSamples')" :min="0" :step="1" :error="hasError('circuit_breaker.limits')" />
            <NumberField v-model="config.circuit_breaker.error_rate_percent" :label="t('admin.routingPolicies.form.errorRatePercent')" :min="0" :max="100" suffix="%" :error="hasError('circuit_breaker.error_rate_percent')" />
            <NumberField v-model="config.circuit_breaker.cooldown_ms" :label="t('admin.routingPolicies.form.cooldown')" :min="0" suffix="ms" :error="hasError('circuit_breaker.cooldown')" />
            <NumberField v-model="config.circuit_breaker.max_cooldown_ms" :label="t('admin.routingPolicies.form.maxCooldown')" :min="0" suffix="ms" :error="hasError('circuit_breaker.cooldown')" />
            <NumberField v-model="config.circuit_breaker.half_open_max_requests" :label="t('admin.routingPolicies.form.halfOpenRequests')" :min="0" :step="1" :error="hasError('circuit_breaker.limits')" />
          </div>
          <div v-if="hasError('circuit_breaker.error_rate_percent', 'circuit_breaker.limits', 'circuit_breaker.cooldown')" data-test="field-error-circuit" class="input-error-text mt-2 space-y-1"><p v-for="error in matchingErrors('circuit_breaker.error_rate_percent', 'circuit_breaker.limits', 'circuit_breaker.cooldown')" :key="error">{{ errorLabel(error) }}</p></div>
        </div>
      </div>

      <div v-show="activeTab === 'fallback'" class="space-y-6">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <ToggleField v-model="config.fallback.allow_cross_tier" :label="t('admin.routingPolicies.form.allowCrossTier')" />
          <ToggleField v-model="config.fallback.require_explicit_model_map" :label="t('admin.routingPolicies.form.explicitModelMap')" />
          <NumberField v-model="config.fallback.max_cost_multiplier" :label="t('admin.routingPolicies.form.maxCostMultiplier')" :min="0" :step="0.1" :error="hasError('fallback.max_cost_multiplier')" />
        </div>
        <p v-if="hasError('fallback.max_cost_multiplier')" data-test="field-error-fallback" class="input-error-text">{{ errorLabel('fallback.max_cost_multiplier') }}</p>
        <div>
          <label class="input-label">{{ t('admin.routingPolicies.form.fallbackGroups') }}</label>
          <div class="grid max-h-40 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 bg-gray-50 p-2 dark:border-dark-600 dark:bg-dark-900 sm:grid-cols-2">
            <label v-for="group in groups" :key="group.id" class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-white dark:hover:bg-dark-700">
              <input v-model="config.fallback.group_ids" type="checkbox" :value="group.id" class="h-4 w-4 rounded text-primary-600" />
              <GroupBadge :name="group.name" :platform="group.platform" :subscription-type="group.subscription_type" :rate-multiplier="group.rate_multiplier" class="min-w-0 flex-1" />
            </label>
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('admin.routingPolicies.form.modelMappings') }}</label>
          <KeyValueEditor v-model="config.model_mappings" :key-placeholder="t('admin.routingPolicies.form.requestModel')" :value-placeholder="t('admin.routingPolicies.form.upstreamModel')" />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <TextNumberField v-model="config.cost_budget.max_upstream_usd" :label="t('admin.routingPolicies.form.maxUpstreamUsd')" :error="hasError('cost_budget')" />
          <TextNumberField v-model="config.cost_budget.max_attempt_cost_usd" :label="t('admin.routingPolicies.form.maxAttemptUsd')" :error="hasError('cost_budget')" />
          <TextNumberField v-model="config.cost_budget.reserve_for_hedge_usd" :label="t('admin.routingPolicies.form.hedgeReserveUsd')" :error="hasError('cost_budget')" />
        </div>
        <p v-if="hasError('cost_budget')" data-test="field-error-budget" class="input-error-text">{{ errorLabel('cost_budget') }}</p>
      </div>
    </form>

    <template #footer>
      <div class="flex w-full justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button type="submit" form="routing-policy-form" class="btn btn-primary" :disabled="saving">
          <Icon name="check" size="sm" class="mr-2" />
          {{ saving ? t('common.saving') : t('admin.routingPolicies.saveDraft') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyValueEditor from './KeyValueEditor.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, routingPresetConfig, type RoutingPolicy, type RoutingPolicyConfig, type RoutingPolicyStatus, type RoutingPreset, type RoutingRevision, type RoutingScoringWeights } from '@/api/admin/routingPolicy'
import type { AdminGroup } from '@/types'
import { cloneRoutingPolicyConfig, validateRoutingPolicyConfig } from '@/views/admin/routingPolicyForm'

const NumberField = defineComponent({
  props: { modelValue: { type: Number, required: true }, label: { type: String, required: true }, min: Number, max: Number, step: { type: Number, default: 1 }, suffix: String, error: Boolean },
  emits: ['update:modelValue'],
  setup(props, { emit }) { return () => h('div', [h('label', { class: 'input-label' }, props.label), h('div', { class: 'relative' }, [h('input', { class: ['input', props.error && 'input-error'], type: 'number', value: props.modelValue, min: props.min, max: props.max, step: props.step, 'aria-invalid': props.error || undefined, onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)) }), props.suffix ? h('span', { class: 'pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-400' }, props.suffix) : null])]) }
})
const TextNumberField = defineComponent({
  props: { modelValue: { type: String, required: true }, label: { type: String, required: true }, error: Boolean }, emits: ['update:modelValue'],
  setup(props, { emit }) { return () => h('div', [h('label', { class: 'input-label' }, props.label), h('input', { class: ['input', props.error && 'input-error'], type: 'number', min: 0, step: '0.000001', value: props.modelValue, 'aria-invalid': props.error || undefined, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) })]) }
})
const ToggleField = defineComponent({
  props: { modelValue: { type: Boolean, required: true }, label: { type: String, required: true } }, emits: ['update:modelValue'],
  setup(props, { emit }) { return () => h('div', { class: 'flex items-center justify-between gap-4 rounded-lg border border-gray-200 px-3 py-2.5 dark:border-dark-600' }, [h('span', { class: 'text-sm text-gray-700 dark:text-gray-300' }, props.label), h(Toggle, { modelValue: props.modelValue, 'onUpdate:modelValue': (value: boolean) => emit('update:modelValue', value) })]) }
})

const props = defineProps<{ show: boolean; policy?: RoutingPolicy | null; revision?: RoutingRevision | null; groups: AdminGroup[] }>()
const emit = defineEmits<{ close: []; saved: [policy: RoutingPolicy] }>()
const { t } = useI18n()
const appStore = useAppStore()
const activeTab = ref('basic')
const preset = ref<RoutingPreset>('standard')
const name = ref('')
const description = ref('')
const status = ref<RoutingPolicyStatus>('active')
const config = ref<RoutingPolicyConfig>(routingPresetConfig('standard'))
const retryCodesText = ref('408, 409, 429, 500, 502, 503, 504, 529')
const errors = ref<string[]>([])
const saving = ref(false)

const tabs = computed(() => [
  { value: 'basic', label: t('admin.routingPolicies.form.basic') },
  { value: 'scoring', label: t('admin.routingPolicies.form.scoring') },
  { value: 'timeouts', label: t('admin.routingPolicies.form.timeoutsRetry') },
  { value: 'resilience', label: t('admin.routingPolicies.form.resilience') },
  { value: 'fallback', label: t('admin.routingPolicies.form.fallbackBudget') },
])
const presetOptions = ['economy', 'standard', 'professional'].map(value => ({ value, label: value }))
const statusOptions = computed(() => ['active', 'disabled', 'archived'].map(value => ({ value, label: t(`admin.routingPolicies.statuses.${value}`) })))
const platformOptions = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].map(value => ({ value, label: value === 'openai' ? 'OpenAI' : value.charAt(0).toUpperCase() + value.slice(1) }))
const reliabilityOptions = ['standard', 'trusted', 'partner', 'official'].map(value => ({ value, label: value }))
const scoringFields = computed<Array<{ key: keyof RoutingScoringWeights; label: string }>>(() => [
  { key: 'price', label: t('admin.routingPolicies.form.weightPrice') },
  { key: 'error_rate', label: t('admin.routingPolicies.form.weightError') },
  { key: 'ttft', label: t('admin.routingPolicies.form.weightTtft') },
  { key: 'load', label: t('admin.routingPolicies.form.weightLoad') },
  { key: 'queue', label: t('admin.routingPolicies.form.weightQueue') },
  { key: 'reliability', label: t('admin.routingPolicies.form.weightReliability') },
])

watch(() => props.show, (show) => {
  if (!show) return
  activeTab.value = 'basic'
  errors.value = []
  name.value = props.policy?.name || ''
  description.value = props.policy?.description || ''
  status.value = props.policy?.status || 'active'
  config.value = cloneRoutingPolicyConfig(props.revision?.config || routingPresetConfig(preset.value))
  retryCodesText.value = config.value.retry.retryable_status_codes.join(', ')
}, { immediate: true })

function applyPreset() {
  config.value = cloneRoutingPolicyConfig(routingPresetConfig(preset.value))
  retryCodesText.value = config.value.retry.retryable_status_codes.join(', ')
}

function errorLabel(error: string) { return t(`admin.routingPolicies.validation.${error}`, error) }
function hasError(...keys: string[]) { return keys.some(key => errors.value.includes(key)) }
function matchingErrors(...keys: string[]) { return errors.value.filter(error => keys.includes(error)) }
function tabForError(error: string) {
  if (error === 'name') return 'basic'
  if (error.startsWith('scoring')) return 'scoring'
  if (error.startsWith('timeouts') || error.startsWith('retry')) return 'timeouts'
  if (error.startsWith('hedge') || error.startsWith('circuit_breaker')) return 'resilience'
  return 'fallback'
}

async function submit() {
  config.value.retry.retryable_status_codes = retryCodesText.value.split(/[\s,;]+/).map(Number).filter(code => Number.isInteger(code) && code >= 100 && code <= 599)
  errors.value = validateRoutingPolicyConfig(config.value)
  if (!name.value.trim()) errors.value.unshift('name')
  if (errors.value.length) {
    activeTab.value = tabForError(errors.value[0])
    appStore.showError(t('admin.routingPolicies.validationFailed'))
    return
  }
  saving.value = true
  try {
    let saved: RoutingPolicy
    if (props.policy) {
      await routingPolicyApi.validate(props.policy.id, config.value)
      const result = await routingPolicyApi.update(props.policy.id, { name: name.value.trim(), description: description.value.trim(), status: status.value, config: config.value, comment: 'edited from admin console' })
      saved = result.policy
    } else {
      const result = await routingPolicyApi.create({ name: name.value.trim(), description: description.value.trim(), status: status.value, config: config.value, comment: `created from ${preset.value} preset` })
      saved = result.policy
    }
    appStore.showSuccess(t('admin.routingPolicies.saved'))
    emit('saved', saved)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.saveFailed')))
  } finally {
    saving.value = false
  }
}
</script>
