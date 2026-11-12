<script setup lang="ts">
import { CheckCheckIcon, DownloadIcon } from '@lucide/vue'

import StatusBadge from '@/components/StatusBadge.vue'
import { fmtAgo, fmtTime } from '@/format'
import { icons } from '@/lib/icons'
import type { Tone } from '@/lib/tone'
import type { ActivityItem } from '@/types'

// 记录时间线：回传、审核、推送、通知按时间倒序。图标角上的圆点表示结果；异常和失败另在标题后加徽章，
// 不只靠颜色区分，成功不加徽章（只给读屏一句“成功”），列表保持安静。
// 推送成功时详情是推了哪些变量（后端用逗号连接），拆开逐个显示，长变量名不会在中间断开。
// compact 用在详情里，不显示来源名（都是同一个来源）。
defineProps<{ items: ActivityItem[]; compact?: boolean }>()

const kindIcons = {
  sync: DownloadIcon,
  review: CheckCheckIcon,
  push: icons.push,
  notify: icons.notify,
}
const outcome: Record<string, { dot: string; label: string; badge?: Tone }> = {
  success: { dot: 'bg-success', label: '成功' },
  warning: { dot: 'bg-warning', label: '异常', badge: 'warning' },
  failure: { dot: 'bg-destructive', label: '失败', badge: 'danger' },
  '': { dot: 'bg-muted-foreground/50', label: '' },
}
const varsOf = (it: ActivityItem) =>
  it.kind === 'push' && it.outcome === 'success' && it.detail ? it.detail.split(',') : null
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
          <StatusBadge v-if="outcome[it.outcome]?.badge" :tone="outcome[it.outcome]!.badge">{{
            outcome[it.outcome]!.label
          }}</StatusBadge>
          <span v-else-if="outcome[it.outcome]?.label" class="sr-only">{{
            outcome[it.outcome]?.label
          }}</span>
          <span class="text-muted-foreground text-xs" :title="fmtTime(it.at)">{{
            fmtAgo(it.at)
          }}</span>
        </p>
        <p v-if="who(it, compact)" class="text-muted-foreground text-xs">{{ who(it, compact) }}</p>
        <div v-if="varsOf(it)" class="mt-1 flex flex-wrap gap-1 text-xs">
          <code
            v-for="v in varsOf(it)"
            :key="v"
            class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono [overflow-wrap:anywhere]"
            >{{ v }}</code
          >
        </div>
        <p
          v-else-if="it.detail"
          class="text-sm break-words"
          :class="it.outcome === 'failure' ? 'text-destructive' : 'text-muted-foreground'"
        >
          {{ it.detail }}
        </p>
      </div>
    </li>
  </ol>
</template>
