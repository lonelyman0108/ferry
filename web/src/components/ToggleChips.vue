<script setup lang="ts" generic="T extends string | number">
import { CheckIcon } from '@lucide/vue'

import { cn } from '@/lib/utils'
import { type Tone, toneDot } from '@/lib/tone'

// 多选标签：点一下选中、再点取消，替代成组的复选框。
const model = defineModel<T[]>({ required: true })
defineProps<{ options: { value: T; label: string; tone?: Tone }[]; label?: string }>()

function toggle(v: T) {
  model.value = model.value.includes(v) ? model.value.filter((x) => x !== v) : [...model.value, v]
}
</script>

<template>
  <div role="group" :aria-label="label" class="flex flex-wrap gap-2">
    <button
      v-for="o in options"
      :key="o.value"
      type="button"
      :aria-pressed="model.includes(o.value)"
      :class="
        cn(
          'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm transition max-md:h-11',
          model.includes(o.value)
            ? 'border-primary bg-primary/10 text-foreground'
            : 'text-muted-foreground hover:border-foreground/25 hover:text-foreground',
        )
      "
      @click="toggle(o.value)"
    >
      <CheckIcon v-if="model.includes(o.value)" class="text-primary size-3.5" />
      <span v-else-if="o.tone" :class="cn('size-1.5 rounded-full', toneDot[o.tone])" />
      {{ o.label }}
    </button>
  </div>
</template>
