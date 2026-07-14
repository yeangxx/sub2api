<template>
  <BaseDialog :show="show" :title="t('admin.routingPolicies.simulationTitle')" width="extra-wide" @close="emit('close')">
    <form class="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]" @submit.prevent="run">
      <div>
        <label class="input-label">{{ t('admin.routingPolicies.group') }}</label>
        <Select v-model="groupId" :options="groupOptions" searchable :placeholder="t('admin.routingPolicies.selectGroup')">
          <template #selected="{ option }">
            <div v-if="option" class="flex min-w-0 items-center gap-2">
              <GroupBadge :name="optionGroup(option).name" :platform="optionGroup(option).platform" :subscription-type="optionGroup(option).subscription_type" :rate-multiplier="optionGroup(option).rate_multiplier" :show-rate="false" class="min-w-0" />
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ platformLabel(optionGroup(option).platform) }}</span>
              <span class="badge" :class="groupStatusClass(optionGroup(option).status)">{{ groupStatusLabel(optionGroup(option).status) }}</span>
              <span class="text-xs text-gray-400">#{{ optionGroup(option).id }}</span>
            </div>
          </template>
          <template #option="{ option }">
            <div class="flex min-w-0 flex-1 items-center gap-2">
              <GroupBadge :name="optionGroup(option).name" :platform="optionGroup(option).platform" :subscription-type="optionGroup(option).subscription_type" :rate-multiplier="optionGroup(option).rate_multiplier" :show-rate="false" class="min-w-0" />
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ platformLabel(optionGroup(option).platform) }}</span>
              <span class="badge" :class="groupStatusClass(optionGroup(option).status)">{{ groupStatusLabel(optionGroup(option).status) }}</span>
              <span class="ml-auto text-xs text-gray-400">#{{ optionGroup(option).id }}</span>
            </div>
          </template>
        </Select>
      </div>
      <div>
        <label class="input-label">{{ t('admin.routingPolicies.model') }}</label>
        <input v-model="model" class="input" placeholder="gpt-4o" />
      </div>
      <div class="self-end"><button class="btn btn-primary w-full" type="submit" :disabled="loading || !groupId || !model.trim()"><Icon name="play" size="sm" class="mr-2" />{{ t('admin.routingPolicies.simulate') }}</button></div>
    </form>

    <div v-if="result" class="mt-6 space-y-4">
      <div class="flex items-center gap-3 rounded-lg border px-4 py-3" :class="result.selection?.selected_account ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/50 dark:bg-emerald-900/20' : 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800'">
        <Icon :name="result.selection?.selected_account ? 'checkCircle' : 'infoCircle'" size="md" :class="result.selection?.selected_account ? 'text-emerald-600' : 'text-gray-400'" />
        <div><div class="text-xs" :class="result.selection?.selected_account ? 'text-emerald-700 dark:text-emerald-300' : 'text-gray-500 dark:text-gray-400'">{{ t('admin.routingPolicies.selectedAccount') }}</div><div class="font-medium" :class="result.selection?.selected_account ? 'text-emerald-900 dark:text-emerald-100' : 'text-gray-800 dark:text-gray-200'">{{ result.selection?.selected_account?.name || t('admin.routingPolicies.noCandidate') }}<span v-if="result.selection?.selected_account" class="ml-2 text-xs font-normal">#{{ result.selection.selected_account.id }} · {{ result.selection.selected_account.platform }}</span></div></div>
      </div>
      <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-700/50 dark:text-gray-400"><tr><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.finalSelection') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.account') }}</th><th class="px-3 py-2 text-right">{{ t('admin.routingPolicies.score') }}</th><th class="px-3 py-2 text-right">{{ t('admin.routingPolicies.cost') }}</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.priceStatus') }}</th><th class="px-3 py-2 text-right">{{ t('admin.routingPolicies.errorRate') }}</th><th class="px-3 py-2 text-right">TTFT</th><th class="px-3 py-2 text-left">{{ t('admin.routingPolicies.decision') }}</th></tr></thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700"><tr v-for="candidate in result.selection?.candidates || []" :key="candidate.account_id" :data-test="`simulation-row-${candidate.account_id}`" :class="isSelected(candidate.account_id) && 'bg-primary-50/50 dark:bg-primary-900/10'"><td class="px-3 py-2"><span v-if="isSelected(candidate.account_id)" :data-test="`simulation-selected-${candidate.account_id}`" class="badge badge-success">{{ t('admin.routingPolicies.selected') }}</span><span v-else class="text-gray-300 dark:text-dark-500">-</span></td><td class="px-3 py-2"><div class="font-medium text-gray-900 dark:text-white">{{ candidate.account_name || `#${candidate.account_id}` }}</div><div class="text-xs text-gray-400">{{ candidate.platform }} · #{{ candidate.account_id }}</div></td><td class="px-3 py-2 text-right tabular-nums">{{ candidate.score.toFixed(3) }}</td><td :data-test="`simulation-cost-${candidate.account_id}`" class="px-3 py-2 text-right tabular-nums">{{ candidate.price_known ? `$${candidate.estimated_cost_usd.toFixed(6)}` : '-' }}</td><td class="px-3 py-2"><span :data-test="`simulation-price-${candidate.account_id}`" class="badge" :class="candidate.price_known ? 'badge-success' : 'badge-warning'">{{ candidate.price_known ? t('admin.routingPolicies.priceKnown') : t('admin.routingPolicies.priceUnknown') }}</span></td><td class="px-3 py-2 text-right tabular-nums">{{ (candidate.health.error_rate * 100).toFixed(1) }}%</td><td class="px-3 py-2 text-right tabular-nums">{{ candidate.health.ttft_ms.toFixed(0) }}ms</td><td class="px-3 py-2"><span class="badge" :class="candidate.excluded ? 'badge-danger' : 'badge-success'">{{ candidate.excluded ? exclusionLabel(candidate.exclusion_reason) : t('admin.routingPolicies.available') }}</span></td></tr></tbody>
        </table>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type RoutingPolicy, type RoutingSimulationResult } from '@/api/admin/routingPolicy'
import type { AdminGroup } from '@/types'

const props = defineProps<{ show: boolean; policy: RoutingPolicy | null; groups: AdminGroup[] }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const groupId = ref<number | null>(null)
const model = ref('gpt-4o')
const loading = ref(false)
const result = ref<RoutingSimulationResult | null>(null)
const groupOptions = computed(() => props.groups.map(group => ({
  value: group.id,
  label: `${group.name} ${platformLabel(group.platform)} ${groupStatusLabel(group.status)} #${group.id}`,
  description: `${group.name} ${platformLabel(group.platform)} ${groupStatusLabel(group.status)} ${group.status} #${group.id}`,
  status: group.status,
  group,
})))

watch(() => props.show, show => { if (show) result.value = null })
function platformLabel(platform: string) { return platform === 'openai' ? 'OpenAI' : platform.charAt(0).toUpperCase() + platform.slice(1) }
function optionGroup(option: Record<string, unknown>) { return option.group as AdminGroup }
function groupStatusLabel(status: AdminGroup['status']) { return t(`admin.routingPolicies.groupStatuses.${status}`) }
function groupStatusClass(status: AdminGroup['status']) { return status === 'active' ? 'badge-success' : 'badge-gray' }
function isSelected(accountId: number) { return result.value?.selection?.selected_account?.id === accountId }
function exclusionLabel(reason?: string) { return reason ? t(`admin.routingPolicies.exclusions.${reason}`, reason) : t('admin.routingPolicies.excluded') }
async function run() {
  if (!props.policy || !groupId.value || !model.value.trim()) return
  loading.value = true
  try { result.value = await routingPolicyApi.simulate(props.policy.id, groupId.value, model.value.trim(), props.policy.draft_revision_id || props.policy.published_revision_id || undefined) }
  catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.simulateFailed'))) }
  finally { loading.value = false }
}
</script>
