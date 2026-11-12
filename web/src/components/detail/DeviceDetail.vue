<script setup lang="ts">
import { QrCodeIcon, SettingsIcon } from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import { clientName } from '@/components/flow/inbox'
import InboxItem from '@/components/flow/InboxItem.vue'
import { deviceBadges, sourceBadges } from '@/components/flow/nodes'
import EntityIcon from '@/components/EntityIcon.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { fmtAgo, fmtTime } from '@/format'
import { deviceMark } from '@/lib/icons'
import { useFlow } from '@/stores/flow'
import { useOverview } from '@/stores/overview'
import { useSourceIcons } from '@/stores/sourceIcons'
import type { SourceSummary, Subscription } from '@/types'
import SubscriptionInstallDialog from '@/views/SubscriptionInstallDialog.vue'

import DetailSection from './DetailSection.vue'
import SummaryCells from './SummaryCells.vue'

// 设备（订阅）详情：安装与连通状态、传输方式、覆盖的来源、手机上要放行的域名。
const props = defineProps<{ id: number }>()
const emit = defineEmits<{ changed: []; focus: [kind: 'source', id: number]; title: [string] }>()

const router = useRouter()
const flow = useFlow()
const overview = useOverview()
const sub = ref<Subscription | null>(null)
const sources = ref<SourceSummary[]>([])
const installing = ref(false)

async function load() {
  try {
    const [s, src] = await Promise.all([
      api.get<Subscription>(`/subscriptions/${props.id}`),
      api.get<SourceSummary[]>('/sources'),
    ])
    sub.value = s.data
    sources.value = src.data
    useSourceIcons().set(src.data)
    emit('title', s.data.name)
  } catch (e) {
    toast.error(errorText(e))
  }
}

watch(
  () => props.id,
  () => {
    sub.value = null
    void load()
  },
  { immediate: true },
)

const node = computed(() => flow.data?.devices.find((d) => d.id === props.id))
const covered = computed(() =>
  (flow.data?.sources ?? []).filter((s) => node.value?.source_ids.includes(s.id)),
)
const items = computed(() =>
  (overview.data?.items ?? []).filter((it) => it.subscription_id === props.id),
)

const statusText = { connected: '已连通', installed: '已安装', not_installed: '未安装' }
const summary = computed(() => {
  const s = sub.value
  if (!s) return []
  return [
    {
      label: '状态',
      value: statusText[s.status],
      sub: s.outdated ? 'MITM 域名已变更，需更新订阅' : undefined,
    },
    { label: '传输方式', value: s.transport_mode === 'sealed' ? '加密' : '明文' },
    {
      label: '最近连接',
      value: fmtAgo(s.last_seen_at),
      sub: s.file_fetched_at ? `最近下载订阅 ${fmtAgo(s.file_fetched_at)}` : undefined,
    },
    {
      label: '覆盖来源',
      value: s.all_sources ? `全部（${covered.value.length} 个）` : `${covered.value.length} 个`,
    },
  ]
})

async function onUpdated() {
  await load()
  emit('changed')
}
</script>

<template>
  <div v-if="!sub" class="grid grid-cols-1 gap-4">
    <Skeleton class="h-16" />
    <Skeleton class="h-20" />
    <Skeleton class="h-48" />
  </div>
  <div v-else class="grid grid-cols-1 gap-6">
    <header class="flex flex-wrap items-start gap-x-4 gap-y-3">
      <EntityIcon :mark="deviceMark(sub.client)" size="lg" />
      <div class="min-w-0 flex-1">
        <h1 class="flex flex-wrap items-center gap-2 text-xl font-semibold">
          {{ sub.name }}
          <StatusBadge
            v-for="b in node ? deviceBadges(node) : []"
            :key="b.label"
            :tone="b.tone"
            dot
            >{{ b.label }}</StatusBadge
          >
        </h1>
        <p class="text-muted-foreground text-sm">
          {{ clientName(sub.client) }} · 令牌 <span class="font-mono">{{ sub.token_prefix }}…</span>
        </p>
      </div>
      <div class="flex w-full flex-wrap gap-2 sm:w-auto">
        <Button variant="outline" size="sm" @click="router.push('/subscriptions')"
          ><SettingsIcon /> 在订阅页管理</Button
        >
        <Button size="sm" @click="installing = true"><QrCodeIcon /> 安装引导</Button>
      </div>
    </header>

    <SummaryCells :cells="summary" />

    <section
      v-if="items.length"
      class="bg-card grid grid-cols-1 gap-3 rounded-lg border p-4"
      aria-label="相关待处理事项"
    >
      <h2 class="text-muted-foreground text-xs">相关待处理事项</h2>
      <div v-for="it in items" :key="it.kind" class="border-b pb-3 last:border-b-0 last:pb-0">
        <InboxItem :item="it" @done="emit('changed')" />
      </div>
    </section>

    <div class="grid grid-cols-1 gap-8 lg:grid-cols-2">
      <DetailSection title="覆盖的来源" :aside="`${covered.length} 个`">
        <ul class="bg-card divide-y rounded-lg border">
          <li v-for="s in covered" :key="s.id">
            <button
              type="button"
              class="hover:bg-accent/50 flex w-full flex-wrap items-center gap-2 p-3 text-left"
              @click="emit('focus', 'source', s.id)"
            >
              <SourceAvatar :name="s.name" :skey="s.key" :disabled="!s.enabled" size="xs" />
              <span class="font-medium">{{ s.name }}</span>
              <span class="text-muted-foreground font-mono text-xs">{{ s.key }}</span>
              <StatusBadge v-for="b in sourceBadges(s)" :key="b.label" :tone="b.tone" dot>{{
                b.label
              }}</StatusBadge>
            </button>
          </li>
        </ul>
      </DetailSection>

      <DetailSection title="需解密的域名" aside="须加入代理客户端的 MITM 主机名列表">
        <ul class="bg-card grid grid-cols-1 gap-1 rounded-lg border p-3 font-mono text-sm">
          <li v-for="h in sub.hostnames" :key="h">{{ h }}</li>
          <li v-if="!sub.hostnames.length" class="text-muted-foreground font-sans">
            覆盖的来源均未配置匹配规则
          </li>
        </ul>
        <p v-if="sub.transport_mode === 'sealed'" class="text-muted-foreground text-xs">
          <template v-if="sub.transport.verified_at"
            >加密传输：密钥更换后首次解密成功于 {{ fmtTime(sub.transport.verified_at) }}</template
          >
          <template v-else>加密传输：密钥更换后尚未成功解密该设备的报文</template>
        </p>
      </DetailSection>
    </div>
  </div>

  <SubscriptionInstallDialog
    :subscription="installing ? sub : null"
    :sources="sources"
    @close="installing = false"
    @updated="onUpdated"
  />
</template>
