<script setup lang="ts">
import { ArrowRightIcon, SparklesIcon } from '@lucide/vue'
import { onKeyStroke } from '@vueuse/core'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { type RouteLocationRaw, useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import DestinationDetail from '@/components/detail/DestinationDetail.vue'
import DeviceDetail from '@/components/detail/DeviceDetail.vue'
import SourceDetail from '@/components/detail/SourceDetail.vue'
import FlowCanvas from '@/components/flow/FlowCanvas.vue'
import FocusRail from '@/components/flow/FocusRail.vue'
import InboxPanel from '@/components/flow/InboxPanel.vue'
import MiniChain from '@/components/flow/MiniChain.vue'
import { type FocusKind, nodeKey } from '@/components/flow/nodes'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { crumbTail } from '@/lib/crumb'
import { useFlow } from '@/stores/flow'
import { useOverview } from '@/stores/overview'

// 总览与聚焦共用这一页。总览：整页是一张流向图，左上角浮着收件箱，窄于 1280 时收件箱放在图的上面。
// 聚焦（/sources/:id、/subscriptions/:id、/destinations/:id）：图收成左侧窄栏，只留相关节点，详情占主体；
// 手机上窄栏换成详情顶部的横向链路。两种模式切换时节点卡从原位置滑到新位置。
const route = useRoute()
const router = useRouter()
const overview = useOverview()
const flow = useFlow()

const inboxOpen = ref(true)
watch(
  () => route.query.inbox,
  (v) => {
    if (v === 'open') inboxOpen.value = true
  },
  { immediate: true },
)

async function load(quiet = false) {
  try {
    await Promise.all([overview.load(false), flow.load()])
  } catch (e) {
    if (!quiet) toast.error(errorText(e))
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(() => void load(true), 60_000)
})
onUnmounted(() => {
  clearInterval(timer)
  crumbTail.value = null
})

// 新装好的实例按顺序引导，一次只提示第一件没做的事。
const nextStep = computed<{ text: string; action: string; to: RouteLocationRaw } | null>(() => {
  const c = overview.data?.counts
  if (!c) return null
  if (!c.sources)
    return {
      text: '新建来源，配置目标 App 的请求匹配与字段读取规则。',
      action: '新建来源',
      to: '/sources/new',
    }
  if (!c.subscriptions)
    return {
      text: '新建订阅，并安装到设备的代理客户端。',
      action: '新建订阅',
      to: '/subscriptions',
    }
  if (!c.destinations)
    return {
      text: '暂无目的地。新建青龙或 Webhook 目的地，用于接收推送的登录态。',
      action: '新建目的地',
      to: '/destinations',
    }
  if (!c.bindings)
    return {
      text: '暂无推送规则。推送规则指定来源的输出变量推送至哪个目的地。',
      action: '新建推送规则',
      to: { path: '/destinations', query: { tab: 'bindings' } },
    }
  if (!c.rules)
    return {
      text: '暂无通知规则。推送失败或登录态过期时不发送通知。',
      action: c.channels ? '新建通知规则' : '新建通知渠道',
      to: { path: '/notify', query: c.channels ? { tab: 'rules' } : {} },
    }
  return null
})

// ---- 聚焦 ----
const bases: Record<string, FocusKind> = {
  sources: 'source',
  subscriptions: 'device',
  destinations: 'destination',
}
const pathOf: Record<FocusKind, string> = {
  source: 'sources',
  device: 'subscriptions',
  destination: 'destinations',
}
const focus = computed<{ kind: FocusKind; id: number } | null>(() => {
  const m = /^\/(sources|subscriptions|destinations)\/(\d+)/.exec(route.path)
  return m ? { kind: bases[m[1]!]!, id: Number(m[2]) } : null
})

function select(kind: FocusKind, id: number) {
  void router.push(`/${pathOf[kind]}/${id}`)
}
function back() {
  void router.push('/')
}

watch(focus, (f) => {
  if (!f) crumbTail.value = null
})

// 没有弹窗开着时，Esc 回全图。
onKeyStroke('Escape', () => {
  if (focus.value && !document.querySelector('[role="dialog"], [role="alertdialog"]')) back()
})

// ---- 切换动画：记下切换前各节点卡的位置，切换后让同一个节点从旧位置滑过去，新出现的淡入 ----
const host = ref<HTMLElement | null>(null)
let before = new Map<string, DOMRect>()
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)')
const wide = window.matchMedia('(min-width: 768px)')
const focusKey = computed(() => (focus.value ? nodeKey(focus.value.kind, focus.value.id) : ''))

function snapshot() {
  const out = new Map<string, DOMRect>()
  host.value?.querySelectorAll<HTMLElement>('[data-node]').forEach((el) => {
    out.set(el.dataset.node!, el.getBoundingClientRect())
  })
  return out
}

function play() {
  if (reduced.matches || !wide.matches) return
  host.value?.querySelectorAll<HTMLElement>('[data-node]').forEach((el) => {
    const old = before.get(el.dataset.node!)
    if (!old) {
      el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 240, easing: 'ease-out' })
      return
    }
    const now = el.getBoundingClientRect()
    const dx = old.left - now.left
    const dy = old.top - now.top
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return
    el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], {
      duration: 360,
      easing: 'cubic-bezier(.3,.7,.2,1)',
    })
  })
}

watch(focusKey, () => (before = snapshot()), { flush: 'pre' })
watch(focusKey, () => void nextTick(() => requestAnimationFrame(play)), { flush: 'post' })

const legend = [
  { label: '正常', cls: 'border-(--edge)' },
  { label: '失败', cls: 'border-(--edge-failed) border-dashed' },
  { label: '等待重试', cls: 'border-warning border-dashed' },
  { label: '未连通或未推送', cls: 'border-(--edge-idle) border-dotted' },
]
</script>

<template>
  <div ref="host" class="flex flex-1 flex-col">
    <!-- 总览 -->
    <div
      v-if="!focus"
      class="relative flex-1 bg-[radial-gradient(var(--canvas-dot)_1px,transparent_1.2px)] [background-size:22px_22px] px-4 pt-4 pb-24 md:px-8 md:pb-20"
    >
      <h1 class="sr-only">总览</h1>

      <div
        v-if="overview.data"
        class="mb-6 max-w-xl xl:absolute xl:top-4 xl:left-4 xl:z-10 xl:mb-0 xl:max-h-[calc(100svh-8rem)] xl:w-[340px]"
        :class="!inboxOpen && 'xl:w-auto'"
      >
        <InboxPanel v-model:open="inboxOpen" :items="overview.data.items" @changed="load(true)" />
      </div>

      <div
        class="transition-[padding] duration-300 xl:pt-10"
        :class="inboxOpen ? 'xl:pl-[372px]' : 'xl:pt-20 xl:pl-8'"
      >
        <div
          v-if="nextStep"
          class="bg-card mx-auto mb-8 flex max-w-2xl flex-wrap items-center gap-3 rounded-xl border p-4"
        >
          <SparklesIcon class="text-primary size-5 shrink-0" />
          <p class="min-w-0 flex-1 text-sm">{{ nextStep.text }}</p>
          <Button size="sm" @click="router.push(nextStep.to)">
            {{ nextStep.action }}<ArrowRightIcon />
          </Button>
        </div>

        <FlowCanvas
          v-if="flow.data && flow.data.sources.length"
          :flow="flow.data"
          class="mx-auto max-w-5xl"
          @select="select"
        />
        <div v-else-if="!flow.data" class="mx-auto grid max-w-5xl gap-10 md:grid-cols-3">
          <Skeleton v-for="i in 3" :key="i" class="h-64 rounded-xl" />
        </div>
      </div>

      <footer
        v-if="flow.data?.sources.length"
        class="bg-card/90 text-muted-foreground absolute right-4 bottom-4 hidden flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border px-3 py-1.5 text-xs backdrop-blur md:flex"
      >
        <span v-for="l in legend" :key="l.label" class="flex items-center gap-1.5">
          <span class="w-5 border-t-2" :class="l.cls" />{{ l.label }}
        </span>
        <span v-if="overview.data" class="border-l pl-4">
          {{ overview.data.counts.identities }} 个身份 ·
          {{ overview.data.counts.bindings }} 条推送规则
        </span>
      </footer>
    </div>

    <!-- 聚焦 -->
    <div v-else class="flex flex-1">
      <aside
        aria-label="相关节点"
        class="hidden w-[304px] shrink-0 bg-[radial-gradient(var(--canvas-dot)_1px,transparent_1.2px)] [background-size:22px_22px] md:block"
      >
        <div class="sticky top-14 max-h-[calc(100svh-3.5rem)] overflow-y-auto p-4">
          <FocusRail
            v-if="flow.data"
            :id="focus.id"
            :flow="flow.data"
            :kind="focus.kind"
            @select="select"
            @back="back"
          />
        </div>
      </aside>
      <Transition
        appear
        enter-active-class="transition duration-300 ease-out motion-reduce:transition-none"
        enter-from-class="opacity-0 translate-x-4"
      >
        <div
          :key="focus.kind"
          class="bg-background min-w-0 flex-1 px-4 pt-4 pb-24 md:border-l md:px-8 md:pt-6 md:pb-12"
        >
          <MiniChain
            v-if="flow.data"
            :id="focus.id"
            class="mb-4 md:hidden"
            :flow="flow.data"
            :kind="focus.kind"
            @select="select"
          />
          <SourceDetail
            v-if="focus.kind === 'source'"
            :id="focus.id"
            @changed="load(true)"
            @focus="select"
            @title="(t) => (crumbTail = t)"
          />
          <DeviceDetail
            v-else-if="focus.kind === 'device'"
            :id="focus.id"
            @changed="load(true)"
            @focus="select"
            @title="(t) => (crumbTail = t)"
          />
          <DestinationDetail
            v-else
            :id="focus.id"
            @changed="load(true)"
            @focus="select"
            @title="(t) => (crumbTail = t)"
          />
        </div>
      </Transition>
    </div>
  </div>
</template>
