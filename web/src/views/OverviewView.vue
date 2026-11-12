<script setup lang="ts">
import {
  BellRingIcon,
  BoxesIcon,
  CalendarClockIcon,
  ChevronRightIcon,
  CircleCheckIcon,
  ClockAlertIcon,
  DatabaseIcon,
  HourglassIcon,
  ListTodoIcon,
  QrCodeIcon,
  RefreshCwIcon,
  SendIcon,
  ShieldAlertIcon,
  SmartphoneIcon,
  SparklesIcon,
  TriangleAlertIcon,
} from '@lucide/vue'
import { type Component, computed, onMounted, ref } from 'vue'
import { type RouteLocationRaw, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import IconTile from '@/components/IconTile.vue'
import PageHeader from '@/components/PageHeader.vue'
import SectionHeading from '@/components/SectionHeading.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtAgo, fmtTime } from '@/format'
import { type Tone, toneText } from '@/lib/tone'
import { useOverview } from '@/stores/overview'

const router = useRouter()
const overview = useOverview()
const data = computed(() => overview.data)
const loaded = ref(!!overview.data)

onMounted(async () => {
  try {
    await overview.load(false)
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loaded.value = true
  }
})

// 新装好的实例按顺序引导，一次只提示第一件没做的事。
const nextStep = computed<{
  key: string
  text: string
  action: string
  to: RouteLocationRaw
} | null>(() => {
  const c = data.value?.counts
  if (!c) return null
  if (!c.sources)
    return {
      key: 'source',
      text: '尚未配置来源：定义请求匹配规则与提取字段。',
      action: '新建来源',
      to: '/sources',
    }
  if (!c.subscriptions)
    return {
      key: 'subscription',
      text: '尚未创建订阅：生成客户端插件并安装到设备。',
      action: '新建订阅',
      to: '/subscriptions',
    }
  if (!c.subscriptions_connected)
    return {
      key: 'connect',
      text: '订阅还没连通：在手机上安装订阅，再打开对应的 App。',
      action: '查看订阅',
      to: '/subscriptions',
    }
  if (!c.destinations)
    return {
      key: 'destination',
      text: '尚未配置目的地：新建一个青龙或 Webhook 目的地。',
      action: '新建目的地',
      to: '/destinations',
    }
  if (!c.bindings)
    return {
      key: 'binding',
      text: '尚未配置推送规则：指定来源的输出变量推到哪个目的地。',
      action: '新建推送规则',
      to: { path: '/destinations', query: { tab: 'bindings' } },
    }
  if (!c.rules)
    return {
      key: 'rule',
      text: '还没有通知规则：推送失败、登录态过期时不会提醒你。',
      action: c.channels ? '新建通知规则' : '新建通知渠道',
      to: { path: '/notify', query: c.channels ? { tab: 'rules' } : {} },
    }
  return null
})

// 需要处理的事，每项点进去就是处理的页面，带好筛选条件。
const attention = computed(() => {
  const a = data.value?.attention
  if (!a) return []
  const items: {
    key: string
    tone: Tone
    icon: Component
    title: string
    hint: string
    to: RouteLocationRaw
  }[] = []
  if (a.pending_review)
    items.push({
      key: 'pending',
      tone: 'warning',
      icon: ShieldAlertIcon,
      title: `${a.pending_review} 个新值待审核`,
      hint: '审核通过后才会生效并推送',
      to: { path: '/data', query: { status: 'pending' } },
    })
  if (a.push_failed)
    items.push({
      key: 'push-failed',
      tone: 'danger',
      icon: TriangleAlertIcon,
      title: `${a.push_failed} 个推送失败`,
      hint: '重试用完仍没推出去，检查目的地后立即重试',
      to: { path: '/destinations', query: { tab: 'pushes', status: 'failed' } },
    })
  if (a.push_retrying)
    items.push({
      key: 'push-retrying',
      tone: 'warning',
      icon: RefreshCwIcon,
      title: `${a.push_retrying} 个推送在等待重试`,
      hint: '上次推送失败，稍后会自动重试',
      to: { path: '/destinations', query: { tab: 'pushes', status: 'retrying' } },
    })
  if (a.expired)
    items.push({
      key: 'expired',
      tone: 'danger',
      icon: ClockAlertIcon,
      title: `${a.expired} 个登录态已过期`,
      hint: '在手机上打开对应的 App，让它重新回传',
      to: { path: '/data', query: { status: 'expired' } },
    })
  if (a.expiring)
    items.push({
      key: 'expiring',
      tone: 'warning',
      icon: CalendarClockIcon,
      title: `${a.expiring} 个登录态 3 天内过期`,
      hint: '在手机上打开对应的 App 就会刷新',
      to: { path: '/data', query: { status: 'expiring' } },
    })
  if (a.outdated)
    items.push({
      key: 'outdated',
      tone: 'warning',
      icon: RefreshCwIcon,
      title: `${a.outdated} 个订阅待更新`,
      hint: 'MITM 域名有变化，在客户端里更新后才会读取新来源',
      to: '/subscriptions',
    })
  if (a.collecting)
    items.push({
      key: 'collecting',
      tone: 'info',
      icon: HourglassIcon,
      title: `${a.collecting} 批回传正在收集`,
      hint: '字段到齐或合并窗口结束后自动处理',
      to: '/data',
    })
  // 一个订阅都没连通时，“下一步”已经在提示，这里不重复。
  if (a.never_connected && nextStep.value?.key !== 'connect')
    items.push({
      key: 'connect',
      tone: 'neutral',
      icon: SmartphoneIcon,
      title: `${a.never_connected} 个订阅从未连通`,
      hint: '在手机上安装订阅后，打开对应的 App',
      to: '/subscriptions',
    })
  return items
})

const lastSeen = computed(
  () =>
    (data.value?.sources ?? [])
      .map((s) => s.last_seen_at)
      .filter((t): t is string => !!t)
      .sort()
      .pop() ?? null,
)

// 各类对象的数量，一行排开，点进去是对应的页面。
const counts = computed(() => {
  const c = data.value?.counts
  if (!c) return []
  return [
    {
      label: '来源',
      icon: BoxesIcon,
      tone: 'violet' as Tone,
      value: c.sources,
      sub: `${c.sources_enabled} 个启用`,
      to: '/sources',
    },
    {
      label: '订阅',
      icon: QrCodeIcon,
      tone: 'indigo' as Tone,
      value: c.subscriptions,
      sub: `${c.subscriptions_connected} 个已连通`,
      to: '/subscriptions',
    },
    {
      label: '身份',
      icon: DatabaseIcon,
      tone: 'sky' as Tone,
      value: c.identities,
      sub: lastSeen.value ? `最近回传 ${fmtAgo(lastSeen.value)}` : '暂无回传',
      to: '/data',
    },
    {
      label: '目的地',
      icon: SendIcon,
      tone: 'emerald' as Tone,
      value: c.destinations,
      sub: `${c.bindings} 条推送规则`,
      to: '/destinations',
    },
    {
      label: '通知渠道',
      icon: BellRingIcon,
      tone: 'amber' as Tone,
      value: c.channels,
      sub: `${c.rules} 条通知规则`,
      to: '/notify',
    },
  ]
})
</script>

<template>
  <PageHeader />

  <Card v-if="nextStep" class="border-primary/30 bg-primary/5 mb-6 ring-0">
    <CardContent class="flex flex-wrap items-center gap-3">
      <IconTile :icon="SparklesIcon" tone="primary" />
      <div class="mr-auto min-w-0">
        <div class="text-primary text-xs font-medium">下一步</div>
        <div class="text-sm">{{ nextStep.text }}</div>
      </div>
      <Button size="sm" @click="router.push(nextStep.to)"
        >{{ nextStep.action }} <ChevronRightIcon
      /></Button>
    </CardContent>
  </Card>

  <SectionHeading title="需要处理" :icon="ListTodoIcon" />
  <div v-if="!loaded && !data" class="mb-8 grid gap-3 md:grid-cols-2">
    <Skeleton class="h-16 rounded-xl" />
    <Skeleton class="h-16 rounded-xl" />
  </div>
  <div v-else-if="attention.length" class="mb-8 grid gap-3 md:grid-cols-2">
    <button
      v-for="it in attention"
      :key="it.key"
      type="button"
      class="group bg-card ring-foreground/10 hover:ring-foreground/20 flex items-center gap-3 rounded-xl p-3 text-left ring-1 transition hover:shadow-sm sm:p-4"
      @click="router.push(it.to)"
    >
      <IconTile :icon="it.icon" :tone="it.tone" />
      <div class="min-w-0 flex-1">
        <div class="text-sm font-medium">{{ it.title }}</div>
        <div class="text-muted-foreground text-xs">{{ it.hint }}</div>
      </div>
      <ChevronRightIcon
        class="text-muted-foreground size-4 shrink-0 transition group-hover:translate-x-0.5"
      />
    </button>
  </div>
  <div
    v-else-if="data"
    class="border-success/25 bg-success/5 mb-8 flex items-center gap-3 rounded-xl border p-3 sm:p-4"
  >
    <IconTile :icon="CircleCheckIcon" tone="success" />
    <div>
      <div class="text-sm font-medium">一切正常</div>
      <div class="text-muted-foreground text-xs">没有待审核、推送异常或快过期的登录态</div>
    </div>
  </div>

  <div
    v-if="counts.length"
    class="bg-border ring-foreground/10 mb-8 grid grid-cols-2 gap-px overflow-hidden rounded-xl ring-1 sm:grid-cols-5"
  >
    <button
      v-for="(c, i) in counts"
      :key="c.label"
      type="button"
      class="bg-card hover:bg-muted/40 grid gap-0.5 p-3 text-left transition sm:p-4"
      :class="i === counts.length - 1 ? 'max-sm:col-span-2' : ''"
      @click="router.push(c.to)"
    >
      <span class="text-muted-foreground flex items-center gap-1.5 text-xs"
        ><component :is="c.icon" class="size-3.5" :class="toneText[c.tone]" />{{ c.label }}</span
      >
      <span
        class="text-xl font-semibold tabular-nums"
        :class="c.value ? '' : 'text-muted-foreground/60'"
        >{{ c.value }}</span
      >
      <span class="text-muted-foreground truncate text-xs">{{ c.sub }}</span>
    </button>
  </div>

  <SectionHeading title="各来源" :icon="BoxesIcon" />
  <div class="bg-card overflow-hidden rounded-xl border">
    <Table>
      <TableHeader class="bg-muted/40">
        <TableRow>
          <TableHead>来源</TableHead>
          <TableHead class="hidden text-right sm:table-cell">身份</TableHead>
          <TableHead class="hidden sm:table-cell">最近回传</TableHead>
          <TableHead class="text-right">待审核</TableHead>
          <TableHead class="text-right" :title="`最近 ${data?.window_days ?? 7} 天`"
            >被驳回</TableHead
          >
          <TableHead class="text-right">推送异常</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="r in data?.sources ?? []"
          :key="r.id"
          class="cursor-pointer"
          @click="router.push({ path: '/data', query: { source: r.key } })"
        >
          <TableCell>
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-medium">{{ r.name }}</span>
              <span class="text-muted-foreground font-mono text-xs">{{ r.key }}</span>
              <StatusBadge v-if="!r.enabled" tone="neutral" dot>停用</StatusBadge>
              <StatusBadge v-if="r.expired" tone="danger" dot>已过期 {{ r.expired }}</StatusBadge>
              <StatusBadge v-else-if="r.expiring" tone="warning" dot
                >即将过期 {{ r.expiring }}</StatusBadge
              >
              <StatusBadge v-if="!r.bindings" tone="warning" dot>无推送规则</StatusBadge>
            </div>
          </TableCell>
          <TableCell class="hidden text-right tabular-nums sm:table-cell">{{
            r.identities
          }}</TableCell>
          <TableCell class="hidden sm:table-cell" :title="fmtTime(r.last_seen_at)">
            <span :class="r.last_seen_at ? '' : 'text-muted-foreground'">{{
              fmtAgo(r.last_seen_at)
            }}</span>
          </TableCell>
          <TableCell
            class="text-right font-medium tabular-nums"
            :class="r.pending_review ? toneText.warning : 'text-muted-foreground/60'"
            >{{ r.pending_review }}</TableCell
          >
          <TableCell
            class="text-right font-medium tabular-nums"
            :class="r.rejected ? toneText.warning : 'text-muted-foreground/60'"
            >{{ r.rejected }}</TableCell
          >
          <TableCell
            class="text-right font-medium tabular-nums"
            :class="r.push_failed + r.push_retrying ? toneText.danger : 'text-muted-foreground/60'"
            >{{ r.push_failed + r.push_retrying }}</TableCell
          >
        </TableRow>
        <TableRow v-if="data && !data.sources.length">
          <TableCell colspan="6" class="text-muted-foreground py-8 text-center">暂无来源</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
  <p class="text-muted-foreground mt-2 text-xs">
    被驳回统计最近 {{ data?.window_days ?? 7 }} 天；推送异常只看每条推送规则最近一次推送。
  </p>
</template>
