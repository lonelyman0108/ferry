<script setup lang="ts">
import { ChevronLeftIcon, ChevronRightIcon } from '@lucide/vue'
import { useScroll } from '@vueuse/core'
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import EntityIcon from '@/components/EntityIcon.vue'
import type { Flow } from '@/types'

import type { EdgeState, FocusKind } from './nodes'
import { useRelated } from './related'

// 窄屏上代替窄栏：左侧固定一个返回全图的按钮，右边是一条可以横滑的链路，上游 › 当前 › 下游，点了切换聚焦。
// 进入页面和切换对象时把当前节点滚到中间；还能往哪边滑，哪边的边缘就渐隐。
const props = defineProps<{ flow: Flow; kind: FocusKind; id: number }>()
const emit = defineEmits<{ select: [kind: FocusKind, id: number]; back: [] }>()

const { current, groups } = useRelated(
  () => props.flow,
  () => props.kind,
  () => props.id,
)

const track = ref<HTMLElement | null>(null)
const cur = ref<HTMLElement | null>(null)
const { arrivedState } = useScroll(track)

function center() {
  const el = track.value
  const c = cur.value
  if (el && c) el.scrollLeft = c.offsetLeft - (el.clientWidth - c.offsetWidth) / 2
}
onMounted(center)
watch(
  () => [props.kind, props.id],
  () => void nextTick(center),
)

const mask = computed(() => {
  const l = arrivedState.left ? 'black' : 'transparent'
  const r = arrivedState.right ? 'black' : 'transparent'
  const g = `linear-gradient(to right, ${l}, black 2rem, black calc(100% - 2rem), ${r})`
  return { maskImage: g, webkitMaskImage: g }
})

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
  <div class="flex items-center gap-2">
    <button
      type="button"
      class="bg-card hover:bg-accent flex min-h-11 shrink-0 items-center gap-1 rounded-lg border pr-3 pl-2 text-sm"
      @click="emit('back')"
    >
      <ChevronLeftIcon class="size-4" /> 返回全图
    </button>
    <nav
      ref="track"
      class="no-scrollbar relative -mr-4 flex min-w-0 flex-1 items-center gap-2 overflow-x-auto py-1 pr-4 pl-1 md:-mr-8 md:pr-8"
      :style="mask"
      aria-label="相关链路"
    >
      <template v-for="n in groups.up" :key="n.key">
        <button
          type="button"
          class="bg-card flex min-h-11 shrink-0 items-center gap-2 rounded-lg border px-3 text-sm"
          @click="emit('select', n.kind, n.id)"
        >
          <EntityIcon :mark="n.mark" size="xs" />
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
        ref="cur"
        aria-current="page"
        class="border-primary ring-primary/20 bg-card flex min-h-11 shrink-0 items-center gap-2 rounded-lg border px-3 text-sm font-medium ring-[3px]"
        ><EntityIcon :mark="current.mark" size="xs" />{{ current.title }}</span
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
        <EntityIcon :mark="n.mark" size="xs" />
        {{ n.title }}
        <span v-if="edgeText[n.edge ?? 'idle']" class="text-muted-foreground text-xs">{{
          edgeText[n.edge ?? 'idle']
        }}</span>
      </button>
    </nav>
  </div>
</template>
