<template>
  <BaseDialog :show="show" :title="t('admin.routingPolicies.bindingTitle')" width="normal" @close="closeDialog">
    <form id="routing-binding-form" class="space-y-5" @submit.prevent="submit">
      <div>
        <label class="input-label">{{ t('admin.routingPolicies.group') }} <span class="text-red-500">*</span></label>
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
        <label class="input-label">{{ t('admin.routingPolicies.bindingMode') }}</label>
        <Select v-model="mode" :options="modeOptions" />
        <p class="input-hint">{{ mode === 'shadow' ? t('admin.routingPolicies.shadowHint') : t('admin.routingPolicies.enforceHint') }}</p>
      </div>
      <div>
        <label class="input-label">{{ t('admin.routingPolicies.revision') }}</label>
        <Select v-model="revisionId" :options="revisionOptions" :placeholder="t('admin.routingPolicies.followPublished')" clearable />
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" type="submit" form="routing-binding-form" :disabled="saving || !groupId">
          <Icon name="link" size="sm" class="mr-2" />{{ t('admin.routingPolicies.bind') }}
        </button>
      </div>
    </template>
  </BaseDialog>
  <ConfirmDialog
    :show="!!pendingBinding"
    :title="t('admin.routingPolicies.bindingConfirmTitle')"
    :message="bindingConfirmMessage"
    :confirm-text="t('admin.routingPolicies.bind')"
    :loading="saving"
    @confirm="confirmBinding"
    @cancel="cancelPendingBinding"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { routingPolicyApi, type RoutingMode, type RoutingPolicy, type RoutingRevision } from '@/api/admin/routingPolicy'
import type { AdminGroup } from '@/types'

const props = defineProps<{ show: boolean; policy: RoutingPolicy | null; groups: AdminGroup[]; versions: RoutingRevision[] }>()
const emit = defineEmits<{ close: []; bound: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const groupId = ref<number | null>(null)
const mode = ref<RoutingMode>('shadow')
const revisionId = ref<number | null>(null)
const saving = ref(false)
const pendingBinding = ref<{ groupId: number; groupName: string; mode: RoutingMode; revisionId?: number } | null>(null)

const groupOptions = computed(() => props.groups.map(group => ({
  value: group.id,
  label: `${group.name} ${platformLabel(group.platform)} ${groupStatusLabel(group.status)} #${group.id}`,
  description: `${group.name} ${platformLabel(group.platform)} ${groupStatusLabel(group.status)} ${group.status} #${group.id}`,
  disabled: group.status !== 'active',
  status: group.status,
  group,
})))
const modeOptions = computed(() => [
  { value: 'shadow', label: t('admin.routingPolicies.shadow') },
  { value: 'enforce', label: t('admin.routingPolicies.enforce') },
])
const revisionOptions = computed(() => props.versions.filter(version => version.state === 'published').map(version => ({ value: version.id, label: `v${version.version} · ${t('admin.routingPolicies.published')}` })))
const bindingConfirmMessage = computed(() => pendingBinding.value
  ? t('admin.routingPolicies.bindingConfirm', {
      group: pendingBinding.value.groupName,
      mode: t(`admin.routingPolicies.${pendingBinding.value.mode}`),
    })
  : '')

watch(() => props.show, show => {
  if (!show) return
  groupId.value = null
  mode.value = 'shadow'
  revisionId.value = props.policy?.published_revision_id || null
  pendingBinding.value = null
}, { immediate: true })

function platformLabel(platform: string) {
  return platform === 'openai' ? 'OpenAI' : platform.charAt(0).toUpperCase() + platform.slice(1)
}
function optionGroup(option: Record<string, unknown>) {
  return option.group as AdminGroup
}
function groupStatusLabel(status: AdminGroup['status']) {
  return t(`admin.routingPolicies.groupStatuses.${status}`)
}
function groupStatusClass(status: AdminGroup['status']) {
  return status === 'active' ? 'badge-success' : 'badge-gray'
}
function submit() {
  if (!props.policy || !groupId.value) return
  const group = props.groups.find(item => item.id === groupId.value)
  if (!group || group.status !== 'active') return
  pendingBinding.value = {
    groupId: group.id,
    groupName: group.name,
    mode: mode.value,
    revisionId: revisionId.value || undefined,
  }
}
function cancelPendingBinding() {
  if (saving.value) return
  pendingBinding.value = null
}
function closeDialog() {
  if (pendingBinding.value) {
    cancelPendingBinding()
    return
  }
  if (!saving.value) emit('close')
}
async function confirmBinding() {
  if (!props.policy || !pendingBinding.value || saving.value) return
  const binding = pendingBinding.value
  saving.value = true
  try {
    await routingPolicyApi.bindGroup(props.policy.id, binding.groupId, binding.mode, binding.revisionId)
    appStore.showSuccess(t('admin.routingPolicies.bound'))
    pendingBinding.value = null
    emit('bound')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.bindFailed')))
  } finally {
    saving.value = false
  }
}
</script>
