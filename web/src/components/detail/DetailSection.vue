<script setup lang="ts">
// 详情里的一个分区：标题、右侧的计数或说明，下面是内容。
defineProps<{ title: string; aside?: string }>()
</script>

<template>
  <section class="grid min-w-0 content-start gap-3">
    <div class="flex items-baseline justify-between gap-3">
      <h2 class="font-semibold">{{ title }}</h2>
      <span v-if="aside" class="text-muted-foreground text-xs">{{ aside }}</span>
      <slot name="aside" />
    </div>
    <slot />
  </section>
</template>
