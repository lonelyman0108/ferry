<script setup lang="ts">
import type { HTMLAttributes } from 'vue'

import { brands } from '@/lib/brands'
import { cn } from '@/lib/utils'

// 品牌图标，尺寸与 IconTile 对齐，可以在同一列表里混用。
const props = withDefaults(
  defineProps<{ brand: string; size?: 'sm' | 'md' | 'lg'; class?: HTMLAttributes['class'] }>(),
  { size: 'md', class: undefined },
)

const box = { sm: 'size-7 rounded-md', md: 'size-9 rounded-lg', lg: 'size-11 rounded-xl' }
const pad = { sm: 'p-1', md: 'p-1.5', lg: 'p-2' }
</script>

<template>
  <span
    v-if="brands[brand]"
    :class="
      cn(
        'ring-foreground/10 inline-flex shrink-0 overflow-hidden ring-1',
        box[size],
        brands[brand].logo && ['bg-white', pad[size]],
        props.class,
      )
    "
  >
    <img :src="brands[brand].src" alt="" class="size-full object-contain" />
  </span>
</template>
