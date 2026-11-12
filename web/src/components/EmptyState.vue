<script setup lang="ts">
import type { Component } from 'vue'

import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

// 列表为空时的占位：图标、标题、一句说明；默认插槽放下一步要点的按钮，description 插槽可以放带链接的说明。
defineProps<{ icon: Component; title: string; description?: string }>()
</script>

<template>
  <Empty class="border border-dashed">
    <EmptyHeader>
      <EmptyMedia variant="icon"><component :is="icon" /></EmptyMedia>
      <EmptyTitle>{{ title }}</EmptyTitle>
      <EmptyDescription v-if="description || $slots.description">
        <slot name="description">{{ description }}</slot>
      </EmptyDescription>
    </EmptyHeader>
    <EmptyContent v-if="$slots.default">
      <div class="flex flex-wrap justify-center gap-2"><slot /></div>
    </EmptyContent>
  </Empty>
</template>
