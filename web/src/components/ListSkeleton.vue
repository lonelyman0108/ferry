<script setup lang="ts">
import type { HTMLAttributes } from 'vue'

import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

// 加载中的占位。cards：一行一张卡片，形状和 ListRow 相近；table：表格外框里几行细条。
// class 改容器的排列（如多列网格），item-class 改每一块的高度。
const props = withDefaults(
  defineProps<{
    rows?: number
    variant?: 'cards' | 'table'
    class?: HTMLAttributes['class']
    itemClass?: HTMLAttributes['class']
  }>(),
  { rows: 3, variant: 'cards', class: undefined, itemClass: undefined },
)
</script>

<template>
  <div
    v-if="variant === 'table'"
    :class="cn('bg-card grid gap-3 rounded-xl border p-4', props.class)"
    aria-busy="true"
    aria-label="加载中"
  >
    <Skeleton v-for="i in rows" :key="i" :class="cn('h-5', itemClass)" />
  </div>
  <div v-else :class="cn('grid gap-3', props.class)" aria-busy="true" aria-label="加载中">
    <Skeleton v-for="i in rows" :key="i" :class="cn('h-[4.5rem] rounded-xl', itemClass)" />
  </div>
</template>
