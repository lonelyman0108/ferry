<script setup lang="ts">
import { BoxesIcon, SendIcon, SmartphoneIcon, WebhookIcon } from '@lucide/vue'
import { useResizeObserver } from '@vueuse/core'
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import type { Flow } from '@/types'

import { clientName } from './inbox'
import FlowNode from './FlowNode.vue'
import {
  bindingState,
  coverState,
  deviceBadges,
  destBadges,
  destLine,
  type EdgeState,
  edgeClass,
  sourceBadges,
  sourceLine,
  sourceMeta,
} from './nodes'

// 三列流向图：设备（订阅）→ 来源 → 目的地。连线画在一层 SVG 上，端点位置按节点卡实际渲染的位置量出来。
// 线型表示通不通：实线正常，红虚线失败，琥珀虚线等待重试，灰虚线没连通或没推过。
// 窄屏上不画线，三列上下排，每张卡用一句话写去向（宽屏上这句话留给读屏）。
// 键盘聚焦到节点时和鼠标悬停一样高亮相关连线。
const props = defineProps<{ flow: Flow; selected?: string | null }>()
const emit = defineEmits<{ select: [kind: 'device' | 'source' | 'destination', id: number] }>()

interface Edge {
  id: string
  from: string
  to: string
  state: EdgeState
  label?: string
}

const sources = computed(() =>
  [...props.flow.sources].sort((a, b) => Number(b.enabled) - Number(a.enabled)),
)
const devices = computed(() => props.flow.devices)
const dests = computed(() =>
  [...props.flow.destinations].sort((a, b) => Number(b.enabled) - Number(a.enabled)),
)
const sourceOn = computed(() => new Map(props.flow.sources.map((s) => [s.id, s.enabled])))

const edges = computed<Edge[]>(() => {
  const out: Edge[] = []
  for (const d of devices.value)
    for (const sid of d.source_ids) {
      if (!sourceOn.value.has(sid)) continue
      out.push({
        id: `c${d.id}-${sid}`,
        from: `dev-${d.id}`,
        to: `src-${sid}`,
        state: coverState(props.flow, d, sid),
      })
    }
  for (const b of props.flow.bindings) {
    const state = bindingState(props.flow, b)
    out.push({
      id: `b${b.id}`,
      from: `src-${b.source_id}`,
      to: `dst-${b.destination_id}`,
      state,
      label: state === 'fail' ? '推送失败' : state === 'warn' ? '等待重试' : undefined,
    })
  }
  return out
})

// ---- 端口位置 ----
const root = ref<HTMLElement | null>(null)
const ports = ref(new Map<string, { l: [number, number]; r: [number, number] }>())
const size = ref({ w: 0, h: 0 })

function measure() {
  const el = root.value
  if (!el) return
  const base = el.getBoundingClientRect()
  const next = new Map<string, { l: [number, number]; r: [number, number] }>()
  el.querySelectorAll<HTMLElement>('[data-node]').forEach((n) => {
    const r = n.getBoundingClientRect()
    const y = r.top - base.top + r.height / 2
    next.set(n.dataset.node!, { l: [r.left - base.left, y], r: [r.right - base.left, y] })
  })
  ports.value = next
  size.value = { w: base.width, h: base.height }
}

useResizeObserver(root, () => measure())
onMounted(() => {
  measure()
  void document.fonts?.ready.then(measure)
})
watch(
  () => props.flow,
  () => void nextTick(measure),
  { deep: true },
)

const hovered = ref<string | null>(null)
// 键盘焦点和鼠标悬停分开记，离开时各清各的：焦点从一个节点移走时，鼠标仍停着的节点保持高亮。
// 两者同时存在时取最近发生的那个。
const focused = ref<string | null>(null)
const byFocus = ref(false)
const focus = computed(
  () =>
    (byFocus.value ? (focused.value ?? hovered.value) : (hovered.value ?? focused.value)) ??
    props.selected ??
    null,
)
function hoverOn(node: string) {
  hovered.value = node
  byFocus.value = false
}
function focusOn(node: string) {
  focused.value = node
  byFocus.value = true
}

const paths = computed(() =>
  edges.value.flatMap((e) => {
    const a = ports.value.get(e.from)
    const b = ports.value.get(e.to)
    if (!a || !b) return []
    const [x1, y1] = a.r
    const [x2, y2] = b.l
    const dx = Math.max(24, (x2 - x1) / 2)
    return [
      {
        ...e,
        d: `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`,
        mid: [(x1 + x2) / 2, (y1 + y2) / 2] as [number, number],
        // 两列靠得太近时不放线上的字，红虚线和目的地上的徽章已经说明了
        label: x2 - x1 >= 96 ? e.label : undefined,
        hl: focus.value === e.from || focus.value === e.to,
      },
    ]
  }),
)
</script>

<template>
  <div ref="root" class="relative">
    <svg
      class="pointer-events-none absolute inset-0 hidden overflow-visible md:block"
      :width="size.w"
      :height="size.h"
      aria-hidden="true"
    >
      <path
        v-for="p in paths"
        :key="p.id"
        :d="p.d"
        fill="none"
        class="transition-[stroke] duration-200"
        :class="[edgeClass[p.state], p.hl && p.state === 'ok' && 'stroke-(--edge-active)']"
        :stroke-width="p.hl ? 2 : 1.5"
      />
    </svg>
    <div class="pointer-events-none absolute inset-0 hidden md:block">
      <span
        v-for="p in paths.filter((x) => x.label)"
        :key="p.id"
        class="bg-background absolute -translate-x-1/2 -translate-y-1/2 rounded-md border px-2 text-xs leading-5 whitespace-nowrap"
        :class="
          p.state === 'fail'
            ? 'text-destructive border-destructive/40'
            : 'text-warning border-warning/40'
        "
        :style="{ left: `${p.mid[0]}px`, top: `${p.mid[1]}px` }"
        >{{ p.label }}</span
      >
    </div>

    <div
      class="relative grid gap-6 md:grid-cols-[minmax(150px,190px)_minmax(200px,250px)_minmax(150px,200px)] md:items-center md:justify-between md:gap-10"
    >
      <section class="grid min-w-0 grid-cols-1 content-center gap-3" aria-label="设备">
        <h2 class="text-muted-foreground flex justify-between px-0.5 text-xs">
          设备 · 订阅<span>{{ devices.length }} 台</span>
        </h2>
        <div
          v-for="d in devices"
          :key="d.id"
          @mouseenter="hoverOn(`dev-${d.id}`)"
          @mouseleave="hovered = null"
          @focusin="focusOn(`dev-${d.id}`)"
          @focusout="focused = null"
        >
          <FlowNode
            :icon="SmartphoneIcon"
            :title="d.name"
            :subtitle="clientName(d.client)"
            :badges="deviceBadges(d)"
            :line="`覆盖 ${d.source_ids.length} 个来源`"
            :muted="d.status === 'not_installed'"
            :data-node="`dev-${d.id}`"
            :selected="selected === `dev-${d.id}`"
            port-right
            @select="emit('select', 'device', d.id)"
          />
        </div>
      </section>

      <section class="grid min-w-0 grid-cols-1 content-center gap-3" aria-label="来源">
        <h2 class="text-muted-foreground flex justify-between px-0.5 text-xs">
          来源<span>已启用 {{ sources.filter((s) => s.enabled).length }} 个</span>
        </h2>
        <div
          v-for="s in sources"
          :key="s.id"
          @mouseenter="hoverOn(`src-${s.id}`)"
          @mouseleave="hovered = null"
          @focusin="focusOn(`src-${s.id}`)"
          @focusout="focused = null"
        >
          <FlowNode
            :icon="BoxesIcon"
            :title="s.name"
            :subtitle="s.key"
            mono
            :badges="sourceBadges(s)"
            :meta="sourceMeta(s)"
            :line="s.enabled ? sourceLine(flow, s) : undefined"
            :muted="!s.enabled"
            :data-node="`src-${s.id}`"
            :selected="selected === `src-${s.id}`"
            port-left
            port-right
            @select="emit('select', 'source', s.id)"
          />
        </div>
      </section>

      <section class="grid min-w-0 grid-cols-1 content-center gap-3" aria-label="目的地">
        <h2 class="text-muted-foreground flex justify-between px-0.5 text-xs">
          目的地<span>{{ dests.length }} 个</span>
        </h2>
        <div
          v-for="d in dests"
          :key="d.id"
          @mouseenter="hoverOn(`dst-${d.id}`)"
          @mouseleave="hovered = null"
          @focusin="focusOn(`dst-${d.id}`)"
          @focusout="focused = null"
        >
          <FlowNode
            :icon="d.kind === 'webhook' ? WebhookIcon : SendIcon"
            :title="d.name"
            :subtitle="d.kind === 'webhook' ? 'Webhook' : '青龙'"
            :badges="destBadges(flow, d)"
            :line="destLine(d)"
            :muted="!d.enabled"
            :data-node="`dst-${d.id}`"
            :selected="selected === `dst-${d.id}`"
            port-left
            @select="emit('select', 'destination', d.id)"
          />
          <p
            v-if="d.enabled && (d.state === 'failed' || d.state === 'retrying') && d.last_error"
            class="text-destructive mt-1 hidden truncate px-1 text-xs md:block"
            :title="d.last_error"
          >
            {{ d.last_error }}
          </p>
        </div>
      </section>
    </div>
  </div>
</template>
