<script setup lang="ts">
import { ChevronLeftIcon, LayoutGridIcon } from '@lucide/vue'
import { useResizeObserver } from '@vueuse/core'
import { nextTick, onMounted, ref, watch } from 'vue'

import type { Flow } from '@/types'

import FlowNode from './FlowNode.vue'
import { type EdgeState, edgeClass, type FocusKind, sourceMeta } from './nodes'
import { useRelated } from './related'

// 聚焦时的窄栏：只留和当前对象直接相关的节点，上游在上、下游在下，连线走卡片右侧的通道，互不交叉。
// 其余节点收成一个胶囊，点了回全图。
const props = defineProps<{ flow: Flow; kind: FocusKind; id: number }>()
const emit = defineEmits<{ select: [kind: FocusKind, id: number]; back: [] }>()

const { current, groups, hidden } = useRelated(
  () => props.flow,
  () => props.kind,
  () => props.id,
)

const currentLabel = { device: '设备 · 订阅', source: '来源', destination: '目的地' }

// ---- 连线：都从卡片右缘出发，在右侧通道里竖着走，再拐回另一张卡的右缘 ----
const root = ref<HTMLElement | null>(null)
const paths = ref<{ key: string; d: string; state: EdgeState }[]>([])
const size = ref({ w: 0, h: 0 })

function measure() {
  const el = root.value
  if (!el || !current.value) return
  const base = el.getBoundingClientRect()
  const pos = (key: string) => {
    const n = el.querySelector<HTMLElement>(`[data-node="${key}"]`)
    if (!n) return null
    const r = n.getBoundingClientRect()
    return { x: r.right - base.left, y: r.top - base.top + r.height / 2 }
  }
  const cur = pos(current.value.key)
  if (!cur) return
  const out: { key: string; d: string; state: EdgeState }[] = []
  const all = [...groups.value.up, ...groups.value.down]
  all.forEach((n, i) => {
    const p = pos(n.key)
    if (!p) return
    const cx = Math.max(p.x, cur.x) + 12 + i * 5
    const r = 6
    const [a, b] = p.y < cur.y ? [p, cur] : [cur, p]
    out.push({
      key: n.key,
      state: n.edge ?? 'idle',
      d: `M${a.x},${a.y} H${cx - r} Q${cx},${a.y} ${cx},${a.y + r} V${b.y - r} Q${cx},${b.y} ${cx - r},${b.y} H${b.x}`,
    })
  })
  paths.value = out
  size.value = { w: base.width, h: base.height }
}

useResizeObserver(root, () => measure())
onMounted(() => {
  measure()
  void document.fonts?.ready.then(measure)
})
watch(
  () => [props.flow, props.kind, props.id],
  () => void nextTick(measure),
  { deep: true },
)
</script>

<template>
  <div ref="root" class="relative flex flex-col gap-5 pr-10">
    <svg
      class="pointer-events-none absolute inset-0 overflow-visible"
      :width="size.w"
      :height="size.h"
      aria-hidden="true"
    >
      <path
        v-for="p in paths"
        :key="p.key"
        :d="p.d"
        fill="none"
        stroke-width="1.5"
        :class="[edgeClass[p.state], p.state === 'ok' && 'stroke-(--edge-active)']"
      />
    </svg>

    <button
      type="button"
      class="bg-card hover:bg-accent inline-flex h-8 w-fit items-center gap-1.5 rounded-md border px-2.5 text-sm"
      @click="emit('back')"
    >
      <ChevronLeftIcon class="size-4" /> 返回全图
    </button>

    <section v-if="groups.up.length" class="grid gap-2">
      <p class="text-muted-foreground flex justify-between text-xs">
        {{ groups.upLabel }}<span>{{ groups.up.length }}</span>
      </p>
      <FlowNode
        v-for="n in groups.up"
        :key="n.key"
        :data-node="n.key"
        :mark="n.mark"
        :title="n.title"
        :subtitle="n.subtitle"
        :mono="n.mono"
        :badges="n.badges"
        :muted="n.muted"
        @select="emit('select', n.kind, n.id)"
      />
    </section>

    <section v-if="current" class="grid gap-2">
      <p class="text-muted-foreground text-xs">{{ currentLabel[kind] }}</p>
      <FlowNode
        :data-node="current.key"
        :mark="current.mark"
        :title="current.title"
        :subtitle="current.subtitle"
        :mono="current.mono"
        :badges="current.badges"
        :meta="kind === 'source' ? sourceMeta(flow.sources.find((s) => s.id === id)!) : undefined"
        :muted="current.muted"
        selected
      />
    </section>

    <section v-if="groups.down.length" class="grid gap-2">
      <p class="text-muted-foreground flex justify-between text-xs">
        {{ groups.downLabel }}<span>{{ groups.down.length }}</span>
      </p>
      <FlowNode
        v-for="n in groups.down"
        :key="n.key"
        :data-node="n.key"
        :mark="n.mark"
        :title="n.title"
        :subtitle="n.subtitle"
        :mono="n.mono"
        :badges="n.badges"
        :muted="n.muted"
        @select="emit('select', n.kind, n.id)"
      />
    </section>

    <button
      v-if="hidden > 0"
      type="button"
      class="text-muted-foreground hover:text-foreground hover:bg-accent/50 flex items-center gap-2 rounded-lg border border-dashed px-3 py-2.5 text-left text-sm"
      @click="emit('back')"
    >
      <LayoutGridIcon class="size-4 shrink-0" />
      已收起 {{ hidden }} 个节点，点击返回全图
    </button>
  </div>
</template>
