<script setup lang="ts">
import { PlusIcon, Trash2Icon } from '@lucide/vue'
import { ref, watch } from 'vue'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

// 编辑请求头这类键值对。内部用数组保存，键为空的行不会写回。
const model = defineModel<Record<string, string> | undefined>()
withDefaults(defineProps<{ keyPlaceholder?: string; valuePlaceholder?: string }>(), {
  keyPlaceholder: '名称',
  valuePlaceholder: '值',
})

const rows = ref<{ k: string; v: string }[]>([])

function collect(): Record<string, string> {
  const out: Record<string, string> = {}
  for (const { k, v } of rows.value) if (k.trim()) out[k.trim()] = v
  return out
}

// 外部值和当前行一致时不重建，否则正在填写的空行会被吞掉。
watch(
  model,
  (m) => {
    if (JSON.stringify(collect()) === JSON.stringify(m ?? {})) return
    rows.value = Object.entries(m ?? {}).map(([k, v]) => ({ k, v }))
  },
  { immediate: true },
)

watch(
  rows,
  () => {
    const out = collect()
    model.value = Object.keys(out).length ? out : undefined
  },
  { deep: true },
)
</script>

<template>
  <div class="grid gap-2">
    <div v-for="(r, i) in rows" :key="i" class="flex gap-2">
      <Input
        v-model="r.k"
        :name="`kv-key-${i}`"
        :aria-label="keyPlaceholder"
        :placeholder="keyPlaceholder"
        class="w-2/5 font-mono"
      />
      <Input
        v-model="r.v"
        :name="`kv-value-${i}`"
        :aria-label="valuePlaceholder"
        :placeholder="valuePlaceholder"
        class="flex-1 font-mono"
      />
      <Button variant="ghost" size="icon" aria-label="删除" @click="rows.splice(i, 1)">
        <Trash2Icon />
      </Button>
    </div>
    <div>
      <Button variant="outline" size="sm" @click="rows.push({ k: '', v: '' })">
        <PlusIcon /> 添加
      </Button>
    </div>
  </div>
</template>
