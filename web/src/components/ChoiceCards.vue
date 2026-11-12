<script setup lang="ts" generic="T extends string">
import { CheckIcon } from '@lucide/vue'
import type { Component } from 'vue'

import { cn } from '@/lib/utils'
import BrandIcon from './BrandIcon.vue'
import { type Tone, toneTile } from '@/lib/tone'

// 单选卡片：替代下拉框，用在类型这类少量、需要一眼区分的选项上。
const model = defineModel<T>({ required: true })
defineProps<{
  options: {
    value: T
    label: string
    description?: string
    icon?: Component
    tone?: Tone
    // 品牌图标名（lib/brands），优先于 icon
    brand?: string
  }[]
  disabled?: boolean
  label?: string
  // 不给时一行排开；选项多时按 2 / 3 列换行
  cols?: 2 | 3
}>()
const colClass = { 2: 'grid-cols-2', 3: 'grid-cols-2 sm:grid-cols-3' }
</script>

<template>
  <div
    role="radiogroup"
    :aria-label="label"
    :class="cn('grid gap-2', cols ? colClass[cols] : 'sm:auto-cols-fr sm:grid-flow-col')"
  >
    <button
      v-for="o in options"
      :key="o.value"
      type="button"
      role="radio"
      :aria-checked="model === o.value"
      :disabled="disabled && model !== o.value"
      :class="
        cn(
          'flex items-center gap-3 rounded-lg border p-3 text-left transition',
          model === o.value
            ? 'border-primary bg-primary/5 ring-primary/30 ring-2'
            : 'hover:border-foreground/25 hover:bg-muted/40',
          disabled && model !== o.value ? 'cursor-not-allowed opacity-40' : '',
        )
      "
      @click="!disabled && (model = o.value)"
    >
      <BrandIcon v-if="o.brand" :brand="o.brand" size="sm" class="size-8" />
      <span
        v-else-if="o.icon"
        :class="
          cn(
            'inline-flex size-8 shrink-0 items-center justify-center rounded-md [&>svg]:size-4',
            toneTile[o.tone ?? 'neutral'],
          )
        "
        ><component :is="o.icon"
      /></span>
      <span class="min-w-0 flex-1">
        <span class="block text-sm font-medium [overflow-wrap:anywhere]">{{ o.label }}</span>
        <span v-if="o.description" class="text-muted-foreground block text-xs">{{
          o.description
        }}</span>
      </span>
      <!-- 单选圆点固定在右侧居中，不随文字长短漂移，也不压住说明文字 -->
      <span
        :class="
          cn(
            'flex size-4 shrink-0 items-center justify-center rounded-full border transition',
            model === o.value
              ? 'border-primary bg-primary text-primary-foreground'
              : 'border-input',
          )
        "
        ><CheckIcon v-if="model === o.value" class="size-3" :stroke-width="3"
      /></span>
    </button>
  </div>
</template>
