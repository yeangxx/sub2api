<template>
  <div class="border-t border-gray-200 pt-5 dark:border-dark-600">
    <div class="mb-4 flex items-center gap-2">
      <Icon name="sort" size="sm" class="text-primary-500" />
      <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.accounts.routing.title') }}</h4>
    </div>
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
      <div>
        <label class="input-label">{{ t('admin.accounts.routing.failureDomain') }}</label>
        <input :value="failureDomain" class="input" :placeholder="t('admin.accounts.routing.failureDomainPlaceholder')" @input="emit('update:failureDomain', ($event.target as HTMLInputElement).value)" />
        <p class="input-hint">{{ t('admin.accounts.routing.failureDomainHint') }}</p>
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.routing.reliabilityClass') }}</label>
        <Select :model-value="reliabilityClass" :options="reliabilityOptions" creatable clearable :placeholder="t('admin.accounts.routing.reliabilityPlaceholder')" @update:model-value="emit('update:reliabilityClass', String($event || ''))" />
      </div>
      <div class="md:col-span-2">
        <label class="input-label">{{ t('admin.accounts.routing.priceBook') }}</label>
        <Select
          data-testid="price-book-select"
          :model-value="priceBookId"
          :options="priceBookOptions"
          :disabled="priceBooksLoading || priceBooksLoadFailed"
          clearable
          searchable
          :placeholder="priceBooksLoading ? t('admin.accounts.routing.priceBookLoading') : t('admin.accounts.routing.priceBookPlaceholder')"
          @update:model-value="emit('update:priceBookId', $event == null ? null : Number($event))"
        />
        <p v-if="priceBooksLoading" class="input-hint">
          {{ t('admin.accounts.routing.priceBookLoading') }}
        </p>
        <div
          v-else-if="priceBooksLoadFailed"
          data-testid="price-book-load-error"
          class="mt-2 flex flex-wrap items-center justify-between gap-2 text-sm text-red-600 dark:text-red-400"
        >
          <span>{{ t('admin.accounts.routing.priceBookLoadFailed') }}</span>
          <button
            type="button"
            data-testid="retry-price-books"
            class="btn btn-secondary !px-3 !py-1.5 text-xs"
            @click="loadPriceBooks"
          >
            <Icon name="refresh" size="sm" />
            {{ t('admin.accounts.routing.priceBookRetry') }}
          </button>
        </div>
      </div>
      <div class="md:col-span-2">
        <label class="input-label">{{ t('admin.accounts.routing.labels') }}</label>
        <KeyValueEditor :model-value="routingLabels" @update:model-value="emit('update:routingLabels', $event)" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyValueEditor from '@/components/admin/routing/KeyValueEditor.vue'
import { routingPolicyApi, type PriceBook } from '@/api/admin/routingPolicy'

defineProps<{ failureDomain: string; reliabilityClass: string; routingLabels: Record<string, string>; priceBookId: number | null }>()
const emit = defineEmits<{
  'update:failureDomain': [value: string]
  'update:reliabilityClass': [value: string]
  'update:routingLabels': [value: Record<string, string>]
  'update:priceBookId': [value: number | null]
}>()
const { t } = useI18n()
const appStore = useAppStore()
const priceBooks = ref<PriceBook[]>([])
const priceBooksLoading = ref(true)
const priceBooksLoadFailed = ref(false)
const reliabilityOptions = ['standard', 'trusted', 'partner', 'official'].map(value => ({ value, label: value }))
const priceBookOptions = computed(() => priceBooks.value.map(book => ({ value: book.id, label: `${book.name} (${book.currency})`, disabled: book.status !== 'active' })))

const loadPriceBooks = async () => {
  priceBooksLoading.value = true
  priceBooksLoadFailed.value = false
  try {
    priceBooks.value = await routingPolicyApi.listPriceBooks()
  } catch {
    priceBooks.value = []
    priceBooksLoadFailed.value = true
    appStore.showError(t('admin.accounts.routing.priceBookLoadFailed'))
  } finally {
    priceBooksLoading.value = false
  }
}

onMounted(loadPriceBooks)
</script>
