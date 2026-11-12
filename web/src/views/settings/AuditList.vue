<script setup lang="ts">
import { CogIcon, KeyRoundIcon, SmartphoneIcon, UserRoundIcon, UserRoundXIcon } from '@lucide/vue'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import HintText from '@/components/HintText.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtTime } from '@/format'
import type { Tone } from '@/lib/tone'
import {
  auditCategories,
  type AuditLog,
  type Binding,
  bindingModes,
  type Channel,
  channelKinds,
  clients,
  type Destination,
  destinationKinds,
  eventTypes,
  type PushState,
  pushStates,
} from '@/types'

const route = useRoute()
const router = useRouter()
const list = ref<AuditLog[] | null>(null)
const more = ref(false)
const pageSize = 50

// 类别放在地址里，刷新页面时保留。
const category = computed({
  get: () => (typeof route.query.category === 'string' ? route.query.category : ''),
  set: (v) =>
    void router.replace({ query: { ...route.query, category: v || undefined } }).then(() => load()),
})

async function load(append = false) {
  try {
    const params: Record<string, string | number> = { limit: pageSize }
    if (category.value) params.category = category.value
    if (append && list.value?.length) params.before = list.value[list.value.length - 1].id
    const page = (await api.get<AuditLog[]>('/audit', { params })).data
    list.value = append && list.value ? [...list.value, ...page] : page
    more.value = page.length === pageSize
  } catch (e) {
    toast.error(errorText(e))
  }
}

onMounted(() => load())

// 颜色看结果；查看原值没有成败之分，单独标出来，方便查谁看过。
function tone(a: AuditLog): Tone {
  if (a.outcome === 'success') return 'success'
  if (a.outcome === 'failure') return 'danger'
  if (a.outcome === 'warning') return 'warning'
  return a.category === 'reveal' ? 'violet' : 'neutral'
}

const actorIcons = {
  user: UserRoundIcon,
  token: KeyRoundIcon,
  subscription: SmartphoneIcon,
  system: CogIcon,
  anonymous: UserRoundXIcon,
}
const actorIcon = (a: AuditLog) => (a.actor_kind ? actorIcons[a.actor_kind] : UserRoundIcon)

function eventNames(raw: string) {
  try {
    const evs = JSON.parse(raw) as string[]
    return evs.map((e) => eventTypes.find((t) => t.value === e)?.label ?? e).join('、')
  } catch {
    return raw
  }
}

// 详情换成界面上的叫法；各动作的详情格式由后端写审计的地方决定，认不出的原样显示。
function detail(a: AuditLog) {
  const d = a.detail
  if (!d) return ''
  switch (a.action) {
    case 'destination.test':
    case 'channel.test':
      return d === 'true' ? '成功' : '失败'
    case 'push.manual':
    case 'push.retry':
      // 当场跑完还在排队，说明这次失败了，已排上重试。
      return d === 'queued' ? pushStates.retrying.label : (pushStates[d as PushState]?.label ?? d)
    case 'binding.create':
    case 'binding.update':
      return bindingModes[d as Binding['mode']] ?? d
    case 'subscription.create':
    case 'subscription.update':
      return clients.find((c) => c.value === d)?.label ?? d
    case 'subscription.transport_mode':
      return d === 'sealed' ? '加密传输' : d === 'plain' ? '明文传输' : d
    case 'destination.create':
      return destinationKinds[d as Destination['kind']] ?? d
    case 'channel.create':
      return channelKinds[d as Channel['kind']] ?? d
    case 'notify_rule.create':
    case 'notify_rule.update':
      return eventNames(d)
    case 'sync.accepted':
    case 'sync.rejected':
      return d.split(',').join('、')
  }
  if (a.action.startsWith('notify.')) return d === 'ok' ? '已发送' : d
  return d
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <HintText class="mr-auto"
      >记录登录、配置变更、查看原值、回传、推送，以及每条通知的发送结果。</HintText
    >
    <NativeSelect v-model="category" aria-label="按类别筛选">
      <NativeSelectOption value="">全部类别</NativeSelectOption>
      <NativeSelectOption v-for="c in auditCategories" :key="c.value" :value="c.value">{{
        c.label
      }}</NativeSelectOption>
    </NativeSelect>
  </div>
  <div class="bg-card overflow-hidden rounded-xl border">
    <Table>
      <TableHeader class="bg-muted/40">
        <TableRow>
          <TableHead>时间</TableHead>
          <TableHead>操作</TableHead>
          <TableHead class="hidden md:table-cell">操作者</TableHead>
          <TableHead class="hidden md:table-cell">对象</TableHead>
          <TableHead class="hidden lg:table-cell">详情</TableHead>
          <TableHead class="hidden lg:table-cell">IP</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="a in list" :key="a.id">
          <!-- 日期和时间分开，窄屏时折成两行，给后面的操作列让地方 -->
          <TableCell class="text-muted-foreground align-top whitespace-normal tabular-nums">
            <span class="whitespace-nowrap">{{ fmtTime(a.at).slice(0, 10) }}</span>
            {{ ' ' }}
            <span class="whitespace-nowrap">{{ fmtTime(a.at).slice(11) }}</span>
          </TableCell>
          <TableCell class="whitespace-normal">
            <!-- 窄屏不显示后面几列：对象跟在徽章后面，操作者和详情放在下面 -->
            <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
              <StatusBadge :tone="tone(a)" :title="a.action">{{ a.action_label }}</StatusBadge>
              <span v-if="a.target_name" class="wrap-break-word md:hidden">{{
                a.target_name
              }}</span>
            </div>
            <div class="text-muted-foreground mt-1 grid gap-0.5 text-xs wrap-break-word lg:hidden">
              <span class="flex items-center gap-1 md:hidden"
                ><component :is="actorIcon(a)" class="size-3 shrink-0" />{{ a.actor_name }}</span
              >
              <span v-if="detail(a)">{{ detail(a) }}</span>
            </div>
          </TableCell>
          <TableCell class="hidden md:table-cell" :title="a.actor">
            <span class="flex items-center gap-1.5"
              ><component :is="actorIcon(a)" class="text-muted-foreground size-3.5 shrink-0" />{{
                a.actor_name
              }}</span
            >
          </TableCell>
          <TableCell
            class="hidden wrap-break-word whitespace-normal md:table-cell"
            :title="a.target"
            >{{ a.target_name }}</TableCell
          >
          <TableCell
            class="text-muted-foreground hidden max-w-72 wrap-break-word whitespace-normal lg:table-cell"
            >{{ detail(a) }}</TableCell
          >
          <TableCell class="text-muted-foreground hidden font-mono text-xs lg:table-cell">{{
            a.ip
          }}</TableCell>
        </TableRow>
        <TableRow v-if="list && !list.length">
          <TableCell colspan="6" class="text-muted-foreground py-8 text-center">{{
            category ? '这个类别下还没有记录' : '暂无审计记录'
          }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
  <div v-if="more">
    <Button variant="outline" @click="load(true)">加载更多</Button>
  </div>
</template>
