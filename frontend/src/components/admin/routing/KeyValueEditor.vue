<template>
  <div class="space-y-2">
    <div v-for="row in rows" :key="row.id" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_36px] gap-2">
      <input v-model="row.key" class="input" :placeholder="keyPlaceholder" @input="emitValue" />
      <input v-model="row.value" class="input" :placeholder="valuePlaceholder" @input="emitValue" />
      <button type="button" class="btn btn-secondary px-2" :title="t('common.delete')" @click="remove(row.id)">
        <Icon name="trash" size="sm" />
      </button>
    </div>
    <button type="button" class="btn btn-secondary btn-sm" @click="add">
      <Icon name="plus" size="sm" class="mr-1" />
      {{ addLabel || t('common.add') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { newKeyValueRow, recordsToRows, rowsToRecord, type KeyValueRow } from '@/views/admin/routingPolicyForm'

const props = withDefaults(defineProps<{
  modelValue: Record<string, string>
  keyPlaceholder?: string
  valuePlaceholder?: string
  addLabel?: string
}>(), {
  keyPlaceholder: 'Key',
  valuePlaceholder: 'Value',
})

const emit = defineEmits<{ 'update:modelValue': [value: Record<string, string>] }>()
const { t } = useI18n()
const rows = ref<KeyValueRow[]>(recordsToRows(props.modelValue))

watch(() => props.modelValue, (value) => {
  if (JSON.stringify(rowsToRecord(rows.value)) !== JSON.stringify(value || {})) {
    rows.value = recordsToRows(value || {})
  }
}, { deep: true })

function emitValue() {
  emit('update:modelValue', rowsToRecord(rows.value))
}

function add() {
  rows.value.push(newKeyValueRow())
}

function remove(id: string) {
  rows.value = rows.value.filter((row) => row.id !== id)
  emitValue()
}
</script>
