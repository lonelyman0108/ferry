<script setup lang="ts">
import { CheckIcon, DownloadIcon, LoaderCircleIcon, PlusIcon, TriangleAlertIcon } from '@lucide/vue'
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import BrandIcon from '@/components/BrandIcon.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { fmtAgo } from '@/format'
import { cn } from '@/lib/utils'
import { useOverview } from '@/stores/overview'
import {
  type Batch,
  type Binding,
  bindingModes,
  type Destination,
  type Identity,
  type SourceSummary,
  type Subscription,
  subscriptionStatus,
} from '@/types'

import SubscriptionInstallDialog from '../SubscriptionInstallDialog.vue'

// 来源的接入清单：加入订阅 → 在客户端里安装或更新 → 推送到目的地 → 等第一次回传。
// 每一步都可以就地完成；打开期间每 5 秒刷新，回传到了、订阅更新了都会就地显示。
const props = defineProps<{ source: { id: number; key: string; name: string; enabled: boolean } }>()

const overview = useOverview()
const loaded = ref(false)
const subs = ref<Subscription[]>([])
const sources = ref<SourceSummary[]>([])
const dests = ref<Destination[]>([])
const bindings = ref<Binding[]>([])
const identities = ref<Identity[]>([])
const batches = ref<Batch[]>([])
const installFor = ref<Subscription | null>(null)
const busy = ref(0)

async function load() {
  try {
    const [s, src, d, b, a, bt] = await Promise.all([
      api.get<Subscription[]>('/subscriptions'),
      api.get<SourceSummary[]>('/sources'),
      api.get<Destination[]>('/destinations'),
      api.get<Binding[]>('/bindings'),
      api.get<Identity[]>('/identities', { params: { source: props.source.key } }),
      api.get<Batch[]>('/batches'),
    ])
    subs.value = s.data
    sources.value = src.data
    dests.value = d.data
    bindings.value = b.data.filter((x) => x.source_id === props.source.id)
    identities.value = a.data
    batches.value = bt.data.filter((x) => x.source_id === props.source.id)
  } catch (e) {
    if (!loaded.value) toast.error(errorText(e))
  } finally {
    loaded.value = true
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 5000)
})
onUnmounted(() => clearInterval(timer))

const included = (s: Subscription) => s.all_sources || s.source_keys.includes(props.source.key)
const usedBy = computed(() => subs.value.filter(included))
const pending = computed(() =>
  usedBy.value.filter((s) => s.outdated || s.status === 'not_installed'),
)
const lastSeen = computed(
  () =>
    identities.value
      .map((a) => a.last_seen_at)
      .sort()
      .pop() ?? null,
)

const done = computed(() => ({
  subscribe: usedBy.value.length > 0,
  install: usedBy.value.length > 0 && pending.value.length === 0,
  push: bindings.value.length > 0,
  data: identities.value.length > 0,
}))
const stepClass = (ok: boolean) =>
  cn(
    'flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold',
    ok ? 'border-success bg-success text-white' : 'text-muted-foreground',
  )

// 勾选、取消一个订阅：只改它的来源列表，其余设置原样提交。
async function toggle(s: Subscription, on: boolean) {
  busy.value = s.id
  try {
    const keys = on
      ? [...s.source_keys, props.source.key]
      : s.source_keys.filter((k) => k !== props.source.key)
    await api.put(`/subscriptions/${s.id}`, {
      name: s.name,
      client: s.client,
      endpoint_id: s.endpoint_id,
      fallback_id: s.fallback_id || null,
      all_sources: false,
      source_keys: keys,
      transport_mode: s.transport.mode,
      script_url: s.script_url,
    })
    await Promise.all([load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = 0
  }
}

// ---- 推送到目的地 ----
const destName = (id: number) => dests.value.find((d) => d.id === id)?.name ?? `#${id}`
const unbound = computed(() =>
  dests.value.filter((d) => !bindings.value.some((b) => b.destination_id === d.id)),
)
const bindForm = reactive({ destination_id: 0, mode: 'per_identity' as Binding['mode'] })
async function addBinding() {
  const dest = bindForm.destination_id || unbound.value[0]?.id
  if (!dest) return
  busy.value = -1
  try {
    await api.post('/bindings', {
      source_id: props.source.id,
      destination_id: dest,
      mode: bindForm.mode,
      rename: {},
      enabled: true,
    })
    bindForm.destination_id = 0
    toast.success('已添加推送规则')
    await Promise.all([load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = 0
  }
}

function onInstallUpdated(s: Subscription) {
  const i = subs.value.findIndex((x) => x.id === s.id)
  if (i >= 0) subs.value[i] = s
}
</script>

<template>
  <div v-if="!loaded" class="text-muted-foreground flex items-center gap-2 text-sm">
    <LoaderCircleIcon class="size-4 animate-spin" />加载中…
  </div>
  <div v-else class="grid gap-6">
    <p
      v-if="!source.enabled"
      class="border-warning/30 bg-warning/5 text-warning flex items-center gap-2 rounded-lg border px-3 py-2 text-sm"
    >
      <TriangleAlertIcon class="size-4 shrink-0" />来源已停用：订阅不会读取它，也不会接收回传。
    </p>

    <ol class="grid gap-6">
      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.subscribe)"
          ><CheckIcon v-if="done.subscribe" class="size-4" /><template v-else>1</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div>
            <div class="font-medium">加入订阅</div>
            <p class="text-muted-foreground text-sm">
              选择哪些手机上的订阅读取这个来源。改动后，订阅的 MITM 域名会跟着变。
            </p>
          </div>
          <div v-if="subs.length" class="divide-y rounded-lg border">
            <div v-for="s in subs" :key="s.id" class="flex items-center gap-3 px-3 py-2">
              <BrandIcon :brand="s.client" size="sm" />
              <span class="min-w-0 flex-1 truncate text-sm">{{ s.name }}</span>
              <span v-if="s.all_sources" class="text-muted-foreground text-xs">包含全部来源</span>
              <Switch
                :model-value="included(s)"
                :disabled="s.all_sources || busy === s.id"
                :aria-label="`${s.name} 读取这个来源`"
                @update:model-value="(v: boolean) => toggle(s, v)"
              />
            </div>
          </div>
          <p v-else class="text-muted-foreground text-sm">
            还没有订阅，先到
            <router-link to="/subscriptions" class="text-primary underline-offset-2 hover:underline"
              >订阅</router-link
            >
            页新建一个。
          </p>
        </div>
      </li>

      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.install)"
          ><CheckIcon v-if="done.install" class="size-4" /><template v-else>2</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div>
            <div class="font-medium">在客户端里安装或更新</div>
            <p class="text-muted-foreground text-sm">
              订阅要装在手机上，MITM 域名变了以后还要在客户端里更新一次。
            </p>
          </div>
          <div v-if="pending.length" class="divide-y rounded-lg border">
            <div v-for="s in pending" :key="s.id" class="flex items-center gap-3 px-3 py-2">
              <BrandIcon :brand="s.client" size="sm" />
              <span class="min-w-0 flex-1 truncate text-sm">{{ s.name }}</span>
              <StatusBadge v-if="s.outdated" tone="warning" dot>待更新</StatusBadge>
              <StatusBadge v-else :tone="subscriptionStatus[s.status].tone" dot>{{
                subscriptionStatus[s.status].label
              }}</StatusBadge>
              <Button size="sm" @click="installFor = s"
                ><DownloadIcon /> {{ s.outdated ? '更新' : '安装' }}</Button
              >
            </div>
          </div>
          <p v-else-if="usedBy.length" class="text-success flex items-center gap-1.5 text-sm">
            <CheckIcon class="size-4" />加入的订阅都已装好，是最新的。
          </p>
          <p v-else class="text-muted-foreground text-sm">先完成上一步。</p>
        </div>
      </li>

      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.push)"
          ><CheckIcon v-if="done.push" class="size-4" /><template v-else>3</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div>
            <div class="font-medium">推送到目的地</div>
            <p class="text-muted-foreground text-sm">新值生效后推到哪里，例如青龙。</p>
          </div>
          <div v-if="bindings.length" class="divide-y rounded-lg border">
            <div
              v-for="b in bindings"
              :key="b.id"
              class="flex items-center gap-2 px-3 py-2 text-sm"
            >
              <span class="min-w-0 flex-1 truncate">{{ destName(b.destination_id) }}</span>
              <StatusBadge :tone="b.mode === 'joined' ? 'indigo' : 'sky'">{{
                bindingModes[b.mode]
              }}</StatusBadge>
              <StatusBadge v-if="!b.enabled" tone="neutral" dot>停用</StatusBadge>
            </div>
          </div>
          <div v-if="unbound.length" class="flex flex-wrap items-center gap-2">
            <NativeSelect
              v-model.number="bindForm.destination_id"
              aria-label="目的地"
              class="min-w-40"
            >
              <NativeSelectOption :value="0" disabled>选择目的地</NativeSelectOption>
              <NativeSelectOption v-for="d in unbound" :key="d.id" :value="d.id">{{
                d.name
              }}</NativeSelectOption>
            </NativeSelect>
            <NativeSelect v-model="bindForm.mode" aria-label="写入方式">
              <NativeSelectOption value="per_identity">每个身份一条</NativeSelectOption>
              <NativeSelectOption value="joined">合并为一条</NativeSelectOption>
            </NativeSelect>
            <Button
              variant="outline"
              size="sm"
              :disabled="busy === -1 || !bindForm.destination_id"
              @click="addBinding"
              ><PlusIcon /> 添加推送规则</Button
            >
          </div>
          <p v-else-if="!dests.length" class="text-muted-foreground text-sm">
            还没有目的地，先到
            <router-link to="/destinations" class="text-primary underline-offset-2 hover:underline"
              >推送</router-link
            >
            页新建一个。
          </p>
          <p class="text-muted-foreground text-xs">
            变量改名等设置在
            <router-link
              :to="{ path: '/destinations', query: { tab: 'bindings', source: source.key } }"
              class="text-primary underline-offset-2 hover:underline"
              >推送规则</router-link
            >
            里改。
          </p>
        </div>
      </li>

      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.data)"
          ><CheckIcon v-if="done.data" class="size-4" /><template v-else>4</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div>
            <div class="font-medium">等待第一次回传</div>
            <p class="text-muted-foreground text-sm">
              在手机上打开“{{ source.name }}”对应的 App，触发一次匹配的请求。
            </p>
          </div>
          <div
            class="flex flex-wrap items-center gap-2 rounded-lg border p-3 text-sm"
            :class="done.data ? 'border-success/30 bg-success/5' : ''"
          >
            <template v-if="done.data">
              <CheckIcon class="text-success size-4" />已收到 {{ identities.length }} 个身份 ·
              最近回传 {{ fmtAgo(lastSeen) }}
              <router-link
                :to="`/sources/${source.id}`"
                class="text-primary ml-auto underline-offset-2 hover:underline"
                >查看登录态</router-link
              >
            </template>
            <template v-else-if="batches.length">
              <LoaderCircleIcon class="text-info size-4 animate-spin" />收到回传，正在收集中…
            </template>
            <template v-else>
              <LoaderCircleIcon class="text-muted-foreground size-4 animate-spin" />还没收到回传
            </template>
          </div>
        </div>
      </li>
    </ol>
  </div>

  <SubscriptionInstallDialog
    :subscription="installFor"
    :sources="sources"
    @close="installFor = null"
    @updated="onInstallUpdated"
  />
</template>
