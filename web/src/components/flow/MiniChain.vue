<script setup lang="ts">
import { ChevronRightIcon } from '@lucide/vue'

import type { Flow } from '@/types'

import type { EdgeState, FocusKind } from './nodes'
import { useRelated } from './related'

// 手机上代替窄栏：详情顶部一条可以横滑的链路，上游 › 当前 › 下游，点了切换聚焦。
const props = defineProps<{ flow: Flow; kind: FocusKind; id: number }>()
const emit = defineEmits<{ select: [kind: FocusKind, id: number] }>()

const { current, groups } = useRelated(
  () => props.flow,
  () => props.kind,
  () => props.id,
)

const edgeText: Record<EdgeState, string> = {
  ok: '',
  fail: '失败',
  warn: '等待重试',
  idle: '未连通',
}
const edgeTone: Record<EdgeState, string> = {
  ok: 'border-(--edge)',
  fail: 'border-(--edge-failed) border-dashed',
  warn: 'border-warning border-dashed',
  idle: 'border-(--edge-idle) border-dotted',
}
</script>

<template>
  <nav
    class="no-scrollbar -mx-4 flex items-center gap-2 overflow-x-auto px-4 pb-1"
    aria-label="相关链路"
  >
    <template v-for="n in groups.up" :key="n.key">
      <button
        type="button"
        class="bg-card flex min-h-11 shrink-0 items-center gap-2 rounded-lg border px-3 text-sm"
        @click="emit('select', n.kind, n.id)"
      >
        {{ n.title }}
        <span class="w-4 border-t-2" :class="edgeTone[n.edge ?? 'idle']" aria-hidden="true" />
        <span v-if="edgeText[n.edge ?? 'idle']" class="text-muted-foreground text-xs">{{
          edgeText[n.edge ?? 'idle']
        }}</span>
      </button>
    </template>
    <ChevronRightIcon v-if="groups.up.length" class="text-muted-foreground size-4 shrink-0" />
    <span
      v-if="current"
      class="border-primary ring-primary/20 bg-card flex min-h-11 shrink-0 items-center rounded-lg border px-3 text-sm font-medium ring-[3px]"
      >{{ current.title }}</span
    >
    <ChevronRightIcon v-if="groups.down.length" class="text-muted-foreground size-4 shrink-0" />
    <button
      v-for="n in groups.down"
      :key="n.key"
      type="button"
      class="bg-card flex min-h-11 shrink-0 items-center gap-2 rounded-lg border px-3 text-sm"
      @click="emit('select', n.kind, n.id)"
    >
      <span class="w-4 border-t-2" :class="edgeTone[n.edge ?? 'idle']" aria-hidden="true" />
      {{ n.title }}
      <span v-if="edgeText[n.edge ?? 'idle']" class="text-muted-foreground text-xs">{{
        edgeText[n.edge ?? 'idle']
      }}</span>
    </button>
  </nav>
</template>
