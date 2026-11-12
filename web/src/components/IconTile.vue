<script setup lang="ts">
import type { Component, HTMLAttributes } from 'vue'

import { cn } from '@/lib/utils'
import { type Tone, toneTile } from '@/lib/tone'

const props = withDefaults(
  defineProps<{
    icon: Component
    tone?: Tone
    size?: 'sm' | 'md' | 'lg'
    class?: HTMLAttributes['class']
  }>(),
  { tone: 'neutral', size: 'md', class: undefined },
)

const box = {
  sm: 'size-7 rounded-md [&>svg]:size-3.5',
  md: 'size-9 rounded-lg [&>svg]:size-4.5',
  lg: 'size-11 rounded-xl [&>svg]:size-5.5',
}
</script>

<template>
  <span
    :class="
      cn('inline-flex shrink-0 items-center justify-center', box[size], toneTile[tone], props.class)
    "
  >
    <component :is="icon" />
  </span>
</template>
