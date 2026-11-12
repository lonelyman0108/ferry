<script setup lang="ts">
// 详情顶部一排概要数字。
defineProps<{
  cells: { label: string; value: string; sub?: string; tone?: 'warning' | 'danger' }[]
}>()
</script>

<template>
  <dl class="bg-card grid grid-cols-2 overflow-hidden rounded-lg border sm:grid-cols-4">
    <div
      v-for="c in cells"
      :key="c.label"
      class="min-w-0 border-b p-3 last:border-b-0 sm:border-r sm:border-b-0 sm:last:border-r-0 [&:nth-child(odd)]:border-r"
    >
      <dt class="text-muted-foreground text-xs">{{ c.label }}</dt>
      <dd
        class="truncate font-medium"
        :class="
          c.tone === 'danger' ? 'text-destructive' : c.tone === 'warning' ? 'text-warning' : ''
        "
      >
        {{ c.value }}
      </dd>
      <dd v-if="c.sub" class="text-muted-foreground truncate text-xs">{{ c.sub }}</dd>
    </div>
  </dl>
</template>
