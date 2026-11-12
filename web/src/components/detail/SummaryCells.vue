<script setup lang="ts">
// 详情顶部一排概要数字。列数按详情区的容器宽度切换（详情外层是 @container），不看视口：
// 聚焦页旁边有侧栏和窄栏时，视口宽不代表详情宽。两列时最后一行的格子去掉底边，免得和外框叠成双线。
defineProps<{
  cells: { label: string; value: string; sub?: string; tone?: 'warning' | 'danger' }[]
}>()
</script>

<template>
  <dl class="bg-card grid grid-cols-2 overflow-hidden rounded-lg border @xl:grid-cols-4">
    <div
      v-for="c in cells"
      :key="c.label"
      class="min-w-0 border-b p-3 last:border-b-0 [&:nth-child(odd)]:border-r [&:nth-last-child(2):nth-child(odd)]:border-b-0 @xl:border-r @xl:border-b-0 @xl:last:border-r-0"
    >
      <dt class="text-muted-foreground truncate text-xs" :title="c.label">{{ c.label }}</dt>
      <dd
        class="truncate font-medium"
        :class="
          c.tone === 'danger' ? 'text-destructive' : c.tone === 'warning' ? 'text-warning' : ''
        "
        :title="c.value"
      >
        {{ c.value }}
      </dd>
      <dd v-if="c.sub" class="text-muted-foreground truncate text-xs" :title="c.sub">
        {{ c.sub }}
      </dd>
    </div>
  </dl>
</template>
