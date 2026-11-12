<script setup lang="ts">
import {
  ArrowRightIcon,
  MergeIcon,
  HistoryIcon,
  PlugZapIcon,
  PlusIcon,
  RefreshCwIcon,
  SendIcon,
  ServerIcon,
  SplitIcon,
  WebhookIcon,
} from '@lucide/vue'
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import ChoiceCards from '@/components/ChoiceCards.vue'
import EmptyState from '@/components/EmptyState.vue'
import FormField from '@/components/FormField.vue'
import FormSection from '@/components/FormSection.vue'
import FormDialog from '@/components/FormDialog.vue'
import HintText from '@/components/HintText.vue'
import BrandIcon from '@/components/BrandIcon.vue'
import IconTile from '@/components/IconTile.vue'
import ListRow from '@/components/ListRow.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import KeyValueEditor from '@/components/KeyValueEditor.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { fmtAgo, fmtTime } from '@/format'
import { urlProblem, useFormCheck } from '@/lib/form'
import { toneText } from '@/lib/tone'
import { useOverview } from '@/stores/overview'
import {
  type Identity,
  type Binding,
  bindingModes,
  type DestConfig,
  type Destination,
  destinationKinds,
  type PushJob,
  pushStateOf,
  pushStates,
  pushTrigger,
  type SourceSummary,
  type SourceView,
} from '@/types'

const route = useRoute()
const router = useRouter()
const overview = useOverview()
// 可以用 ?tab= 直接打开某个标签页（总览上的推送异常会跳到推送记录）。
const tabs = ['destinations', 'bindings', 'pushes']
const tab = ref(
  typeof route.query.tab === 'string' && tabs.includes(route.query.tab)
    ? route.query.tab
    : route.query.source
      ? 'bindings'
      : 'destinations',
)
// 来源筛选按来源标识放在地址里，推送规则和推送记录共用；从来源、身份详情跳过来时直接带上。
const sourceFilter = computed({
  get: () => (typeof route.query.source === 'string' ? route.query.source : ''),
  set: (v) => void router.replace({ query: { ...route.query, source: v || undefined } }),
})
const filteredSourceID = computed(() => sources.value.find((s) => s.key === sourceFilter.value)?.id)

const loaded = ref(false)
const dests = ref<Destination[]>([])
const bindings = ref<Binding[]>([])
const pushes = ref<PushJob[] | null>(null)
const sources = ref<SourceSummary[]>([])
const identities = ref<Identity[]>([])
const busy = ref(false)

// 模板语法和 Vue 的插值撞了，示例放在这里再输出。
const bodyExample = '{{index .VarMap "JD_COOKIE"}}'
const kindOptions = [
  {
    value: 'qinglong' as const,
    label: '青龙',
    description: '通过 OpenAPI 写入环境变量',
    brand: 'qinglong',
  },
  {
    value: 'webhook' as const,
    label: 'Webhook',
    description: 'HTTP 请求，可选 HMAC 签名',
    icon: WebhookIcon,
    tone: 'sky' as const,
  },
]
const modeOptions = [
  {
    value: 'per_identity' as const,
    label: '每个身份一条',
    description: '按身份分别写入，备注 ferry:<来源>:<身份>',
    icon: SplitIcon,
    tone: 'sky' as const,
  },
  {
    value: 'joined' as const,
    label: '合并为一条',
    description: '多个身份以 & 连接，备注 ferry:<来源>',
    icon: MergeIcon,
    tone: 'indigo' as const,
  },
]
const sourceName = (id: number) => sources.value.find((s) => s.id === id)?.name ?? `#${id}`
const destName = (id: number) => dests.value.find((d) => d.id === id)?.name ?? `#${id}`
const identityKey = (id: number | null) =>
  id ? (identities.value.find((a) => a.id === id)?.key ?? `#${id}`) : '全部身份'
const bindingLabel = (id: number) => {
  const b = bindings.value.find((x) => x.id === id)
  return b ? `${sourceName(b.source_id)} → ${destName(b.destination_id)}` : `推送规则 #${id}`
}
const shownBindings = computed(() =>
  sourceFilter.value
    ? bindings.value.filter((b) => b.source_id === filteredSourceID.value)
    : bindings.value,
)

async function load() {
  try {
    const [d, b, s, a] = await Promise.all([
      api.get<Destination[]>('/destinations'),
      api.get<Binding[]>('/bindings'),
      api.get<SourceSummary[]>('/sources'),
      api.get<Identity[]>('/identities'),
      loadPushes(),
    ])
    dests.value = d.data
    bindings.value = b.data
    sources.value = s.data
    identities.value = a.data
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loaded.value = true
  }
}
onMounted(load)

// 推送记录的筛选：状态可以从地址带过来（总览上的推送失败、等待重试会跳过来），目的地在页面上选，来源和推送规则共用。
const pushFilter = reactive({
  status: typeof route.query.status === 'string' ? route.query.status : '',
  destination: 0,
})
async function loadPushes() {
  try {
    const params: Record<string, string | number> = {}
    if (pushFilter.status) params.status = pushFilter.status
    if (pushFilter.destination) params.destination = pushFilter.destination
    if (sourceFilter.value) params.source = sourceFilter.value
    pushes.value = (await api.get<PushJob[]>('/pushes', { params })).data
  } catch (e) {
    toast.error(errorText(e))
  }
}
watch([pushFilter, sourceFilter], loadPushes)

// 立即重试：等待重试的马上执行，失败或跳过的重新排一次；结果直接提示。
function toastPush(job: PushJob) {
  const state = pushStateOf(job)
  if (state === 'success') toast.success('推送成功')
  else if (state === 'failed' || state === 'retrying')
    toast.error(`推送失败：${job.last_error ?? ''}`)
  else toast.info(`推送状态：${pushStates[state].label}`)
}
const retrying = ref(0)
const canRetry = (p: PushJob) => ['failed', 'retrying', 'skipped'].includes(pushStateOf(p))
async function retry(p: PushJob) {
  retrying.value = p.id
  try {
    toastPush((await api.post<PushJob>(`/pushes/${p.id}/retry`, {})).data)
    await Promise.all([load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    retrying.value = 0
  }
}

// ---- 目的地 ----
const destForm = reactive({
  open: false,
  id: 0,
  name: '',
  kind: 'qinglong' as Destination['kind'],
  enabled: true,
  config: {} as DestConfig,
  secrets: {} as Record<string, boolean>,
})

const destCheck = useFormCheck(() => {
  const c = destForm.config
  const q = destForm.kind === 'qinglong'
  return {
    name: !destForm.name.trim() && '请填写名称',
    base_url: q && urlProblem(c.base_url),
    client_id: q && !c.client_id?.trim() && '请填写 Client ID',
    // 编辑时密钥不回显，留空沿用旧值。
    client_secret:
      q && !c.client_secret && !destForm.secrets.client_secret && '请填写 Client Secret',
    url: !q && urlProblem(c.url, 'URL'),
  }
})

function newDest() {
  destCheck.reset()
  Object.assign(destForm, {
    open: true,
    id: 0,
    name: '',
    kind: 'qinglong',
    enabled: true,
    config: { method: 'POST' },
    secrets: {},
  })
}

// 推送规则要先有目的地：切到目的地标签页，打开新建表单。
function startDest() {
  tab.value = 'destinations'
  newDest()
}

function editDest(d: Destination) {
  destCheck.reset()
  Object.assign(destForm, {
    open: true,
    id: d.id,
    name: d.name,
    kind: d.kind,
    enabled: d.enabled,
    config: { ...d.config },
    secrets: d.secrets_set,
  })
}

async function saveDest() {
  if (!destCheck.ok()) return
  busy.value = true
  try {
    const c = destForm.config
    const config: DestConfig =
      destForm.kind === 'qinglong'
        ? {
            base_url: c.base_url,
            client_id: c.client_id,
            client_secret: c.client_secret || undefined,
          }
        : {
            url: c.url,
            method: c.method,
            headers: c.headers,
            body: c.body || undefined,
            secret: c.secret || undefined,
          }
    const body = { name: destForm.name, kind: destForm.kind, enabled: destForm.enabled, config }
    if (destForm.id) await api.put(`/destinations/${destForm.id}`, body)
    else await api.post('/destinations', body)
    toast.success('已保存')
    destForm.open = false
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

const testing = ref(0)
async function testDest(d: Destination) {
  testing.value = d.id
  try {
    const r = (await api.post<{ ok: boolean; error?: string }>(`/destinations/${d.id}/test`, {}))
      .data
    if (r.ok) toast.success(`「${d.name}」连接正常`)
    else toast.error(`「${d.name}」连接失败：${r.error}`)
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    testing.value = 0
  }
}

// 列表里的启用开关：切换即保存。密钥不回显，留空由后端沿用旧值。
const toggling = ref('')
async function toggleDest(d: Destination, enabled: boolean) {
  toggling.value = `d${d.id}`
  try {
    await api.put(`/destinations/${d.id}`, {
      name: d.name,
      kind: d.kind,
      enabled,
      config: d.config,
    })
    d.enabled = enabled
    void overview.load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = ''
  }
}

async function toggleBinding(b: Binding, enabled: boolean) {
  toggling.value = `b${b.id}`
  try {
    const { source_id, destination_id, mode, rename } = b
    await api.put(`/bindings/${b.id}`, { source_id, destination_id, mode, rename, enabled })
    b.enabled = enabled
    void overview.load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = ''
  }
}

async function removeDest(d: Destination) {
  try {
    await api.delete(`/destinations/${d.id}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(errorText(e))
  }
}

// ---- 推送规则 ----
const bindForm = reactive({
  open: false,
  id: 0,
  source_id: 0,
  destination_id: 0,
  mode: 'per_identity' as Binding['mode'],
  rename: {} as Record<string, string>,
  enabled: true,
})
const outputs = ref<string[]>([])
const noSources = '暂无来源，请在“来源”页面新建'
const noDests = '暂无目的地，请在“目的地”标签页新建'
const bindCheck = useFormCheck(() => ({
  source_id: !bindForm.source_id && (sources.value.length ? '请选择来源' : noSources),
  destination_id: !bindForm.destination_id && (dests.value.length ? '请选择目的地' : noDests),
}))

async function loadOutputs(sourceID: number) {
  outputs.value = []
  if (!sourceID) return
  try {
    const s = (await api.get<SourceView>(`/sources/${sourceID}`)).data
    outputs.value = (s.outputs ?? []).map((o) => o.name)
  } catch (e) {
    toast.error(errorText(e))
  }
}

async function newBinding() {
  bindCheck.reset()
  Object.assign(bindForm, {
    open: true,
    id: 0,
    source_id: filteredSourceID.value || sources.value[0]?.id || 0,
    destination_id: dests.value[0]?.id ?? 0,
    mode: 'per_identity',
    rename: {},
    enabled: true,
  })
  await loadOutputs(bindForm.source_id)
}

async function editBinding(b: Binding) {
  bindCheck.reset()
  Object.assign(bindForm, { open: true, ...b, rename: { ...b.rename } })
  await loadOutputs(b.source_id)
}

// 换来源后输出变量不同，改名表跟着清空。change 事件比 v-model 先到，等一拍再读新值。
async function pickSource() {
  await nextTick()
  bindForm.rename = {}
  await loadOutputs(bindForm.source_id)
}

async function saveBinding() {
  if (!bindCheck.ok()) return
  busy.value = true
  try {
    const rename: Record<string, string> = {}
    for (const [k, v] of Object.entries(bindForm.rename)) if (v.trim()) rename[k] = v.trim()
    const body = {
      source_id: bindForm.source_id,
      destination_id: bindForm.destination_id,
      mode: bindForm.mode,
      rename,
      enabled: bindForm.enabled,
    }
    if (bindForm.id) await api.put(`/bindings/${bindForm.id}`, body)
    else await api.post('/bindings', body)
    toast.success('已保存')
    bindForm.open = false
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function removeBinding(b: Binding) {
  try {
    await api.delete(`/bindings/${b.id}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(errorText(e))
  }
}

// ---- 手动推送 ----
const pushForm = reactive({ open: false, binding: null as Binding | null, identity_id: 0 })
const pushIdentities = computed(() =>
  identities.value.filter((a) => a.source_id === pushForm.binding?.source_id),
)

const pushCheck = useFormCheck(() => ({
  identity_id:
    pushForm.binding?.mode === 'per_identity' &&
    !pushForm.identity_id &&
    (pushIdentities.value.length ? '请选择身份' : '该来源暂无登录态，回传后方可推送'),
}))

function openPush(b: Binding) {
  pushCheck.reset()
  pushForm.binding = b
  pushForm.identity_id = identities.value.find((a) => a.source_id === b.source_id)?.id ?? 0
  pushForm.open = true
}

async function runPush() {
  const b = pushForm.binding
  if (!b || !pushCheck.ok()) return
  busy.value = true
  try {
    const job = (
      await api.post<PushJob>('/pushes', {
        binding_id: b.id,
        identity_id: b.mode === 'per_identity' ? pushForm.identity_id : null,
      })
    ).data
    toastPush(job)
    pushForm.open = false
    await Promise.all([loadPushes(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <PageHeader />

  <Tabs v-model="tab">
    <!-- 手机上标签栏和右侧按钮保持同一行：标签去掉图标，仍放不下时横向滚动 -->
    <div class="mb-2 flex flex-wrap items-center gap-2 max-sm:flex-nowrap">
      <TabsList
        class="no-scrollbar mr-auto max-sm:min-w-0 max-sm:overflow-x-auto max-sm:[&_svg]:hidden"
      >
        <TabsTrigger value="destinations"
          ><ServerIcon /> 目的地
          <span
            class="bg-muted-foreground/15 text-muted-foreground min-w-5 rounded-full px-1.5 text-xs tabular-nums"
            >{{ dests.length }}</span
          ></TabsTrigger
        >
        <TabsTrigger value="bindings"
          ><SplitIcon /> 推送规则
          <span
            class="bg-muted-foreground/15 text-muted-foreground min-w-5 rounded-full px-1.5 text-xs tabular-nums"
            >{{ bindings.length }}</span
          ></TabsTrigger
        >
        <TabsTrigger value="pushes" @click="loadPushes"><HistoryIcon /> 推送记录</TabsTrigger>
      </TabsList>
      <Button v-if="tab === 'destinations'" class="max-sm:min-w-11 max-sm:px-0" @click="newDest"
        ><PlusIcon /><span class="max-sm:sr-only">新建目的地</span></Button
      >
      <Button v-else-if="tab === 'bindings'" class="max-sm:min-w-11 max-sm:px-0" @click="newBinding"
        ><PlusIcon /><span class="max-sm:sr-only">新建推送规则</span></Button
      >
      <Button v-else variant="outline" class="max-sm:min-w-11 max-sm:px-0" @click="loadPushes"
        ><RefreshCwIcon /><span class="max-sm:sr-only">刷新</span></Button
      >
    </div>

    <!-- 目的地 -->
    <TabsContent value="destinations" class="grid gap-3">
      <HintText>密钥仅可写入，保存后不回显。</HintText>
      <ListSkeleton v-if="!loaded" :rows="2" />
      <EmptyState
        v-else-if="!dests.length"
        :icon="SendIcon"
        title="暂无目的地"
        description="目的地是登录态的最终写入位置，例如青龙环境变量或自定义 Webhook。"
      >
        <Button @click="newDest"><PlusIcon /> 新建目的地</Button>
      </EmptyState>
      <ListRow v-for="d in dests" :key="d.id" :dim="!d.enabled">
        <template #icon>
          <BrandIcon v-if="d.kind === 'qinglong'" brand="qinglong" />
          <IconTile v-else :icon="WebhookIcon" tone="sky" />
        </template>
        <template #title
          ><router-link :to="`/destinations/${d.id}`" class="hover:underline" title="查看链路">{{
            d.name
          }}</router-link></template
        >
        <template #badges>
          <StatusBadge :tone="d.kind === 'qinglong' ? 'emerald' : 'sky'">{{
            destinationKinds[d.kind]
          }}</StatusBadge>
          <StatusBadge tone="neutral"
            >{{ bindings.filter((b) => b.destination_id === d.id).length }} 条推送规则</StatusBadge
          >
          <StatusBadge v-if="d.problems" tone="danger" dot>{{ d.problems }} 个推送异常</StatusBadge>
        </template>
        <template #description>
          <span class="font-mono break-all">{{
            d.kind === 'qinglong'
              ? d.config.base_url
              : `${d.config.method ?? 'POST'} ${d.config.url}`
          }}</span>
          <span
            v-if="d.last_push"
            class="flex items-center gap-1"
            :title="fmtTime(d.last_push.finished_at ?? d.last_push.created_at)"
            ><HistoryIcon class="size-3.5" />最近推送
            {{ fmtAgo(d.last_push.finished_at ?? d.last_push.created_at) }} ·
            <span :class="toneText[pushStates[pushStateOf(d.last_push)].tone]">{{
              pushStates[pushStateOf(d.last_push)].label
            }}</span></span
          >
        </template>
        <template #actions>
          <RowActions
            :name="d.name"
            :enabled="d.enabled"
            :toggling="toggling === `d${d.id}`"
            delete-title="删除目的地？"
            delete-description="存在引用该目的地的推送规则时无法删除；已写入目的地的变量将保留。"
            @toggle="(v) => toggleDest(d, v)"
            @edit="editDest(d)"
            @remove="removeDest(d)"
          >
            <Button variant="outline" size="sm" :disabled="testing === d.id" @click="testDest(d)">
              <PlugZapIcon /> 测试连接
            </Button>
          </RowActions>
        </template>
      </ListRow>
    </TabsContent>

    <!-- 推送规则 -->
    <TabsContent value="bindings" class="grid gap-3">
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <HintText class="mr-auto">
          仅管理备注以 <code class="bg-muted rounded px-1 font-mono">ferry:</code>
          开头的青龙变量，其余变量不受影响。
        </HintText>
        <NativeSelect v-model="sourceFilter" aria-label="按来源筛选">
          <NativeSelectOption value="">全部来源</NativeSelectOption>
          <NativeSelectOption v-for="s in sources" :key="s.key" :value="s.key">{{
            s.name
          }}</NativeSelectOption>
        </NativeSelect>
      </div>
      <ListSkeleton v-if="!loaded" :rows="2" />
      <EmptyState
        v-else-if="!bindings.length"
        :icon="SplitIcon"
        title="暂无推送规则"
        description="推送规则定义各来源的登录态推送至哪个目的地。"
      >
        <Button v-if="!dests.length" @click="startDest"><PlusIcon /> 新建目的地</Button>
        <Button v-else-if="!sources.length" as-child
          ><RouterLink to="/sources"><PlusIcon /> 新建来源</RouterLink></Button
        >
        <Button v-else @click="newBinding"><PlusIcon /> 新建推送规则</Button>
      </EmptyState>
      <EmptyState v-else-if="!shownBindings.length" :icon="SplitIcon" title="该来源暂无推送规则">
        <Button @click="newBinding"><PlusIcon /> 新建推送规则</Button>
      </EmptyState>
      <template v-else>
        <ListRow v-for="b in shownBindings" :key="b.id" :dim="!b.enabled">
          <template #icon>
            <IconTile
              :icon="b.mode === 'joined' ? MergeIcon : SplitIcon"
              :tone="b.mode === 'joined' ? 'indigo' : 'sky'"
            />
          </template>
          <template #title>
            <span class="flex flex-wrap items-center gap-1.5"
              >{{ sourceName(b.source_id) }}
              <ArrowRightIcon class="text-muted-foreground size-3.5" />
              {{ destName(b.destination_id) }}</span
            >
          </template>
          <template #badges>
            <StatusBadge :tone="b.mode === 'joined' ? 'indigo' : 'sky'">{{
              bindingModes[b.mode]
            }}</StatusBadge>
          </template>
          <template #description>
            <template v-if="Object.keys(b.rename).length">
              <span v-for="(to, from) in b.rename" :key="from" class="font-mono"
                ><span class="opacity-70">{{ from }}</span> → {{ to }}</span
              >
            </template>
            <span v-else>沿用原变量名</span>
          </template>
          <template #actions>
            <RowActions
              :name="bindingLabel(b.id)"
              :enabled="b.enabled"
              :toggling="toggling === `b${b.id}`"
              delete-title="删除推送规则？"
              delete-description="删除后停止推送，已写入目的地的变量将保留。"
              @toggle="(v) => toggleBinding(b, v)"
              @edit="editBinding(b)"
              @remove="removeBinding(b)"
            >
              <Button variant="outline" size="sm" :disabled="!b.enabled" @click="openPush(b)"
                ><SendIcon /> 手动推送</Button
              >
            </RowActions>
          </template>
        </ListRow>
      </template>
    </TabsContent>

    <!-- 推送记录 -->
    <TabsContent value="pushes" class="grid gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <HintText class="mr-auto"
          >显示最近 100 条记录。失败的任务将自动重试，也可手动立即重试。</HintText
        >
        <NativeSelect v-model="pushFilter.status" aria-label="按状态筛选">
          <NativeSelectOption value="">全部状态</NativeSelectOption>
          <NativeSelectOption value="failed">失败</NativeSelectOption>
          <NativeSelectOption value="retrying">等待重试</NativeSelectOption>
          <NativeSelectOption value="success">成功</NativeSelectOption>
          <NativeSelectOption value="skipped">跳过</NativeSelectOption>
          <NativeSelectOption value="queued">排队中</NativeSelectOption>
        </NativeSelect>
        <NativeSelect v-model.number="pushFilter.destination" aria-label="按目的地筛选">
          <NativeSelectOption :value="0">全部目的地</NativeSelectOption>
          <NativeSelectOption v-for="d in dests" :key="d.id" :value="d.id">{{
            d.name
          }}</NativeSelectOption>
        </NativeSelect>
        <NativeSelect v-model="sourceFilter" aria-label="按来源筛选">
          <NativeSelectOption value="">全部来源</NativeSelectOption>
          <NativeSelectOption v-for="x in sources" :key="x.key" :value="x.key">{{
            x.name
          }}</NativeSelectOption>
        </NativeSelect>
      </div>
      <ListSkeleton v-if="!pushes" variant="table" :rows="4" />
      <div v-else class="bg-card overflow-hidden rounded-xl border">
        <Table>
          <TableHeader class="bg-muted/40">
            <TableRow>
              <TableHead>时间</TableHead>
              <TableHead>推送规则</TableHead>
              <TableHead>状态</TableHead>
              <TableHead class="hidden md:table-cell">触发</TableHead>
              <TableHead class="hidden md:table-cell">详情</TableHead>
              <TableHead class="hidden w-px md:table-cell" />
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="p in pushes" :key="p.id">
              <TableCell class="whitespace-nowrap" :title="fmtTime(p.created_at)">{{
                fmtAgo(p.created_at)
              }}</TableCell>
              <TableCell class="whitespace-normal">
                {{ bindingLabel(p.binding_id) }}
                <div class="text-muted-foreground font-mono text-xs">
                  {{ identityKey(p.identity_id) }}
                </div>
                <!-- 窄屏不显示详情列，出错原因放在这里 -->
                <div
                  v-if="p.last_error"
                  class="mt-1 text-xs wrap-break-word md:hidden"
                  :class="p.status === 'skipped' ? 'text-muted-foreground' : 'text-destructive'"
                >
                  {{ p.last_error }}
                </div>
              </TableCell>
              <TableCell class="align-top">
                <StatusBadge :tone="pushStates[pushStateOf(p)].tone" dot>{{
                  pushStates[pushStateOf(p)].label
                }}</StatusBadge>
                <div v-if="p.attempts > 1" class="text-muted-foreground text-xs">
                  第 {{ p.attempts }} 次
                </div>
                <!-- 窄屏没有操作列，重试按钮放在状态下面 -->
                <Button
                  v-if="canRetry(p)"
                  variant="outline"
                  size="xs"
                  class="mt-1 md:hidden"
                  :disabled="retrying === p.id"
                  @click="retry(p)"
                  ><RefreshCwIcon /> 重试</Button
                >
              </TableCell>
              <TableCell class="hidden md:table-cell">{{
                pushTrigger[p.trigger] ?? p.trigger
              }}</TableCell>
              <TableCell class="hidden max-w-80 text-xs break-all whitespace-normal md:table-cell">
                <!-- 跳过不是失败，原因用中性色 -->
                <span
                  v-if="p.last_error"
                  :class="p.status === 'skipped' ? 'text-muted-foreground' : 'text-destructive'"
                  >{{ p.last_error }}</span
                >
                <span v-else-if="p.vars" class="flex flex-wrap gap-1">
                  <code
                    v-for="v in p.vars.split(',')"
                    :key="v"
                    class="bg-muted rounded px-1.5 py-0.5 font-mono"
                    >{{ v }}</code
                  >
                </span>
              </TableCell>
              <TableCell class="hidden text-right md:table-cell">
                <Button
                  v-if="canRetry(p)"
                  variant="outline"
                  size="xs"
                  :disabled="retrying === p.id"
                  @click="retry(p)"
                  ><RefreshCwIcon /> 立即重试</Button
                >
              </TableCell>
            </TableRow>
            <TableRow v-if="!pushes.length">
              <TableCell colspan="6" class="text-muted-foreground py-8 text-center"
                >暂无推送记录</TableCell
              >
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </TabsContent>
  </Tabs>

  <!-- 目的地表单 -->
  <FormDialog
    v-model:open="destForm.open"
    v-model:enabled="destForm.enabled"
    :title="destForm.id ? '编辑目的地' : '新建目的地'"
    :description="destForm.id ? destForm.name : '登录态更新后的推送目标'"
    :brand="destForm.kind === 'qinglong' ? 'qinglong' : undefined"
    :icon="WebhookIcon"
    :tone="destForm.kind === 'qinglong' ? 'emerald' : 'sky'"
    :busy="busy"
    @submit="saveDest"
  >
    <FormSection title="基本" :cols="1">
      <FormField label="类型" :hint="destForm.id ? '创建后不可修改' : ''">
        <ChoiceCards
          v-model="destForm.kind"
          :options="kindOptions"
          :disabled="!!destForm.id"
          label="类型"
        />
      </FormField>
      <FormField label="名称" for="d-name" :error="destCheck.errors.name">
        <Input id="d-name" v-model="destForm.name" placeholder="如 青龙-家庭" />
      </FormField>
    </FormSection>

    <FormSection
      v-if="destForm.kind === 'qinglong'"
      title="连接"
      description="在青龙“系统设置 → 应用设置”中创建应用，授权范围选择“环境变量”"
    >
      <FormField label="地址" for="d-base" full :error="destCheck.errors.base_url">
        <Input
          id="d-base"
          v-model="destForm.config.base_url"
          class="font-mono"
          placeholder="http://host:5700"
        />
      </FormField>
      <FormField label="Client ID" for="d-cid" :error="destCheck.errors.client_id">
        <Input id="d-cid" v-model="destForm.config.client_id" class="font-mono" />
      </FormField>
      <FormField label="Client Secret" for="d-csecret" :error="destCheck.errors.client_secret">
        <Input
          id="d-csecret"
          v-model="destForm.config.client_secret"
          type="password"
          autocomplete="new-password"
          :placeholder="destForm.secrets.client_secret ? '已设置，留空不修改' : ''"
        />
      </FormField>
    </FormSection>

    <template v-else>
      <FormSection title="请求">
        <div class="grid gap-4 sm:col-span-2 sm:grid-cols-[7rem_1fr]">
          <FormField label="方法" for="d-method">
            <NativeSelect id="d-method" v-model="destForm.config.method" class="w-full">
              <NativeSelectOption v-for="m in ['POST', 'PUT', 'PATCH']" :key="m" :value="m">{{
                m
              }}</NativeSelectOption>
            </NativeSelect>
          </FormField>
          <FormField label="URL" for="d-url" :error="destCheck.errors.url">
            <Input
              id="d-url"
              v-model="destForm.config.url"
              class="font-mono"
              placeholder="https://host/path"
            />
          </FormField>
        </div>
        <FormField label="请求头" optional full>
          <KeyValueEditor v-model="destForm.config.headers" />
        </FormField>
        <FormField label="请求体模板" for="d-body" optional full>
          <Textarea id="d-body" v-model="destForm.config.body" rows="4" class="font-mono text-xs" />
          <template #hint>
            Go 模板；留空时发送默认 JSON（source、identity、vars）。变量引用示例：<code
              class="font-mono"
              >{{ bodyExample }}</code
            >
          </template>
        </FormField>
      </FormSection>
      <FormSection title="签名" :cols="1">
        <FormField
          label="HMAC 密钥"
          for="d-secret"
          optional
          hint="设置后请求头携带 X-Ferry-Signature（HMAC-SHA256）"
        >
          <Input
            id="d-secret"
            v-model="destForm.config.secret"
            type="password"
            autocomplete="new-password"
            :placeholder="destForm.secrets.secret ? '已设置，留空不修改' : ''"
          />
        </FormField>
      </FormSection>
    </template>
  </FormDialog>

  <!-- 推送规则表单 -->
  <FormDialog
    v-model:open="bindForm.open"
    v-model:enabled="bindForm.enabled"
    :title="bindForm.id ? '编辑推送规则' : '新建推送规则'"
    :description="
      bindForm.id
        ? `${sourceName(bindForm.source_id)} → ${destName(bindForm.destination_id)}`
        : '将来源的输出变量推送至目的地'
    "
    :icon="SplitIcon"
    tone="emerald"
    :busy="busy"
    @submit="saveBinding"
  >
    <FormSection title="来源与目的地" :description="bindForm.id ? '创建后不可修改' : ''">
      <FormField
        label="来源"
        for="b-source"
        :hint="sources.length ? '' : noSources"
        :error="bindCheck.errors.source_id"
      >
        <NativeSelect
          id="b-source"
          v-model.number="bindForm.source_id"
          :disabled="!!bindForm.id"
          class="w-full"
          @change="pickSource"
        >
          <NativeSelectOption v-for="s in sources" :key="s.id" :value="s.id">{{
            s.name
          }}</NativeSelectOption>
        </NativeSelect>
      </FormField>
      <FormField
        label="目的地"
        for="b-dest"
        :hint="dests.length ? '' : noDests"
        :error="bindCheck.errors.destination_id"
      >
        <NativeSelect
          id="b-dest"
          v-model.number="bindForm.destination_id"
          :disabled="!!bindForm.id"
          class="w-full"
        >
          <NativeSelectOption v-for="d in dests" :key="d.id" :value="d.id">{{
            d.name
          }}</NativeSelectOption>
        </NativeSelect>
      </FormField>
    </FormSection>

    <FormSection title="写入方式" :cols="1">
      <ChoiceCards v-model="bindForm.mode" :options="modeOptions" label="写入方式" />
    </FormSection>

    <FormSection title="变量重命名" description="留空则沿用原变量名" :cols="1">
      <div v-for="o in outputs" :key="o" class="grid grid-cols-[1fr_auto_1fr] items-center gap-2">
        <code class="bg-muted truncate rounded px-2 py-1.5 font-mono text-xs">{{ o }}</code>
        <ArrowRightIcon class="text-muted-foreground size-4" />
        <Input
          v-model="bindForm.rename[o]"
          :aria-label="`${o} 重命名为`"
          class="font-mono text-xs"
          :placeholder="o"
        />
      </div>
      <p v-if="!outputs.length" class="text-muted-foreground text-sm">该来源未定义输出变量</p>
    </FormSection>
  </FormDialog>

  <!-- 手动推送 -->
  <FormDialog
    v-model:open="pushForm.open"
    title="手动推送"
    :description="pushForm.binding ? bindingLabel(pushForm.binding.id) : ''"
    :icon="SendIcon"
    tone="emerald"
    size="sm"
    submit-text="立即推送"
    :busy="busy"
    @submit="runPush"
  >
    <FormField
      v-if="pushForm.binding?.mode === 'per_identity'"
      label="身份"
      for="push-identity"
      :error="pushCheck.errors.identity_id"
    >
      <NativeSelect id="push-identity" v-model.number="pushForm.identity_id" class="w-full">
        <NativeSelectOption v-for="a in pushIdentities" :key="a.id" :value="a.id">{{
          a.key === 'default' ? '默认' : a.key
        }}</NativeSelectOption>
      </NativeSelect>
      <template v-if="!pushIdentities.length" #hint>该来源暂无登录态，回传后方可推送</template>
    </FormField>
    <p v-else class="text-muted-foreground text-sm">将全部身份的当前值合并后推送一次。</p>
  </FormDialog>
</template>
