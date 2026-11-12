<script setup lang="ts">
import { ArrowRightIcon, PlusIcon, SendHorizontalIcon } from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import ChoiceCards from '@/components/ChoiceCards.vue'
import EmptyState from '@/components/EmptyState.vue'
import FormField from '@/components/FormField.vue'
import FormSection from '@/components/FormSection.vue'
import FormDialog from '@/components/FormDialog.vue'
import HintText from '@/components/HintText.vue'
import EntityIcon from '@/components/EntityIcon.vue'
import IconTile from '@/components/IconTile.vue'
import ListRow from '@/components/ListRow.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import ToggleChips from '@/components/ToggleChips.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { urlProblem, useFormCheck } from '@/lib/form'
import { channelMark, icons } from '@/lib/icons'
import {
  type Channel,
  type ChannelConfig,
  channelKinds,
  eventTypes,
  type NotifyRule,
  type SourceSummary,
} from '@/types'

const loaded = ref(false)
const channels = ref<Channel[]>([])
const rules = ref<NotifyRule[]>([])
const sources = ref<SourceSummary[]>([])
const busy = ref(false)
// 当前标签页放在地址里（?tab=rules），刷新、分享链接时保留；总览的“下一步”也靠它直接打开通知规则。
const route = useRoute()
const router = useRouter()
const tab = computed({
  get: () => (route.query.tab === 'rules' ? 'rules' : 'channels'),
  set: (v: string) =>
    void router.replace({ query: { ...route.query, tab: v === 'rules' ? 'rules' : undefined } }),
})

const kindTone = { bark: 'amber', telegram: 'sky', webhook: 'sky' } as const
const kindOptions = [
  {
    value: 'bark' as const,
    label: 'Bark',
    description: 'iOS 推送',
    brand: 'bark',
  },
  {
    value: 'telegram' as const,
    label: 'Telegram',
    description: 'Bot API',
    brand: 'telegram',
  },
  {
    value: 'webhook' as const,
    label: 'Webhook',
    description: 'HTTP POST',
    icon: icons.webhook,
  },
]
const eventOptions = eventTypes
const sourceOptions = computed(() => sources.value.map((s) => ({ value: s.key, label: s.name })))
const channelOptions = computed(() => channels.value.map((c) => ({ value: c.id, label: c.name })))
const eventLabel = (e: string) => eventTypes.find((x) => x.value === e)?.label ?? e
const eventTone = (e: string) => eventTypes.find((x) => x.value === e)?.tone ?? 'neutral'
const channelName = (id: number) => channels.value.find((c) => c.id === id)?.name ?? `#${id}`
// 规则里可能还留着已删除渠道的 id，显示和保存时只认现有的渠道。
const liveChannels = (ids: number[]) => ids.filter((id) => channels.value.some((c) => c.id === id))
const sourceName = (k: string) => sources.value.find((s) => s.key === k)?.name ?? k
// 引用这个渠道的通知规则数；为 0 时这个渠道不会发出任何通知，和目的地的推送规则数同一种写法。
const rulesOf = (c: Channel) => rules.value.filter((r) => r.channel_ids.includes(c.id)).length

async function load() {
  try {
    const [c, r, s] = await Promise.all([
      api.get<Channel[]>('/channels'),
      api.get<NotifyRule[]>('/notify-rules'),
      api.get<SourceSummary[]>('/sources'),
    ])
    channels.value = c.data
    rules.value = r.data
    sources.value = s.data
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loaded.value = true
  }
}
onMounted(load)

// 通知规则要先有渠道：切到渠道标签页，打开新建表单。
function startChannel() {
  tab.value = 'channels'
  newChannel()
}

function channelSummary(c: Channel) {
  if (c.kind === 'bark') return c.config.server || 'https://api.day.app'
  if (c.kind === 'telegram') return `chat ${c.config.chat_id}`
  return c.config.url
}

// ---- 通知渠道 ----
const chForm = reactive({
  open: false,
  id: 0,
  name: '',
  kind: 'bark' as Channel['kind'],
  enabled: true,
  config: {} as ChannelConfig,
  secrets: {} as Record<string, boolean>,
})

// 选填的地址填了才校验格式；密钥编辑时不回显，留空沿用旧值。
const optionalURL = (v: string | undefined, what: string) => (v?.trim() ? urlProblem(v, what) : '')
const chCheck = useFormCheck(() => {
  const c = chForm.config
  const kept = (k: string) => !!chForm.secrets[k]
  const conn =
    chForm.kind === 'bark'
      ? {
          url: optionalURL(c.url, '推送 URL'),
          server: optionalURL(c.server, '服务器地址'),
          device_key:
            !c.url?.trim() &&
            !c.device_key?.trim() &&
            !kept('device_key') &&
            '请填写 Device Key，或填写完整的推送 URL',
        }
      : chForm.kind === 'telegram'
        ? {
            bot_token: !c.bot_token?.trim() && !kept('bot_token') && '请填写 Bot Token',
            chat_id: !c.chat_id?.trim() && '请填写 Chat ID',
            api_base: optionalURL(c.api_base, 'API Base'),
          }
        : { url: urlProblem(c.url, 'URL') }
  return { name: !chForm.name.trim() && '请填写名称', ...conn }
})

function newChannel() {
  chCheck.reset()
  Object.assign(chForm, {
    open: true,
    id: 0,
    name: '',
    kind: 'bark',
    enabled: true,
    config: {},
    secrets: {},
  })
}

function editChannel(c: Channel) {
  chCheck.reset()
  Object.assign(chForm, {
    open: true,
    id: c.id,
    name: c.name,
    kind: c.kind,
    enabled: c.enabled,
    config: { ...c.config },
    secrets: c.secrets_set,
  })
}

async function saveChannel() {
  if (!chCheck.ok()) return
  busy.value = true
  try {
    const c = chForm.config
    const blank = (v?: string) => (v && v.trim() ? v.trim() : undefined)
    let config: ChannelConfig
    if (chForm.kind === 'bark') {
      config = { url: blank(c.url), server: blank(c.server), device_key: blank(c.device_key) }
    } else if (chForm.kind === 'telegram') {
      config = {
        bot_token: blank(c.bot_token),
        chat_id: blank(c.chat_id),
        api_base: blank(c.api_base),
      }
    } else {
      config = { url: blank(c.url), secret: blank(c.secret) }
    }
    const body = { name: chForm.name, kind: chForm.kind, enabled: chForm.enabled, config }
    if (chForm.id) await api.put(`/channels/${chForm.id}`, body)
    else await api.post('/channels', body)
    toast.success('已保存')
    chForm.open = false
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

const testing = ref(0)
async function testChannel(c: Channel) {
  testing.value = c.id
  try {
    const r = (await api.post<{ ok: boolean; error?: string }>(`/channels/${c.id}/test`, {})).data
    if (r.ok) toast.success(`测试通知已发送`)
    else toast.error(`发送失败：${r.error}`)
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    testing.value = 0
  }
}

// 列表里的启用开关：切换即保存。密钥不回显，留空由后端沿用旧值。
const toggling = ref('')
async function toggleChannel(c: Channel, enabled: boolean) {
  toggling.value = `c${c.id}`
  try {
    await api.put(`/channels/${c.id}`, { name: c.name, kind: c.kind, enabled, config: c.config })
    c.enabled = enabled
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = ''
  }
}

async function removeChannel(c: Channel) {
  try {
    await api.delete(`/channels/${c.id}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(errorText(e))
  }
}

// ---- 通知规则 ----
const ruleForm = reactive({
  open: false,
  id: 0,
  name: '',
  events: [] as string[],
  source_keys: [] as string[],
  channel_ids: [] as number[],
  enabled: true,
})

const noChannels = '暂无通知渠道，请在“通知渠道”标签页新建'
const ruleCheck = useFormCheck(() => ({
  name: !ruleForm.name.trim() && '请填写名称',
  events: !ruleForm.events.length && '请至少选择一个事件',
  channel_ids:
    !ruleForm.channel_ids.length && (channels.value.length ? '请至少选择一个通知渠道' : noChannels),
}))

function newRule() {
  ruleCheck.reset()
  Object.assign(ruleForm, {
    open: true,
    id: 0,
    name: '',
    events: ['push.failed', 'value.expiring', 'subscription.mitm_changed'],
    source_keys: [],
    channel_ids: channels.value.map((c) => c.id).slice(0, 1),
    enabled: true,
  })
}

function editRule(r: NotifyRule) {
  ruleCheck.reset()
  Object.assign(ruleForm, {
    open: true,
    ...r,
    events: [...r.events],
    source_keys: [...r.source_keys],
    channel_ids: liveChannels(r.channel_ids),
  })
}

async function saveRule() {
  if (!ruleCheck.ok()) return
  busy.value = true
  try {
    const body = {
      name: ruleForm.name,
      events: ruleForm.events,
      source_keys: ruleForm.source_keys,
      channel_ids: ruleForm.channel_ids,
      enabled: ruleForm.enabled,
    }
    if (ruleForm.id) await api.put(`/notify-rules/${ruleForm.id}`, body)
    else await api.post('/notify-rules', body)
    toast.success('已保存')
    ruleForm.open = false
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function toggleRule(r: NotifyRule, enabled: boolean) {
  toggling.value = `r${r.id}`
  try {
    const { name, events, source_keys, channel_ids } = r
    await api.put(`/notify-rules/${r.id}`, {
      name,
      events,
      source_keys,
      channel_ids: liveChannels(channel_ids),
      enabled,
    })
    r.enabled = enabled
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = ''
  }
}

async function removeRule(r: NotifyRule) {
  try {
    await api.delete(`/notify-rules/${r.id}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(errorText(e))
  }
}

function secretHint(key: string, fallback = '') {
  return chForm.secrets[key] ? '已设置，留空不修改' : fallback
}
</script>

<template>
  <!-- 当前标签页的主操作放在标题行右侧（与订阅页一致），手机上换到标题下方并保留文字 -->
  <PageHeader>
    <template #actions>
      <Button v-if="tab === 'channels'" @click="newChannel"><PlusIcon /> 新建通知渠道</Button>
      <Button v-else @click="newRule"><PlusIcon /> 新建通知规则</Button>
    </template>
  </PageHeader>

  <Tabs v-model="tab">
    <div class="mb-2 flex">
      <TabsList class="no-scrollbar max-w-full justify-start scroll-px-0.75 overflow-x-auto">
        <TabsTrigger value="channels"
          ><component :is="icons.channel" /> 通知渠道
          <span
            class="bg-muted-foreground/15 text-muted-foreground min-w-5 rounded-full px-1.5 text-xs tabular-nums"
            >{{ channels.length }}</span
          ></TabsTrigger
        >
        <TabsTrigger value="rules"
          ><component :is="icons.notifyRules" /> 通知规则
          <span
            class="bg-muted-foreground/15 text-muted-foreground min-w-5 rounded-full px-1.5 text-xs tabular-nums"
            >{{ rules.length }}</span
          ></TabsTrigger
        >
      </TabsList>
    </div>

    <TabsContent value="channels" class="grid gap-3">
      <HintText>通知内容仅包含来源、身份与字段名，不包含字段值。</HintText>
      <ListSkeleton v-if="!loaded" :rows="2" />
      <EmptyState
        v-else-if="!channels.length"
        :icon="icons.channel"
        title="暂无通知渠道"
        description="推送失败或登录态即将过期时，Ferry 通过通知渠道发送提醒，支持 Bark、Telegram 与 Webhook。"
      >
        <Button @click="newChannel"><PlusIcon /> 新建通知渠道</Button>
      </EmptyState>
      <ListRow v-for="c in channels" :key="c.id" :dim="!c.enabled">
        <template #icon>
          <EntityIcon :mark="channelMark(c.kind)" />
        </template>
        <template #title>{{ c.name }}</template>
        <template #badges>
          <StatusBadge v-if="!c.enabled" dot>已停用</StatusBadge>
          <StatusBadge :tone="kindTone[c.kind]">{{ channelKinds[c.kind] }}</StatusBadge>
          <StatusBadge v-if="rulesOf(c)" tone="neutral">{{ rulesOf(c) }} 条通知规则</StatusBadge>
          <StatusBadge v-else tone="warning">未关联通知规则</StatusBadge>
        </template>
        <template #description>
          <span class="font-mono break-all">{{ channelSummary(c) }}</span>
        </template>
        <template #actions>
          <RowActions
            :name="c.name"
            :enabled="c.enabled"
            :toggling="toggling === `c${c.id}`"
            delete-title="删除通知渠道？"
            delete-description="该渠道将同时从引用该渠道的通知规则中移除；移除后无可用渠道的通知规则将停用。"
            @toggle="(v) => toggleChannel(c, v)"
            @edit="editChannel(c)"
            @remove="removeChannel(c)"
          >
            <Button
              variant="outline"
              size="sm"
              :disabled="testing === c.id"
              @click="testChannel(c)"
            >
              <SendHorizontalIcon /> 发送测试
            </Button>
          </RowActions>
        </template>
      </ListRow>
    </TabsContent>

    <TabsContent value="rules" class="grid gap-3">
      <HintText>通知规则 = 事件 + 来源 → 通知渠道；未选择来源时适用于全部来源。</HintText>
      <ListSkeleton v-if="!loaded" :rows="2" />
      <EmptyState
        v-else-if="!rules.length"
        :icon="icons.notifyRules"
        title="暂无通知规则"
        description="通知规则定义各类事件发送至哪些通知渠道；未配置规则时不发送任何通知。"
      >
        <Button v-if="channels.length" @click="newRule"><PlusIcon /> 新建通知规则</Button>
        <Button v-else @click="startChannel"><PlusIcon /> 新建通知渠道</Button>
      </EmptyState>
      <ListRow v-for="r in rules" :key="r.id" :dim="!r.enabled">
        <template #icon><IconTile :icon="icons.notifyRules" /></template>
        <template #title>{{ r.name }}</template>
        <template v-if="!r.enabled" #badges>
          <StatusBadge dot>已停用</StatusBadge>
        </template>
        <template #description>
          <span class="flex w-full flex-wrap gap-1">
            <StatusBadge v-for="e in r.events" :key="e" :tone="eventTone(e)" dot>{{
              eventLabel(e)
            }}</StatusBadge>
          </span>
          <span class="flex w-full flex-wrap items-center gap-1.5">
            <component :is="icons.source" class="size-3.5" />
            {{ r.source_keys.length ? r.source_keys.map(sourceName).join('、') : '全部来源' }}
            <ArrowRightIcon class="size-3.5" />
            <component :is="icons.channel" class="size-3.5" />
            {{ liveChannels(r.channel_ids).map(channelName).join('、') || '无可用渠道' }}
          </span>
        </template>
        <template #actions>
          <RowActions
            :name="r.name"
            :enabled="r.enabled"
            :toggling="toggling === `r${r.id}`"
            delete-title="删除通知规则？"
            @toggle="(v) => toggleRule(r, v)"
            @edit="editRule(r)"
            @remove="removeRule(r)"
          />
        </template>
      </ListRow>
    </TabsContent>
  </Tabs>

  <FormDialog
    v-model:open="chForm.open"
    v-model:enabled="chForm.enabled"
    :title="chForm.id ? '编辑通知渠道' : '新建通知渠道'"
    :description="chForm.id ? chForm.name : '事件通知的投递目标'"
    :brand="chForm.kind !== 'webhook' ? chForm.kind : undefined"
    :icon="icons.webhook"
    :busy="busy"
    @submit="saveChannel"
  >
    <FormSection title="基本" :cols="1">
      <FormField label="类型" :hint="chForm.id ? '创建后不可修改' : ''">
        <ChoiceCards
          v-model="chForm.kind"
          :options="kindOptions"
          :disabled="!!chForm.id"
          label="类型"
        />
      </FormField>
      <FormField label="名称" for="c-name" :error="chCheck.errors.name">
        <Input id="c-name" v-model="chForm.name" placeholder="如 Bark-iPhone" />
      </FormField>
    </FormSection>

    <FormSection v-if="chForm.kind === 'bark'" title="连接">
      <FormField
        label="推送 URL"
        for="c-bark-url"
        full
        hint="完整的推送地址，将自动解析服务器与 Key（支持子路径）；与下方两项任选其一"
        :error="chCheck.errors.url"
      >
        <Input
          id="c-bark-url"
          v-model="chForm.config.url"
          type="password"
          autocomplete="off"
          class="font-mono"
          :placeholder="secretHint('device_key', 'https://api.day.app/<key>')"
        />
      </FormField>
      <FormField label="服务器" for="c-bark-server" :error="chCheck.errors.server">
        <Input
          id="c-bark-server"
          v-model="chForm.config.server"
          class="font-mono"
          placeholder="https://api.day.app"
        />
      </FormField>
      <FormField label="Device Key" for="c-bark-key" :error="chCheck.errors.device_key">
        <Input
          id="c-bark-key"
          v-model="chForm.config.device_key"
          type="password"
          autocomplete="off"
          :placeholder="secretHint('device_key')"
        />
      </FormField>
    </FormSection>

    <FormSection v-else-if="chForm.kind === 'telegram'" title="连接">
      <FormField label="Bot Token" for="c-tg-token" full :error="chCheck.errors.bot_token">
        <Input
          id="c-tg-token"
          v-model="chForm.config.bot_token"
          type="password"
          autocomplete="off"
          :placeholder="secretHint('bot_token')"
        />
      </FormField>
      <FormField label="Chat ID" for="c-tg-chat" :error="chCheck.errors.chat_id">
        <Input id="c-tg-chat" v-model="chForm.config.chat_id" class="font-mono" />
      </FormField>
      <FormField
        label="API Base"
        for="c-tg-base"
        optional
        hint="反向代理地址"
        :error="chCheck.errors.api_base"
      >
        <Input
          id="c-tg-base"
          v-model="chForm.config.api_base"
          class="font-mono"
          placeholder="https://api.telegram.org"
        />
      </FormField>
    </FormSection>

    <FormSection v-else title="连接" :cols="1">
      <FormField label="URL" for="c-wh-url" :error="chCheck.errors.url">
        <Input
          id="c-wh-url"
          v-model="chForm.config.url"
          class="font-mono"
          placeholder="https://host/path"
        />
      </FormField>
      <FormField
        label="HMAC 密钥"
        for="c-wh-secret"
        optional
        hint="设置后请求头携带 X-Ferry-Signature（HMAC-SHA256）"
      >
        <Input
          id="c-wh-secret"
          v-model="chForm.config.secret"
          type="password"
          autocomplete="off"
          :placeholder="secretHint('secret')"
        />
      </FormField>
    </FormSection>
  </FormDialog>

  <FormDialog
    v-model:open="ruleForm.open"
    v-model:enabled="ruleForm.enabled"
    :title="ruleForm.id ? '编辑通知规则' : '新建通知规则'"
    :description="ruleForm.id ? ruleForm.name : '事件 + 来源 → 通知渠道'"
    :icon="icons.notifyRules"
    :busy="busy"
    @submit="saveRule"
  >
    <FormField label="名称" for="r-name" :error="ruleCheck.errors.name">
      <Input id="r-name" v-model="ruleForm.name" placeholder="如 异常告警" />
    </FormField>
    <FormSection title="事件" :description="`已选 ${ruleForm.events.length} 项`" :cols="1">
      <FormField :error="ruleCheck.errors.events">
        <ToggleChips v-model="ruleForm.events" :options="eventOptions" label="事件" />
      </FormField>
    </FormSection>
    <FormSection
      title="来源"
      :description="
        ruleForm.source_keys.length
          ? `已选 ${ruleForm.source_keys.length} 项`
          : '未选择时适用于全部来源'
      "
      :cols="1"
    >
      <ToggleChips v-model="ruleForm.source_keys" :options="sourceOptions" label="来源" />
    </FormSection>
    <FormSection title="通知渠道" :description="`已选 ${ruleForm.channel_ids.length} 项`" :cols="1">
      <FormField :hint="channels.length ? '' : noChannels" :error="ruleCheck.errors.channel_ids">
        <ToggleChips v-model="ruleForm.channel_ids" :options="channelOptions" label="渠道" />
      </FormField>
    </FormSection>
  </FormDialog>
</template>
