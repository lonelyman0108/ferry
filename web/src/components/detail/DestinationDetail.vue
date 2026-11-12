<script setup lang="ts">
import { PencilIcon, PlugZapIcon, RefreshCwIcon } from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import InboxItem from '@/components/flow/InboxItem.vue'
import { bindingBadges, destBadges } from '@/components/flow/nodes'
import EntityIcon from '@/components/EntityIcon.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { fmtAgo, fmtTime } from '@/format'
import { destMark } from '@/lib/icons'
import { useFlow } from '@/stores/flow'
import { useOverview } from '@/stores/overview'
import {
  bindingModes,
  type Destination,
  type PushJob,
  pushErrored,
  pushStateOf,
  pushStates,
  pushTrigger,
} from '@/types'

import DetailSection from './DetailSection.vue'
import SummaryCells from './SummaryCells.vue'

// 目的地详情：推到这里的推送规则和各自状态，最近的推送记录，可以测试连接、立即重试。
// 编辑目的地在推送页的列表上进行，带 ?edit=<ID> 跳过去直接打开编辑表单；全部推送记录同样跳到推送页，按本目的地筛选。
const props = defineProps<{ id: number }>()
const emit = defineEmits<{ changed: []; focus: [kind: 'source', id: number]; title: [string] }>()

const router = useRouter()
const flow = useFlow()
const overview = useOverview()
const dest = ref<Destination | null>(null)
const pushes = ref<PushJob[]>([])
const busy = ref('')

async function load() {
  try {
    const [d, p] = await Promise.all([
      api.get<Destination[]>('/destinations'),
      api.get<PushJob[]>(`/pushes?destination=${props.id}`),
    ])
    dest.value = d.data.find((x) => x.id === props.id) ?? null
    pushes.value = p.data.slice(0, 20)
    if (dest.value) emit('title', dest.value.name)
  } catch (e) {
    toast.error(errorText(e))
  }
}

watch(
  () => props.id,
  () => {
    dest.value = null
    void load()
  },
  { immediate: true },
)

const node = computed(() => flow.data?.destinations.find((d) => d.id === props.id))
const sourceOf = (id: number) => flow.data?.sources.find((s) => s.id === id)
const sourceName = (id: number) => sourceOf(id)?.name ?? '已删除的来源'
const bindings = computed(() =>
  (flow.data?.bindings ?? []).filter((b) => b.destination_id === props.id),
)
const bindingSourceID = (bindingID: number) =>
  flow.data?.bindings.find((b) => b.id === bindingID)?.source_id ?? 0
const items = computed(() =>
  (overview.data?.items ?? []).filter((it) => it.destination_id === props.id),
)

const address = computed(() => {
  const c = dest.value?.config
  if (!c) return ''
  return dest.value?.kind === 'webhook'
    ? `${c.method ?? 'POST'} ${c.url ?? ''}`
    : (c.base_url ?? '')
})

const summary = computed(() => {
  const n = node.value
  if (!n) return []
  const failed = bindings.value.filter((b) => b.state === 'failed').length
  // 状态已经写在标题的徽章上，这里改为看最近这些推送里出错的情况
  const errored = pushes.value.filter(pushErrored)
  const lastErr = errored[0]
  return [
    { label: '类型', value: n.kind === 'webhook' ? 'Webhook' : '青龙' },
    {
      label: '推送规则',
      value: `${n.bindings} 条`,
      sub: failed ? `${failed} 条失败` : undefined,
      tone: failed ? ('danger' as const) : undefined,
    },
    { label: '最近推送', value: fmtAgo(n.last_at) },
    {
      label: '最近失败',
      value: lastErr ? fmtAgo(lastErr.finished_at ?? lastErr.created_at) : '无',
      sub: errored.length ? `近 ${pushes.value.length} 次中 ${errored.length} 次` : undefined,
    },
  ]
})

async function refresh() {
  await load()
  emit('changed')
}

async function test() {
  busy.value = 'test'
  try {
    const { data } = await api.post<{ ok: boolean; error?: string }>(
      `/destinations/${props.id}/test`,
      {},
    )
    if (data.ok) toast.success('连接正常')
    else toast.error(`连接失败：${data.error}`)
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

async function retry(pushID: number) {
  busy.value = `retry-${pushID}`
  try {
    const { data } = await api.post<PushJob>(`/pushes/${pushID}/retry`, {})
    if (data.status === 'success') toast.success('已推送')
    else toast.error(`推送失败：${data.last_error || '等待自动重试'}`)
    await refresh()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

const jobState = (j: PushJob) => pushStates[pushStateOf(j)]
const jobTime = (j: PushJob) => j.finished_at ?? j.created_at
</script>

<template>
  <div v-if="!dest || !node" class="grid grid-cols-1 gap-4">
    <Skeleton class="h-16" />
    <Skeleton class="h-20" />
    <Skeleton class="h-48" />
  </div>
  <div v-else class="grid grid-cols-1 gap-6">
    <header class="flex flex-wrap items-start gap-x-4 gap-y-3">
      <EntityIcon :mark="destMark(dest.kind)" size="lg" />
      <div class="min-w-0 flex-1">
        <h1 class="flex flex-wrap items-center gap-2 text-xl font-semibold">
          {{ dest.name }}
          <StatusBadge
            v-for="b in destBadges(flow.data!, node)"
            :key="b.label"
            :tone="b.tone"
            dot
            >{{ b.label }}</StatusBadge
          >
        </h1>
        <p class="text-muted-foreground font-mono text-sm break-all">{{ address }}</p>
      </div>
      <div class="flex w-full flex-wrap gap-2 sm:w-auto">
        <Button
          variant="outline"
          size="sm"
          @click="router.push({ path: '/destinations', query: { edit: String(dest.id) } })"
          ><PencilIcon /> 编辑目的地</Button
        >
        <Button size="sm" :disabled="busy === 'test'" @click="test"
          ><PlugZapIcon /> 测试连接</Button
        >
      </div>
    </header>

    <SummaryCells :cells="summary" />

    <section
      v-if="items.length"
      class="bg-card grid grid-cols-1 gap-3 rounded-lg border p-4"
      aria-label="相关待处理事项"
    >
      <h2 class="text-muted-foreground text-xs">相关待处理事项</h2>
      <div
        v-for="it in items"
        :key="`${it.kind}-${it.push_id}`"
        class="border-b pb-3 last:border-b-0 last:pb-0"
      >
        <InboxItem :item="it" @done="refresh" />
      </div>
    </section>

    <div class="grid grid-cols-1 gap-8 @3xl:grid-cols-2">
      <DetailSection title="推送至此的来源" :aside="`${bindings.length} 条推送规则`">
        <p v-if="!bindings.length" class="text-muted-foreground text-sm">暂无关联的推送规则。</p>
        <!-- 整行可点：来源名的 ::after 铺满一行，「立即重试」叠在它上面（relative z-10） -->
        <ul v-else class="bg-card divide-y overflow-hidden rounded-lg border">
          <li
            v-for="b in bindings"
            :key="b.id"
            class="has-[[data-row-link]:hover]:bg-accent/50 has-[[data-row-link]:focus-visible]:ring-ring/50 relative flex flex-wrap items-center gap-2 p-3 has-[[data-row-link]:focus-visible]:ring-3 has-[[data-row-link]:focus-visible]:ring-inset"
          >
            <SourceAvatar
              :name="sourceName(b.source_id)"
              :skey="sourceOf(b.source_id)?.key ?? ''"
              size="xs"
              :disabled="sourceOf(b.source_id)?.enabled === false"
            />
            <button
              type="button"
              data-row-link
              class="text-left font-medium outline-none after:absolute after:inset-0"
              @click="emit('focus', 'source', b.source_id)"
            >
              {{ sourceName(b.source_id) }}
            </button>
            <span class="text-muted-foreground text-xs">{{
              bindingModes[b.mode as keyof typeof bindingModes] ?? b.mode
            }}</span>
            <StatusBadge
              v-for="x in bindingBadges(b, sourceOf(b.source_id)?.enabled !== false)"
              :key="x.label"
              :tone="x.tone"
              dot
              >{{ x.label }}</StatusBadge
            >
            <Button
              v-if="b.push_id && (b.state === 'failed' || b.state === 'retrying')"
              size="sm"
              class="relative z-10 ml-auto"
              :disabled="!!busy"
              @click="retry(b.push_id)"
            >
              <RefreshCwIcon :class="busy === `retry-${b.push_id}` && 'animate-spin'" /> 立即重试
            </Button>
          </li>
        </ul>
      </DetailSection>

      <DetailSection title="最近推送">
        <template #aside>
          <router-link
            :to="{ path: '/destinations', query: { tab: 'pushes', destination: String(dest.id) } }"
            class="text-primary text-sm underline-offset-2 hover:underline max-md:relative max-md:-my-3 max-md:-mr-2 max-md:flex max-md:min-h-11 max-md:items-center max-md:px-2"
            >全部记录</router-link
          >
        </template>
        <p v-if="!pushes.length" class="text-muted-foreground text-sm">暂无推送记录。</p>
        <ul v-else class="bg-card divide-y rounded-lg border text-sm">
          <li v-for="j in pushes" :key="j.id" class="grid grid-cols-1 gap-1 p-3">
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge :tone="jobState(j).tone" dot>{{ jobState(j).label }}</StatusBadge>
              <button
                v-if="sourceOf(bindingSourceID(j.binding_id))"
                type="button"
                class="text-left underline-offset-2 hover:underline max-md:relative max-md:-my-3 max-md:min-h-11"
                @click="emit('focus', 'source', bindingSourceID(j.binding_id))"
              >
                {{ sourceName(bindingSourceID(j.binding_id)) }}
              </button>
              <span v-else>{{ sourceName(0) }}</span>
              <span class="text-muted-foreground text-xs">{{
                pushTrigger[j.trigger] ?? j.trigger
              }}</span>
              <span class="text-muted-foreground ml-auto text-xs" :title="fmtTime(jobTime(j))">{{
                fmtAgo(jobTime(j))
              }}</span>
            </div>
            <p
              v-if="j.last_error && j.status !== 'success'"
              class="text-xs break-words"
              :class="pushErrored(j) ? 'text-destructive' : 'text-muted-foreground'"
            >
              {{ j.last_error }}
            </p>
            <div v-else-if="j.vars" class="flex flex-wrap gap-1 text-xs">
              <code
                v-for="v in j.vars.split(',')"
                :key="v"
                class="bg-muted rounded px-1.5 py-0.5 font-mono [overflow-wrap:anywhere]"
                >{{ v }}</code
              >
            </div>
          </li>
        </ul>
      </DetailSection>
    </div>
  </div>
</template>
