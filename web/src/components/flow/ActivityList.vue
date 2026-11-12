<script setup lang="ts">
import { CheckCheckIcon, DownloadIcon } from '@lucide/vue'

import { fmtAgo, fmtTime } from '@/format'
import { icons } from '@/lib/icons'
import type { ActivityItem } from '@/types'

// 记录时间线：回传、审核、推送、通知按时间倒序。结果用圆点颜色和文字一起表示。
// compact 用在详情里，不显示来源名（都是同一个来源）。
defineProps<{ items: ActivityItem[]; compact?: boolean }>()

const kindIcons = {
  sync: DownloadIcon,
  review: CheckCheckIcon,
  push: icons.push,
  notify: icons.notify,
}
const outcome: Record<string, { dot: string; label: string }> = {
  success: { dot: 'bg-success', label: '成功' },
  warning: { dot: 'bg-warning', label: '异常' },
  failure: { dot: 'bg-destructive', label: '失败' },
  '': { dot: 'bg-muted-foreground/50', label: '' },
}
const who = (it: ActivityItem, compact?: boolean) =>
  [
    compact ? '' : it.source_name,
    it.identity_key && it.identity_key !== 'default' ? it.identity_key : '',
    it.device_name ? `来自 ${it.device_name}` : '',
  ]
    .filter(Boolean)
    .join(' · ')
</script>

<template>
  <ol class="grid">
    <li
      v-for="(it, i) in items"
      :key="`${it.kind}-${it.batch_id ?? it.push_id ?? i}-${it.at}`"
      class="relative grid grid-cols-[1.75rem_1fr] gap-x-3 pb-4 last:pb-0"
    >
      <span
        v-if="i < items.length - 1"
        class="bg-border absolute top-7 bottom-0 left-3.5 w-px"
        aria-hidden="true"
      />
      <span
        class="bg-card text-muted-foreground relative flex size-7 items-center justify-center rounded-full border"
      >
        <component :is="kindIcons[it.kind]" class="size-3.5" />
        <span
          class="ring-card absolute -right-0.5 -bottom-0.5 size-2 rounded-full ring-2"
          :class="outcome[it.outcome]?.dot"
        />
      </span>
      <div class="min-w-0">
        <p class="flex flex-wrap items-baseline gap-x-2 text-sm">
          <span class="font-medium">{{ it.title }}</span>
          <span v-if="outcome[it.outcome]?.label" class="sr-only">{{
            outcome[it.outcome]?.label
          }}</span>
          <span class="text-muted-foreground text-xs" :title="fmtTime(it.at)">{{
            fmtAgo(it.at)
          }}</span>
        </p>
        <p v-if="who(it, compact)" class="text-muted-foreground text-xs">{{ who(it, compact) }}</p>
        <p
          v-if="it.detail"
          class="text-sm break-words"
          :class="it.outcome === 'failure' ? 'text-destructive' : 'text-muted-foreground'"
        >
          {{ it.detail }}
        </p>
      </div>
    </li>
  </ol>
</template>
