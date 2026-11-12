<script setup lang="ts">
// 表单内分组：小标题 + 说明，组内字段两列排布（窄屏单列）。
withDefaults(defineProps<{ title: string; description?: string; cols?: 1 | 2 }>(), {
  description: '',
  cols: 2,
})
</script>

<template>
  <fieldset class="grid gap-3">
    <legend class="mb-3 w-full border-b pb-2">
      <span class="text-sm font-semibold">{{ title }}</span>
      <span v-if="description" class="text-muted-foreground ml-2 text-xs">{{ description }}</span>
    </legend>
    <div class="grid gap-4" :class="cols === 2 ? 'sm:grid-cols-2' : ''">
      <slot />
    </div>
  </fieldset>
</template>
