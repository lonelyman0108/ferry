<script setup lang="ts">
import { HistoryIcon, LoaderCircleIcon, PencilIcon, PlayIcon, RefreshCwIcon } from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import ActivityList from '@/components/flow/ActivityList.vue'
import { clientName } from '@/components/flow/inbox'
import InboxItem from '@/components/flow/InboxItem.vue'
import { bindingBadges, deviceBadges, destStates, isSoon } from '@/components/flow/nodes'
import EntityIcon from '@/components/EntityIcon.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { fmtAgo, fmtTime, fmtUntil, secondaryKey } from '@/format'
import { destMark, deviceMark, icons } from '@/lib/icons'
import IdentityDialog from '@/views/IdentityDialog.vue'
import SourceEditor from '@/views/sources/SourceEditor.vue'
import {
  type Batch,
  bindingModes,
  type IdentityPush,
  type Lane,
  type PushJob,
  pushErrored,
  pushStates,
} from '@/types'

import DetailSection from './DetailSection.vue'
import SummaryCells from './SummaryCells.vue'

// 来源详情：从手机到目的地，这个来源的整条链路。左栏是身份、当前值和推送，右栏是订阅、收集中的回传和最近记录。
// 地址上带 ?edit=<分区> 时打开读取规则编辑器，带 ?identity=<ID> 时打开那个身份的历史与审核。
// 左侧聚焦窄栏显示时（容器 focus 够宽），覆盖此来源的订阅已经列在栏里，详情不再重复列出。
const props = defineProps<{ id: number }>()
const emit = defineEmits<{ changed: []; focus: [kind: 'device', id: number]; title: [string] }>()

const route = useRoute()
const router = useRouter()
const lane = ref<Lane | null>(null)
const batches = ref<Batch[]>([])

async function load() {
  try {
    const [l, b] = await Promise.all([
      api.get<Lane>(`/sources/${props.id}/lane`),
      api.get<Batch[]>('/batches'),
    ])
    lane.value = l.data
    batches.value = b.data.filter((x) => x.source_id === props.id)
    emit('title', l.data.source.name)
  } catch (e) {
    toast.error(errorText(e))
  }
}

watch(
  () => props.id,
  () => {
    lane.value = null
    void load()
  },
  { immediate: true },
)

// 处理完一件事：自己刷新，也让外面刷新流向图和顶栏计数。
async function refresh() {
  await load()
  emit('changed')
}

const src = computed(() => lane.value?.source)
const editSection = computed(() => (typeof route.query.edit === 'string' ? route.query.edit : ''))
const identityOpen = computed(() =>
  typeof route.query.identity === 'string' ? route.query.identity : '',
)
const setQuery = (q: Record<string, string | undefined>) =>
  router.replace({ query: { ...route.query, ...q } })

const summary = computed(() => {
  const s = src.value
  const l = lane.value
  if (!s || !l) return []
  const failed = l.bindings.filter((b) => b.state === 'failed').length
  const pushed = l.bindings
    .map((b) => b.last_at)
    .filter((x): x is string => !!x)
    .sort()
    .pop()
  return [
    {
      label: '身份',
      value: `${s.identities} 个`,
      sub: s.pending_review ? `${s.pending_review} 个值待审核` : undefined,
    },
    {
      label: '最早过期',
      value: s.expires_at ? fmtUntil(s.expires_at) : '—',
      tone: isSoon(s.expires_at) ? ('warning' as const) : undefined,
    },
    { label: '最近回传', value: fmtAgo(s.last_seen_at) },
    {
      label: '推送规则',
      value: `${l.bindings.length} 条`,
      sub: failed ? `${failed} 条失败` : pushed ? `最近 ${fmtAgo(pushed)}` : undefined,
      tone: failed ? ('danger' as const) : undefined,
    },
  ]
})

// ---- 推送 ----
const busy = ref('')

async function pushNow(p: IdentityPush, identityID: number) {
  busy.value = `push-${p.binding_id}-${identityID}`
  try {
    const { data } = await api.post<PushJob>('/pushes', {
      binding_id: p.binding_id,
      identity_id: identityID,
    })
    if (data.status === 'success') toast.success(`已推送到 ${p.destination_name}`)
    else
      toast.error(
        `推送失败：${data.last_error || pushStates[data.status === 'queued' ? 'retrying' : 'failed'].label}`,
      )
    await refresh()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

async function retry(pushID: number, dest: string) {
  busy.value = `retry-${pushID}`
  try {
    const { data } = await api.post<PushJob>(`/pushes/${pushID}/retry`, {})
    if (data.status === 'success') toast.success(`已推送到 ${dest}`)
    else toast.error(`推送失败：${data.last_error || '等待自动重试'}`)
    await refresh()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

function pushState(p: IdentityPush) {
  if (!p.enabled) return { label: '已停用', tone: 'neutral' as const }
  if (!p.last) return destStates.none!
  const st =
    p.last.status === 'queued' && p.last.last_error
      ? 'retrying'
      : p.last.status === 'running'
        ? 'queued'
        : p.last.status
  return pushStates[st]
}

// ---- 收集中的回传 ----
async function processBatch(b: Batch) {
  busy.value = `batch-${b.id}`
  try {
    await api.post(`/batches/${b.id}/process`, {})
    toast.success('已处理')
    await refresh()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

const identityLabel = (key: string) => (key === 'default' ? '默认身份' : key)
</script>

<template>
  <div v-if="!lane || !src" class="grid grid-cols-1 gap-4">
    <Skeleton class="h-16" />
    <Skeleton class="h-20" />
    <Skeleton class="h-64" />
  </div>
  <div v-else class="grid grid-cols-1 gap-6">
    <header class="flex flex-wrap items-start gap-x-4 gap-y-3">
      <SourceAvatar :name="src.name" :skey="src.key" :disabled="!src.enabled" size="lg" />
      <div class="min-w-0 flex-1">
        <h1 class="flex flex-wrap items-center gap-2 text-xl font-semibold">
          {{ src.name }}
          <StatusBadge v-if="!src.enabled" tone="neutral" dot>已停用</StatusBadge>
          <StatusBadge v-if="lane.review === 'manual'" tone="warning" dot>人工审核</StatusBadge>
          <StatusBadge v-if="lane.probe" tone="info" dot>有效性检测</StatusBadge>
        </h1>
        <p class="text-muted-foreground font-mono text-sm">{{ src.key }}</p>
      </div>
      <div class="flex w-full flex-wrap gap-2 sm:w-auto">
        <Button variant="outline" size="sm" @click="setQuery({ edit: 'setup' })"
          ><component :is="icons.checklist" /> 接入清单</Button
        >
        <Button size="sm" @click="setQuery({ edit: 'basic' })"><PencilIcon /> 编辑读取规则</Button>
      </div>
    </header>

    <SummaryCells :cells="summary" />

    <section
      v-if="lane.items.length"
      class="bg-card grid grid-cols-1 gap-3 rounded-lg border p-4"
      aria-label="相关待处理事项"
    >
      <h2 class="text-muted-foreground text-xs">相关待处理事项</h2>
      <div
        v-for="it in lane.items"
        :key="`${it.kind}-${it.version_id ?? it.push_id}`"
        class="border-b pb-3 last:border-b-0 last:pb-0"
      >
        <InboxItem :item="it" @done="refresh" />
      </div>
    </section>

    <div class="grid grid-cols-1 gap-8 @3xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
      <div class="grid grid-cols-1 content-start gap-8">
        <DetailSection title="身份与当前值" aside="敏感值仅显示首尾字符">
          <p v-if="!lane.identities.length" class="text-muted-foreground text-sm">
            暂无回传。设备安装订阅并打开对应 App 后，登录态将自动回传。
          </p>
          <article
            v-for="a in lane.identities"
            :key="a.id"
            class="bg-card grid grid-cols-1 gap-3 rounded-lg border p-4"
          >
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="font-medium" :class="a.key !== 'default' && 'font-mono'">
                {{ identityLabel(a.key) }}
              </h3>
              <StatusBadge v-if="a.fields.some((f) => f.pending)" tone="warning" dot
                >{{ a.fields.reduce((n, f) => n + f.pending, 0) }} 个值待审核</StatusBadge
              >
              <span class="text-muted-foreground text-xs" :title="fmtTime(a.last_seen_at)"
                >最近回传 {{ fmtAgo(a.last_seen_at) }}</span
              >
              <Button
                variant="ghost"
                size="sm"
                class="ml-auto"
                @click="setQuery({ identity: String(a.id) })"
                ><HistoryIcon /> 历史与审核</Button
              >
            </div>
            <dl class="grid grid-cols-1 gap-1.5 text-sm">
              <div
                v-for="f in a.fields"
                :key="f.key"
                class="grid grid-cols-[minmax(0,8rem)_minmax(0,1fr)_auto] items-baseline gap-3"
              >
                <dt class="text-muted-foreground min-w-0">
                  <span class="block truncate">{{ f.name || f.key }}</span>
                  <span
                    v-if="secondaryKey(f.name, f.key)"
                    class="block truncate font-mono text-xs"
                    >{{ f.key }}</span
                  >
                </dt>
                <dd class="font-mono [overflow-wrap:anywhere]">
                  {{ f.current?.value ?? '—' }}
                </dd>
                <dd
                  class="text-xs whitespace-nowrap"
                  :class="isSoon(f.current?.expires_at) ? 'text-warning' : 'text-muted-foreground'"
                >
                  {{ f.current?.expires_at ? fmtUntil(f.current.expires_at) + '过期' : '' }}
                </dd>
              </div>
            </dl>
            <ul v-if="a.pushes.length" class="grid grid-cols-1 gap-1.5 border-t pt-3">
              <li
                v-for="p in a.pushes"
                :key="p.binding_id"
                class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2 text-sm"
              >
                <span class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                  <EntityIcon :mark="destMark(p.destination_kind)" size="xs" />
                  <span class="min-w-0 [overflow-wrap:anywhere]">{{ p.destination_name }}</span>
                  <StatusBadge :tone="pushState(p).tone" dot>{{ pushState(p).label }}</StatusBadge>
                  <span v-if="p.last" class="text-muted-foreground text-xs">{{
                    fmtAgo(p.last.finished_at ?? p.last.created_at)
                  }}</span>
                </span>
                <Button
                  v-if="p.enabled"
                  variant="ghost"
                  size="sm"
                  :disabled="!!busy"
                  @click="pushNow(p, a.id)"
                >
                  <LoaderCircleIcon
                    v-if="busy === `push-${p.binding_id}-${a.id}`"
                    class="animate-spin"
                  /><component :is="icons.push" v-else /> 立即推送
                </Button>
                <span
                  v-if="p.last?.last_error && p.last.status !== 'success'"
                  class="col-span-2 pl-5 text-xs [overflow-wrap:anywhere]"
                  :class="pushErrored(p.last) ? 'text-destructive' : 'text-muted-foreground'"
                  >{{ p.last.last_error }}</span
                >
              </li>
            </ul>
          </article>
        </DetailSection>

        <DetailSection title="推送规则">
          <template #aside>
            <router-link
              :to="{ path: '/destinations', query: { tab: 'bindings', source: src.key } }"
              class="text-primary text-sm underline-offset-2 hover:underline max-md:relative max-md:-my-3 max-md:-mr-2 max-md:flex max-md:min-h-11 max-md:items-center max-md:px-2"
              >管理推送规则</router-link
            >
          </template>
          <p v-if="!lane.bindings.length" class="text-muted-foreground text-sm">
            暂无推送规则。登录态仅保存在 Ferry 中，不推送至任何目的地。
          </p>
          <!-- 整行可点：目的地名的 ::after 铺满一行，「立即重试」和出错原因叠在它上面（relative z-10） -->
          <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
            <li
              v-for="b in lane.bindings"
              :key="b.id"
              class="has-[[data-row-link]:hover]:bg-accent/50 has-[[data-row-link]:focus-visible]:ring-ring/50 relative flex flex-wrap items-center gap-2 p-3 has-[[data-row-link]:focus-visible]:ring-3 has-[[data-row-link]:focus-visible]:ring-inset"
            >
              <EntityIcon :mark="destMark(b.destination_kind)" size="xs" />
              <button
                type="button"
                data-row-link
                class="text-left font-medium outline-none after:absolute after:inset-0"
                @click="router.push(`/destinations/${b.destination_id}`)"
              >
                {{ b.destination_name }}
              </button>
              <span class="text-muted-foreground text-xs">{{
                bindingModes[b.mode as keyof typeof bindingModes] ?? b.mode
              }}</span>
              <StatusBadge v-for="x in bindingBadges(b)" :key="x.label" :tone="x.tone" dot>{{
                x.label
              }}</StatusBadge>
              <span v-if="b.last_at" class="text-muted-foreground text-xs">{{
                fmtAgo(b.last_at)
              }}</span>
              <Button
                v-if="b.push_id && (b.state === 'failed' || b.state === 'retrying')"
                size="sm"
                class="relative z-10 ml-auto"
                :disabled="!!busy"
                @click="retry(b.push_id, b.destination_name)"
              >
                <RefreshCwIcon :class="busy === `retry-${b.push_id}` && 'animate-spin'" /> 立即重试
              </Button>
              <p
                v-if="b.last_error && (b.state === 'failed' || b.state === 'retrying')"
                class="text-destructive relative z-10 w-full text-sm break-words"
              >
                {{ b.last_error }}
              </p>
            </li>
          </ul>
        </DetailSection>
      </div>

      <div class="grid grid-cols-1 content-start gap-8">
        <DetailSection
          title="覆盖此来源的订阅"
          :aside="`${lane.devices.length} 台`"
          class="@4xl/focus:hidden"
        >
          <p v-if="!lane.devices.length" class="text-muted-foreground text-sm">
            暂无覆盖此来源的订阅。
          </p>
          <ul v-else class="bg-card divide-y rounded-lg border">
            <li v-for="d in lane.devices" :key="d.id">
              <button
                type="button"
                class="hover:bg-accent/50 flex w-full flex-wrap items-center gap-2 p-3 text-left"
                @click="emit('focus', 'device', d.id)"
              >
                <EntityIcon :mark="deviceMark(d.client)" size="xs" />
                <span class="font-medium">{{ d.name }}</span>
                <span class="text-muted-foreground text-xs">{{ clientName(d.client) }}</span>
                <StatusBadge v-for="b in deviceBadges(d)" :key="b.label" :tone="b.tone" dot>{{
                  b.label
                }}</StatusBadge>
                <span v-if="d.last_seen_at" class="text-muted-foreground ml-auto text-xs"
                  >最近连接 {{ fmtAgo(d.last_seen_at) }}</span
                >
              </button>
            </li>
          </ul>
        </DetailSection>

        <DetailSection v-if="batches.length" title="收集中的回传" aside="待其余字段到齐后统一处理">
          <ul class="bg-card divide-y rounded-lg border">
            <li
              v-for="b in batches"
              :key="b.id"
              class="flex flex-wrap items-center gap-2 p-3 text-sm"
            >
              <span>来自 {{ b.subscription_name }}</span>
              <span class="text-muted-foreground text-xs">{{ fmtAgo(b.created_at) }}</span>
              <span v-if="b.missing.length" class="text-muted-foreground w-full text-xs"
                >缺少 {{ b.missing.join('、') }}</span
              >
              <Button
                variant="outline"
                size="sm"
                class="ml-auto"
                :disabled="!!busy"
                @click="processBatch(b)"
                ><PlayIcon /> 立即处理</Button
              >
            </li>
          </ul>
        </DetailSection>

        <DetailSection title="最近记录">
          <template #aside>
            <router-link
              :to="{ path: '/activity', query: { source: src.key } }"
              class="text-primary text-sm underline-offset-2 hover:underline max-md:relative max-md:-my-3 max-md:-mr-2 max-md:flex max-md:min-h-11 max-md:items-center max-md:px-2"
              >全部记录</router-link
            >
          </template>
          <p v-if="!lane.recent.length" class="text-muted-foreground text-sm">暂无记录。</p>
          <ActivityList v-else :items="lane.recent" compact />
        </DetailSection>
      </div>
    </div>
  </div>

  <SourceEditor
    v-if="editSection"
    :id="String(id)"
    :key="`edit-${id}`"
    :initial-section="editSection"
    @close="setQuery({ edit: undefined })"
    @saved="(sid: number) => (sid ? refresh() : router.push('/sources'))"
  />
  <IdentityDialog
    v-if="identityOpen"
    :id="identityOpen"
    :key="`identity-${identityOpen}`"
    :source-enabled="src?.enabled"
    @close="setQuery({ identity: undefined })"
    @changed="refresh"
  />
</template>
