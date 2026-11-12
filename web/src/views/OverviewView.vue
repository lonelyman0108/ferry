<script setup lang="ts">
import { ArrowRightIcon, SparklesIcon } from '@lucide/vue'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { type RouteLocationRaw, useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import FlowCanvas from '@/components/flow/FlowCanvas.vue'
import InboxPanel from '@/components/flow/InboxPanel.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useFlow } from '@/stores/flow'
import { useOverview } from '@/stores/overview'

// 总览：整页是一张流向图，左上角浮着收件箱。窄于 1280 时收件箱放在图的上面。
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
onUnmounted(() => clearInterval(timer))

// 新装好的实例按顺序引导，一次只提示第一件没做的事。
const nextStep = computed<{ text: string; action: string; to: RouteLocationRaw } | null>(() => {
  const c = overview.data?.counts
  if (!c) return null
  if (!c.sources)
    return {
      text: '先新建一个来源：定义读取哪个 App 的哪些请求、取哪些值。',
      action: '新建来源',
      to: '/sources/new',
    }
  if (!c.subscriptions)
    return {
      text: '再新建一个订阅，装到手机的代理客户端里。',
      action: '新建订阅',
      to: '/subscriptions',
    }
  if (!c.destinations)
    return {
      text: '还没有目的地：新建一个青龙或 Webhook，登录态才有地方推。',
      action: '新建目的地',
      to: '/destinations',
    }
  if (!c.bindings)
    return {
      text: '还没有推送规则：指定来源的输出变量推到哪个目的地。',
      action: '新建推送规则',
      to: { path: '/destinations', query: { tab: 'bindings' } },
    }
  if (!c.rules)
    return {
      text: '还没有通知规则：推送失败、登录态过期时不会提醒你。',
      action: c.channels ? '新建通知规则' : '新建通知渠道',
      to: { path: '/notify', query: c.channels ? { tab: 'rules' } : {} },
    }
  return null
})

// 点节点先去对应的页面；聚焦详情做好后改成就地聚焦。
function select(kind: 'device' | 'source' | 'destination', id: number) {
  if (kind === 'source') {
    const key = flow.data?.sources.find((s) => s.id === id)?.key
    void router.push({ path: '/data', query: key ? { source: key } : {} })
  } else if (kind === 'device') void router.push('/subscriptions')
  else void router.push('/destinations')
}

const legend = [
  { label: '正常', cls: 'border-(--edge)' },
  { label: '失败', cls: 'border-(--edge-failed) border-dashed' },
  { label: '等待重试', cls: 'border-warning border-dashed' },
  { label: '没连通或没推过', cls: 'border-(--edge-idle) border-dotted' },
]
</script>

<template>
  <div
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
</template>
