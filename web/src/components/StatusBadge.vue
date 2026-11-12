<script setup lang="ts">
import type { Component, HTMLAttributes } from 'vue'

import { cn } from '@/lib/utils'
import { type Tone, toneBadge, toneDot } from '@/lib/tone'

// 带颜色的状态徽章：dot 显示前面的状态点，icon 显示一个小图标（二选一）。
const props = withDefaults(
  defineProps<{
    tone?: Tone
    dot?: boolean
    icon?: Component
    class?: HTMLAttributes['class']
  }>(),
  { tone: 'neutral', dot: false, icon: undefined, class: undefined },
)
</script>

<template>
  <span
    :class="
      cn(
        'inline-flex h-5 shrink-0 items-center gap-1 rounded-full border px-2 text-xs font-medium whitespace-nowrap [&>svg]:size-3',
        toneBadge[tone],
        props.class,
      )
    "
  >
    <span v-if="dot" :class="cn('size-1.5 rounded-full', toneDot[tone])" />
    <component :is="icon" v-else-if="icon" />
    <slot />
  </span>
</template>
