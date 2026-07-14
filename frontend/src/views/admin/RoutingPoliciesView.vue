<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="space-y-4">
          <div class="flex min-w-0 flex-col justify-between gap-4 lg:flex-row lg:items-center">
            <div class="inline-flex w-fit rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
              <button type="button" class="rounded-md px-4 py-2 text-sm font-medium transition-colors" :class="activeView === 'policies' ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-600 dark:text-primary-400' : 'text-gray-500 dark:text-gray-400'" @click="activeView = 'policies'">{{ t('admin.routingPolicies.policies') }}</button>
              <button type="button" class="rounded-md px-4 py-2 text-sm font-medium transition-colors" :class="activeView === 'books' ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-600 dark:text-primary-400' : 'text-gray-500 dark:text-gray-400'" @click="activeView = 'books'">{{ t('admin.routingPolicies.priceBooks') }}</button>
            </div>
            <div class="flex w-full min-w-0 flex-wrap items-center gap-3 lg:w-auto">
              <div class="relative w-full min-w-0 sm:min-w-56 sm:flex-1 lg:w-auto lg:flex-none"><Icon name="search" size="sm" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" /><input v-model="search" class="input w-full min-w-0 pl-9" :placeholder="t('common.searchPlaceholder')" /></div>
              <button class="btn btn-secondary" :title="t('common.refresh')" :disabled="loading" @click="reload"><Icon name="refresh" size="md" :class="loading && 'animate-spin'" /></button>
              <button v-if="activeView === 'policies'" class="btn btn-primary" @click="openCreatePolicy"><Icon name="plus" size="md" class="mr-2" />{{ t('admin.routingPolicies.create') }}</button>
              <button v-else class="btn btn-primary" @click="openCreateBook"><Icon name="plus" size="md" class="mr-2" />{{ t('admin.routingPolicies.createPriceBook') }}</button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable v-if="activeView === 'policies'" :columns="policyColumns" :data="filteredPolicies" :loading="loading" row-key="id">
          <template #cell-name="{ row }"><div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div><div class="mt-0.5 max-w-64 truncate text-xs text-gray-400">{{ row.description || `#${row.id}` }}</div></template>
          <template #cell-status="{ row }"><span class="badge" :class="statusClass(row.status)">{{ t(`admin.routingPolicies.statuses.${row.status}`) }}</span></template>
          <template #cell-revisions="{ row }"><div class="space-y-1 text-xs"><div><span class="text-gray-400">{{ t('admin.routingPolicies.publishedShort') }}</span> {{ row.published_revision_id ? `#${row.published_revision_id}` : '-' }}</div><div><span class="text-gray-400">{{ t('admin.routingPolicies.draftShort') }}</span> {{ row.draft_revision_id ? `#${row.draft_revision_id}` : '-' }}</div></div></template>
          <template #cell-updated_at="{ row }"><span class="text-xs text-gray-500">{{ formatDate(row.updated_at) }}</span></template>
          <template #cell-actions="{ row }"><div class="flex items-center justify-end gap-1"><ActionButton icon="edit" :label="t('common.edit')" @click="openEditPolicy(row)" /><ActionButton icon="link" :label="t('admin.routingPolicies.bind')" :disabled="!row.published_revision_id" @click="openBinding(row)" /><ActionButton icon="play" :label="t('admin.routingPolicies.simulate')" @click="openSimulation(row)" /><ActionButton icon="clock" :label="t('admin.routingPolicies.versions')" @click="openVersions(row)" /><ActionButton v-if="row.draft_revision_id" icon="upload" :label="t('admin.routingPolicies.publish')" @click="requestPublish(row)" /><ActionButton icon="trash" :label="t('common.delete')" danger @click="requestDelete(row)" /></div></template>
        </DataTable>

        <DataTable v-else :columns="bookColumns" :data="filteredBooks" :loading="loading" row-key="id">
          <template #cell-name="{ row }"><div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div><div class="text-xs text-gray-400">#{{ row.id }}</div></template>
          <template #cell-source="{ row }"><span class="badge badge-gray">{{ row.source === 'http_json' ? 'HTTP JSON' : 'Manual' }}</span></template>
          <template #cell-status="{ row }"><span class="badge" :class="statusClass(row.status)">{{ t(`admin.routingPolicies.statuses.${row.status}`) }}</span></template>
          <template #cell-latest_revision_id="{ row }"><span class="text-sm">{{ row.latest_revision_id ? `#${row.latest_revision_id}` : '-' }}</span></template>
          <template #cell-actions="{ row }"><div class="flex items-center justify-end gap-1"><ActionButton icon="edit" :label="t('common.edit')" @click="openEditBook(row)" /><ActionButton v-if="row.source === 'http_json'" icon="refresh" :label="t('admin.routingPolicies.sync')" :loading="syncingBookIds.has(row.id)" :disabled="syncingBookIds.has(row.id)" @click="syncBook(row)" /><ActionButton icon="plus" :label="t('admin.routingPolicies.addRevision')" @click="openPriceRevision(row)" /><ActionButton icon="clock" :label="t('admin.routingPolicies.versions')" @click="openPriceVersions(row)" /></div></template>
        </DataTable>
      </template>
    </TablePageLayout>

    <RoutingPolicyFormDialog :show="showPolicyForm" :policy="selectedPolicy" :revision="selectedRevision" :groups="groups" @close="showPolicyForm = false" @saved="handlePolicySaved" />
    <RoutingPolicyBindingDialog :show="showBinding" :policy="selectedPolicy" :groups="groups" :versions="policyVersions" @close="showBinding = false" @bound="handleBound" />
    <RoutingPolicySimulationDialog :show="showSimulation" :policy="selectedPolicy" :groups="groups" @close="showSimulation = false" />
    <RoutingPolicyVersionsDialog :show="showVersions" :policy="selectedPolicy" :versions="policyVersions" @close="showVersions = false" @restored="handleVersionChanged" @publish="requestPublishVersion" />
    <PriceBookFormDialog :show="showBookForm" :book="selectedBook" @close="showBookForm = false" @saved="handleBookSaved" />
    <PriceBookRevisionDialog :show="showPriceRevision" :book="selectedBook" :initial-prices="latestBookPrices" @close="showPriceRevision = false" @saved="handlePriceRevisionSaved" />
    <PriceBookVersionsDialog :show="showPriceVersions" :book="selectedBook" :revisions="bookVersions" @close="showPriceVersions = false" @published="handlePriceVersionPublished" />

    <ConfirmDialog :show="!!deleteTarget" :title="t('admin.routingPolicies.deleteTitle')" :message="t('admin.routingPolicies.confirmDeleteNamed', { name: deleteTarget?.name })" :confirm-text="t('common.delete')" danger @confirm="deletePolicy" @cancel="deleteTarget = null" />
    <ConfirmDialog :show="!!publishTarget" :title="t('admin.routingPolicies.publishTitle')" :message="t('admin.routingPolicies.publishConfirm', { name: publishTarget?.policy.name, version: publishTarget?.version.version })" :confirm-text="t('admin.routingPolicies.publish')" @confirm="publishPolicy" @cancel="publishTarget = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import RoutingPolicyFormDialog from '@/components/admin/routing/RoutingPolicyFormDialog.vue'
import RoutingPolicyBindingDialog from '@/components/admin/routing/RoutingPolicyBindingDialog.vue'
import RoutingPolicySimulationDialog from '@/components/admin/routing/RoutingPolicySimulationDialog.vue'
import RoutingPolicyVersionsDialog from '@/components/admin/routing/RoutingPolicyVersionsDialog.vue'
import PriceBookFormDialog from '@/components/admin/routing/PriceBookFormDialog.vue'
import PriceBookRevisionDialog from '@/components/admin/routing/PriceBookRevisionDialog.vue'
import PriceBookVersionsDialog from '@/components/admin/routing/PriceBookVersionsDialog.vue'
import { adminAPI } from '@/api/admin'
import { routingPolicyApi, type PriceBook, type PriceBookRevision, type RoutingPolicy, type RoutingRevision } from '@/api/admin/routingPolicy'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'

const ActionButton = defineComponent({
  props: { icon: { type: String, required: true }, label: { type: String, required: true }, danger: Boolean, disabled: Boolean, loading: Boolean },
  emits: ['click'],
  setup(props, { emit }) { return () => h('button', { type: 'button', disabled: props.disabled, title: props.label, class: ['flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-xs transition-colors', props.danger ? 'text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20' : 'text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400', props.disabled && 'cursor-not-allowed opacity-40'], onClick: () => !props.disabled && emit('click') }, [h(Icon, { name: props.icon as any, size: 'sm', class: props.loading && 'animate-spin' }), h('span', props.label)]) }
})

const { t } = useI18n()
const appStore = useAppStore()
const activeView = ref<'policies' | 'books'>('policies')
const search = ref('')
const loading = ref(false)
const policies = ref<RoutingPolicy[]>([])
const priceBooks = ref<PriceBook[]>([])
const groups = ref<AdminGroup[]>([])
const selectedPolicy = ref<RoutingPolicy | null>(null)
const selectedRevision = ref<RoutingRevision | null>(null)
const policyVersions = ref<RoutingRevision[]>([])
const selectedBook = ref<PriceBook | null>(null)
const bookVersions = ref<PriceBookRevision[]>([])
const showPolicyForm = ref(false)
const showBinding = ref(false)
const showSimulation = ref(false)
const showVersions = ref(false)
const showBookForm = ref(false)
const showPriceRevision = ref(false)
const showPriceVersions = ref(false)
const deleteTarget = ref<RoutingPolicy | null>(null)
const publishTarget = ref<{ policy: RoutingPolicy; version: RoutingRevision } | null>(null)
const syncingBookIds = ref(new Set<number>())

const policyColumns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.routingPolicies.name') },
  { key: 'status', label: t('admin.routingPolicies.status') },
  { key: 'revisions', label: t('admin.routingPolicies.revision') },
  { key: 'updated_at', label: t('admin.routingPolicies.updatedAt') },
  { key: 'actions', label: t('common.actions'), class: 'text-right' },
])
const bookColumns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.routingPolicies.name') },
  { key: 'source', label: t('admin.routingPolicies.source') },
  { key: 'status', label: t('admin.routingPolicies.status') },
  { key: 'currency', label: t('admin.routingPolicies.currency') },
  { key: 'latest_revision_id', label: t('admin.routingPolicies.revision') },
  { key: 'actions', label: t('common.actions'), class: 'text-right' },
])
const filteredPolicies = computed(() => { const query = search.value.trim().toLowerCase(); return query ? policies.value.filter(item => item.name.toLowerCase().includes(query) || item.description.toLowerCase().includes(query)) : policies.value })
const filteredBooks = computed(() => { const query = search.value.trim().toLowerCase(); return query ? priceBooks.value.filter(item => item.name.toLowerCase().includes(query)) : priceBooks.value })
const latestBookPrices = computed(() => bookVersions.value[0]?.prices || [])

function statusClass(status: string) { return status === 'active' ? 'badge-success' : status === 'disabled' ? 'badge-warning' : 'badge-gray' }
function formatDate(value: string) { return value ? new Date(value).toLocaleString() : '-' }
async function reload() {
  loading.value = true
  try {
    const [policyList, books, groupList] = await Promise.all([routingPolicyApi.list(), routingPolicyApi.listPriceBooks(), adminAPI.groups.getAllIncludingInactive()])
    policies.value = policyList
    priceBooks.value = books
    groups.value = groupList
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadFailed'))) }
  finally { loading.value = false }
}
async function loadPolicyVersions(policy: RoutingPolicy) { policyVersions.value = await routingPolicyApi.versions(policy.id); selectedRevision.value = policyVersions.value.find(version => version.id === policy.draft_revision_id) || policyVersions.value.find(version => version.id === policy.published_revision_id) || policyVersions.value[0] || null }
async function loadBookVersions(book: PriceBook) { bookVersions.value = await routingPolicyApi.revisions(book.id) }
function openCreatePolicy() { selectedPolicy.value = null; selectedRevision.value = null; showPolicyForm.value = true }
async function openEditPolicy(policy: RoutingPolicy) { selectedPolicy.value = policy; try { await loadPolicyVersions(policy); showPolicyForm.value = true } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
async function openBinding(policy: RoutingPolicy) { selectedPolicy.value = policy; try { await loadPolicyVersions(policy); showBinding.value = true } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
function openSimulation(policy: RoutingPolicy) { selectedPolicy.value = policy; showSimulation.value = true }
async function openVersions(policy: RoutingPolicy) { selectedPolicy.value = policy; try { await loadPolicyVersions(policy); showVersions.value = true } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
function requestDelete(policy: RoutingPolicy) { deleteTarget.value = policy }
async function requestPublish(policy: RoutingPolicy) { try { await loadPolicyVersions(policy); const draft = policyVersions.value.find(version => version.id === policy.draft_revision_id); if (draft) publishTarget.value = { policy, version: draft } } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
function requestPublishVersion(version: RoutingRevision) { if (selectedPolicy.value) publishTarget.value = { policy: selectedPolicy.value, version } }
async function publishPolicy() { if (!publishTarget.value) return; try { await routingPolicyApi.publish(publishTarget.value.policy.id, publishTarget.value.version.id); appStore.showSuccess(t('admin.routingPolicies.published')); publishTarget.value = null; showVersions.value = false; await reload() } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.publishFailed'))) } }
async function deletePolicy() { if (!deleteTarget.value) return; try { await routingPolicyApi.remove(deleteTarget.value.id); appStore.showSuccess(t('admin.routingPolicies.deleted')); deleteTarget.value = null; await reload() } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.deleteFailed'))) } }
async function handlePolicySaved() { showPolicyForm.value = false; await reload() }
function handleBound() { showBinding.value = false }
async function handleVersionChanged() { showVersions.value = false; await reload() }
function openCreateBook() { selectedBook.value = null; showBookForm.value = true }
function openEditBook(book: PriceBook) { selectedBook.value = book; showBookForm.value = true }
async function openPriceRevision(book: PriceBook) { selectedBook.value = book; try { await loadBookVersions(book); showPriceRevision.value = true } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
async function openPriceVersions(book: PriceBook) { selectedBook.value = book; try { await loadBookVersions(book); showPriceVersions.value = true } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.loadVersionsFailed'))) } }
async function syncBook(book: PriceBook) {
  if (syncingBookIds.value.has(book.id)) return
  syncingBookIds.value = new Set(syncingBookIds.value).add(book.id)
  try {
    await routingPolicyApi.syncPriceBook(book.id)
    appStore.showSuccess(t('admin.routingPolicies.synced'))
    await reload()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.routingPolicies.syncFailed')))
  } finally {
    const next = new Set(syncingBookIds.value)
    next.delete(book.id)
    syncingBookIds.value = next
  }
}
async function handleBookSaved() { showBookForm.value = false; await reload() }
async function handlePriceRevisionSaved() { showPriceRevision.value = false; await reload() }
async function handlePriceVersionPublished() { showPriceVersions.value = false; await reload() }
onMounted(reload)
</script>
