<script setup lang="ts">
import {
  ChevronRightIcon,
  DatabaseIcon,
  HourglassIcon,
  LoaderCircleIcon,
  PlayIcon,
  SmartphoneIcon,
  UserRoundIcon,
} from '@lucide/vue'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import IconTile from '@/components/IconTile.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import IdentityView from './IdentityView.vue'
import PageHeader from '@/components/PageHeader.vue'
import SectionHeading from '@/components/SectionHeading.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { expiryState, fmtAgo, fmtTime, secondaryKey } from '@/format'
import { useOverview } from '@/stores/overview'
import { type Identity, type Batch, pushStates, type SourceSummary } from '@/types'

const props = defineProps<{ viewing?: string }>()
const route = useRoute()
const router = useRouter()
const list = ref<Identity[] | null>(null)
const batches = ref<Batch[]>([])
const sources = ref<SourceSummary[]>([])
const processing = ref<number | null>(null)
const now = ref(Date.now())
const overview = useOverview()
// 筛选条件放在地址里，总览上的“需要处理”可以直接带着条件跳过来。
const queryOf = (k: string) => (typeof route.query[k] === 'string' ? route.query[k] : '')
function setQuery(k: string, v: string) {
  return router.replace({ query: { ...route.query, [k]: v || undefined } })
}
const source = computed({
  get: () => queryOf('source'),
  set: (v) => void setQuery('source', v).then(load),
})
const status = computed({
  get: () => queryOf('status'),
  set: (v) => void setQuery('status', v),
})
const statusOptions = [
  { value: 'pending', label: '待审核' },
  { value: 'expiring', label: '即将过期' },
  { value: 'expired', label: '已过期' },
]
const shown = computed(() =>
  (list.value ?? []).filter((a) => {
    if (status.value === 'pending') return !!a.pending
    if (status.value === 'expiring' || status.value === 'expired')
      return expiryState(a.expires_at) === status.value
    return true
  }),
)

async function load() {
  try {
    list.value = (
      await api.get<Identity[]>('/identities', {
        params: source.value ? { source: source.value } : {},
      })
    ).data
  } catch (e) {
    toast.error(errorText(e))
  }
}

// 收集中的批次随时会到期被处理，定时刷新；批次变少说明有值生效了，顺带刷新身份列表。
async function loadBatches() {
  try {
    const all = (await api.get<Batch[]>('/batches')).data
    const shown = source.value ? all.filter((b) => b.source_key === source.value) : all
    if (shown.length < batches.value.length) await load()
    batches.value = shown
  } catch {
    // 定时刷新失败不打扰，下一轮再试
  }
}

async function processNow(b: Batch) {
  processing.value = b.id
  try {
    await api.post(`/batches/${b.id}/process`, {})
    toast.success('已处理')
    await Promise.all([loadBatches(), load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    processing.value = null
  }
}

const left = (b: Batch) =>
  Math.max(0, Math.ceil((new Date(b.closes_at).getTime() - now.value) / 1000))
const leftText = (b: Batch) => {
  const s = left(b)
  if (s === 0) return '处理中'
  return s < 60 ? `${s} 秒后处理` : `${Math.floor(s / 60)} 分 ${s % 60} 秒后处理`
}

// 详情里审核、恢复之后，列表和侧栏角标一起刷新。
function onChanged() {
  void load()
  void overview.load()
}

// 一天内回传过的身份显示绿点。
const recent = (t: string) => Date.now() - new Date(t).getTime() < 86400_000
const identity = (key: string) => (key === 'default' ? '默认' : key)

let poll: ReturnType<typeof setInterval> | undefined
let tick: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  try {
    sources.value = (await api.get<SourceSummary[]>('/sources')).data
  } catch (e) {
    toast.error(errorText(e))
  }
  await Promise.all([load(), loadBatches()])
  poll = setInterval(loadBatches, 3000)
  tick = setInterval(() => (now.value = Date.now()), 1000)
})
onUnmounted(() => {
  clearInterval(poll)
  clearInterval(tick)
})
</script>

<template>
  <PageHeader>
    <template #actions>
      <NativeSelect v-model="source" aria-label="按来源筛选">
        <NativeSelectOption value="">全部来源</NativeSelectOption>
        <NativeSelectOption v-for="s in sources" :key="s.key" :value="s.key">{{
          s.name
        }}</NativeSelectOption>
      </NativeSelect>
      <NativeSelect v-model="status" aria-label="按状态筛选">
        <NativeSelectOption value="">全部状态</NativeSelectOption>
        <NativeSelectOption v-for="o in statusOptions" :key="o.value" :value="o.value">{{
          o.label
        }}</NativeSelectOption>
      </NativeSelect>
    </template>
  </PageHeader>

  <template v-if="batches.length">
    <SectionHeading
      title="收集中"
      description="已回传、等待合并的值；字段齐全或窗口到期后处理"
      :icon="HourglassIcon"
    />
    <div class="mb-8 grid gap-3 lg:grid-cols-2">
      <Card v-for="b in batches" :key="b.id" size="sm" class="border-info/30 border">
        <CardHeader class="flex flex-wrap items-center gap-2">
          <IconTile :icon="HourglassIcon" tone="info" size="sm" />
          <CardTitle>{{ b.source_name }}</CardTitle>
          <span class="text-muted-foreground flex items-center gap-1 text-xs"
            ><SmartphoneIcon class="size-3.5" />{{ b.subscription_name }}</span
          >
          <StatusBadge tone="info" dot class="ml-auto">{{ leftText(b) }}</StatusBadge>
        </CardHeader>
        <CardContent class="grid gap-3 text-sm">
          <div v-for="v in b.values" :key="v.id" class="grid gap-1">
            <span class="text-muted-foreground text-xs"
              >{{ v.name || v.field }}
              <span v-if="secondaryKey(v.name, v.field)" class="font-mono">{{
                v.field
              }}</span></span
            >
            <code class="bg-muted/70 rounded-md border px-2 py-1.5 font-mono text-xs break-all">{{
              v.value
            }}</code>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <template v-if="b.missing.length">
              <span class="text-muted-foreground text-xs">待收到</span>
              <StatusBadge v-for="k in b.missing" :key="k" tone="neutral" class="font-mono">{{
                k
              }}</StatusBadge>
            </template>
            <Button
              size="sm"
              variant="outline"
              class="ml-auto"
              :disabled="processing === b.id"
              @click="processNow(b)"
            >
              <LoaderCircleIcon v-if="processing === b.id" class="animate-spin" /><PlayIcon
                v-else
              />
              立即处理
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  </template>

  <SectionHeading
    v-if="batches.length"
    title="身份"
    description="处理后的值按身份归档；单一身份显示为“默认”"
    :icon="UserRoundIcon"
  />

  <ListSkeleton v-if="!list" variant="table" :rows="4" />
  <EmptyState
    v-else-if="!list.length"
    :icon="DatabaseIcon"
    :title="source ? '这个来源还没有登录态' : '还没有登录态'"
    description="在手机上装好订阅、打开对应的 App，回传的值先进入“收集中”，处理后显示在这里。"
  >
    <Button as-child
      ><RouterLink to="/subscriptions"><SmartphoneIcon /> 去安装订阅</RouterLink></Button
    >
  </EmptyState>

  <div v-else-if="list" class="bg-card overflow-hidden rounded-xl border">
    <Table>
      <TableHeader class="bg-muted/40">
        <TableRow>
          <TableHead>来源</TableHead>
          <TableHead class="hidden md:table-cell">身份</TableHead>
          <TableHead>状态</TableHead>
          <TableHead class="hidden sm:table-cell">推送</TableHead>
          <TableHead class="hidden sm:table-cell">最近回传</TableHead>
          <TableHead class="hidden md:table-cell">首次回传</TableHead>
          <TableHead class="w-8" />
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="a in shown"
          :key="a.id"
          class="cursor-pointer"
          @click="router.push({ path: `/data/${a.id}`, query: route.query })"
        >
          <TableCell>
            <div class="flex items-center gap-2.5">
              <IconTile :icon="DatabaseIcon" tone="sky" size="sm" />
              <div class="min-w-0 whitespace-normal">
                <div class="font-medium">{{ a.source_name }}</div>
                <!-- 窄屏不显示身份列，身份放在来源名下面 -->
                <div class="text-muted-foreground font-mono text-xs break-all md:hidden">
                  {{ identity(a.key) }}
                </div>
              </div>
            </div>
          </TableCell>
          <TableCell class="hidden font-mono text-xs md:table-cell">{{
            identity(a.key)
          }}</TableCell>
          <TableCell>
            <div class="flex flex-wrap gap-1">
              <StatusBadge v-if="a.pending" tone="warning" dot
                >{{ a.pending }} 个待审核</StatusBadge
              >
              <StatusBadge v-if="expiryState(a.expires_at) === 'expired'" tone="danger" dot
                >已过期</StatusBadge
              >
              <StatusBadge
                v-else-if="expiryState(a.expires_at) === 'expiring'"
                tone="warning"
                dot
                :title="`到期 ${fmtTime(a.expires_at)}`"
                >即将过期</StatusBadge
              >
              <StatusBadge v-if="!a.pending && !expiryState(a.expires_at)" tone="success" dot
                >正常</StatusBadge
              >
            </div>
          </TableCell>
          <TableCell
            class="hidden sm:table-cell"
            :title="a.pushed_at ? `最近推送成功 ${fmtTime(a.pushed_at)}` : ''"
          >
            <StatusBadge v-if="a.push_status" :tone="pushStates[a.push_status].tone" dot>{{
              pushStates[a.push_status].label
            }}</StatusBadge>
            <span v-else class="text-muted-foreground text-xs">无推送规则</span>
          </TableCell>
          <TableCell class="hidden sm:table-cell" :title="fmtTime(a.last_seen_at)">
            <span class="flex items-center gap-1.5"
              ><span
                class="size-1.5 rounded-full"
                :class="recent(a.last_seen_at) ? 'bg-success' : 'bg-muted-foreground/40'"
              />{{ fmtAgo(a.last_seen_at) }}</span
            >
          </TableCell>
          <TableCell class="text-muted-foreground hidden md:table-cell">{{
            fmtTime(a.created_at)
          }}</TableCell>
          <TableCell><ChevronRightIcon class="text-muted-foreground size-4" /></TableCell>
        </TableRow>
        <TableRow v-if="!shown.length">
          <TableCell colspan="7" class="text-muted-foreground py-8 text-center"
            >没有符合条件的身份</TableCell
          >
        </TableRow>
      </TableBody>
    </Table>
  </div>

  <IdentityView
    v-if="props.viewing"
    :id="props.viewing"
    :key="props.viewing"
    @close="router.push({ path: '/data', query: route.query })"
    @changed="onChanged"
  />
</template>
