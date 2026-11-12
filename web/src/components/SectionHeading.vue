<script setup lang="ts">
import type { Component } from 'vue'

// 页面内的二级标题：小图标 + 标题，可带说明和右侧操作。
defineProps<{ title: string; description?: string; icon?: Component }>()
</script>

<template>
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <div class="mr-auto min-w-0">
      <h2 class="flex items-center gap-2 text-base font-semibold">
        <component :is="icon" v-if="icon" class="text-muted-foreground size-4" />
        {{ title }}
        <slot name="badges" />
      </h2>
      <p v-if="description" class="text-muted-foreground mt-0.5 text-sm">{{ description }}</p>
    </div>
    <slot name="actions" />
  </div>
</template>
