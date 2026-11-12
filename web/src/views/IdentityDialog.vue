<script setup lang="ts">
import {
  CheckIcon,
  ClockIcon,
  CopyIcon,
  DatabaseIcon,
  EyeIcon,
  EyeOffIcon,
  HistoryIcon,
  KeyRoundIcon,
  LockIcon,
  SendIcon,
  ShieldAlertIcon,
  Undo2Icon,
  WebhookIcon,
  XIcon,
} from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import BrandIcon from '@/components/BrandIcon.vue'
import ConfirmButton from '@/components/ConfirmButton.vue'
import IconTile from '@/components/IconTile.vue'
import DialogShell from '@/components/DialogShell.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import SectionHeading from '@/components/SectionHeading.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { copyText, expiryState, fmtAgo, fmtTime, secondaryKey } from '@/format'
import { type Tone, toneDot, toneText } from '@/lib/tone'
import {
  type Identity,
  type IdentityPush,
  type PushJob,
  pushStateOf,
  pushStates,
  type Version,
  versionStatus,
} from '@/types'

// 登录态的详情，是列表页上的弹窗（和来源编辑器一样），地址 /data/:id 可直接打开。
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ close: []; changed: [] }>()

const identity = ref<Identity | null>(null)
const versions = ref<Version[]>([])
const field = ref('')
const more = ref(false)
const revealed = reactive<Record<number, string>>({})
const busy = ref(false)

const fieldName = (key: string) => identity.value?.fields?.find((f) => f.key === key)?.name || key
const pendingTotal = computed(() =>
  (identity.value?.fields ?? []).reduce((n, f) => n + f.pending, 0),
)

async function loadIdentity() {
  identity.value = (await api.get<Identity>(`/identities/${props.id}`)).data
}

const pageSize = 100

async function loadVersions(append = false) {
  const params: Record<string, string | number> = {}
  if (field.value) params.field = field.value
  if (append && versions.value.length) params.before = versions.value[versions.value.length - 1].id
  const page = (await api.get<Version[]>(`/identities/${props.id}/versions`, { params })).data
  versions.value = append ? [...versions.value, ...page] : page
  more.value = page.length === pageSize
}

// 推送：这个来源的每条推送规则，以及推给这个身份的最近一次任务。
const pushes = ref<IdentityPush[]>([])
async function loadPushes() {
  pushes.value = (await api.get<IdentityPush[]>(`/identities/${props.id}/pushes`)).data
}

const pushing = ref(0)
async function pushNow(p: IdentityPush) {
  pushing.value = p.binding_id
  try {
    const job = (
      await api.post<PushJob>('/pushes', {
        binding_id: p.binding_id,
        identity_id: p.mode === 'per_identity' ? Number(props.id) : null,
      })
    ).data
    const state = pushStateOf(job)
    if (state === 'success') toast.success('推送成功')
    else if (state === 'failed' || state === 'retrying')
      toast.error(job.last_error ? `推送失败：${job.last_error}` : '推送失败')
    else toast.info(`推送状态：${pushStates[state].label}`)
    await loadPushes()
    emit('changed')
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    pushing.value = 0
  }
}

async function refresh() {
  try {
    await Promise.all([loadIdentity(), loadVersions(), loadPushes()])
  } catch (e) {
    toast.error(errorText(e))
  }
}
onMounted(refresh)

async function pickField(v: string | number) {
  field.value = String(v)
  try {
    await loadVersions()
  } catch (e) {
    toast.error(errorText(e))
  }
}

// 原值只在点了“显示”之后才向后端要，后端会写一条审计。
async function toggleReveal(v: Version) {
  if (revealed[v.id] !== undefined) {
    delete revealed[v.id]
    return
  }
  try {
    revealed[v.id] = (await api.post<{ value: string }>(`/versions/${v.id}/reveal`, {})).data.value
  } catch (e) {
    toast.error(errorText(e))
  }
}

async function copyValue(v: Version) {
  try {
    const val = v.masked
      ? (revealed[v.id] ??
        (await api.post<{ value: string }>(`/versions/${v.id}/reveal`, {})).data.value)
      : v.value
    await copyText(val)
    toast.success('已复制原值')
  } catch (e) {
    toast.error(errorText(e))
  }
}

async function act(v: Version, action: 'approve' | 'reject' | 'activate', done: string) {
  busy.value = true
  try {
    await api.post(`/versions/${v.id}/${action}`, {})
    toast.success(done)
    await refresh()
    emit('changed')
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

// 过期提示：已过期标红，快过期的标黄（时长见 expiringWithinMs）。
function expiry(v: Version): { text: string; tone: Tone } | null {
  if (!v.expires_at) return null
  const state = expiryState(v.expires_at)
  if (state === 'expired') return { text: `已过期 · ${fmtTime(v.expires_at)}`, tone: 'danger' }
  if (state === 'expiring') return { text: `即将过期 · ${fmtTime(v.expires_at)}`, tone: 'warning' }
  return { text: `到期 ${fmtTime(v.expires_at)}`, tone: 'neutral' }
}

function shown(v: Version) {
  return revealed[v.id] ?? v.value
}
</script>

<template>
  <DialogShell
    :open="true"
    :title="identity?.source_name ?? '登录态'"
    :icon="DatabaseIcon"
    tone="sky"
    size="xl"
    @close="emit('close')"
  >
    <template #badges>
      <span v-if="identity" class="text-muted-foreground font-mono text-sm font-normal">{{
        identity.key === 'default' ? '默认' : identity.key
      }}</span>
      <StatusBadge v-if="pendingTotal" tone="warning" :icon="ShieldAlertIcon"
        >{{ pendingTotal }} 个值待审核</StatusBadge
      >
    </template>
    <template #description>
      <template v-if="identity"
        >最近回传 {{ fmtAgo(identity.last_seen_at) }}（{{
          fmtTime(identity.last_seen_at)
        }}）</template
      >
    </template>
    <template v-if="identity">
      <SectionHeading title="当前值" :icon="KeyRoundIcon" />
      <div class="mb-8 grid gap-3 md:grid-cols-2">
        <Card
          v-for="f in identity.fields"
          :key="f.key"
          size="sm"
          :class="f.current ? '' : 'border border-dashed ring-0'"
        >
          <CardHeader class="flex flex-wrap items-center gap-2">
            <IconTile :icon="KeyRoundIcon" :tone="f.current ? 'sky' : 'neutral'" size="sm" />
            <CardTitle>{{ f.name || f.key }}</CardTitle>
            <span
              v-if="secondaryKey(f.name, f.key)"
              class="text-muted-foreground font-mono text-xs"
              >{{ f.key }}</span
            >
            <StatusBadge v-if="f.sensitive" tone="warning" :icon="LockIcon">敏感</StatusBadge>
            <StatusBadge v-if="f.pending" tone="warning" dot class="ml-auto"
              >{{ f.pending }} 个待审核</StatusBadge
            >
          </CardHeader>
          <CardContent class="grid gap-2 text-sm">
            <template v-if="f.current">
              <div class="flex items-start gap-1">
                <code
                  class="bg-muted/70 flex-1 rounded-md border px-2 py-1.5 font-mono text-xs break-all"
                  >{{ shown(f.current) }}</code
                >
                <Button
                  v-if="f.current.masked"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="revealed[f.current.id] !== undefined ? '隐藏原值' : '显示原值'"
                  @click="toggleReveal(f.current)"
                >
                  <EyeOffIcon v-if="revealed[f.current.id] !== undefined" /><EyeIcon v-else />
                </Button>
              </div>
              <div class="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
                <span class="flex items-center gap-1"
                  ><ClockIcon class="size-3.5" />生效于 {{ fmtTime(f.current.activated_at) }}</span
                >
                <StatusBadge v-if="expiry(f.current)" :tone="expiry(f.current)!.tone">{{
                  expiry(f.current)!.text
                }}</StatusBadge>
              </div>
            </template>
            <span v-else class="text-muted-foreground">暂无生效值</span>
          </CardContent>
        </Card>
      </div>

      <SectionHeading title="推送" :icon="SendIcon">
        <template #actions>
          <router-link
            :to="{ path: '/destinations', query: { tab: 'bindings', source: identity.source_key } }"
            class="text-primary text-sm underline-offset-2 hover:underline"
            >管理推送规则</router-link
          >
        </template>
      </SectionHeading>
      <div v-if="pushes.length" class="mb-8 divide-y rounded-xl border">
        <div
          v-for="p in pushes"
          :key="p.binding_id"
          class="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3"
        >
          <BrandIcon v-if="p.destination_kind === 'qinglong'" brand="qinglong" size="sm" />
          <IconTile v-else :icon="WebhookIcon" tone="sky" size="sm" />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2 text-sm font-medium">
              {{ p.destination_name }}
              <StatusBadge v-if="!p.enabled" tone="neutral" dot>已停用</StatusBadge>
              <StatusBadge v-else-if="p.last" :tone="pushStates[pushStateOf(p.last)].tone" dot>{{
                pushStates[pushStateOf(p.last)].label
              }}</StatusBadge>
              <StatusBadge v-else tone="neutral" dot>未推送</StatusBadge>
            </div>
            <div class="text-muted-foreground text-xs">
              {{ p.mode === 'joined' ? '合并为一条' : '每个身份一条' }}
              <template v-if="p.last">
                · {{ fmtAgo(p.last.finished_at ?? p.last.created_at) }}</template
              >
            </div>
            <p
              v-if="p.last?.last_error"
              class="mt-1 text-xs break-all"
              :class="
                pushStateOf(p.last) === 'skipped' ? 'text-muted-foreground' : 'text-destructive'
              "
            >
              {{ p.last.last_error }}
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            :disabled="!p.enabled || pushing === p.binding_id"
            @click="pushNow(p)"
            ><SendIcon /> 手动推送</Button
          >
        </div>
      </div>
      <p v-else class="text-muted-foreground mb-8 text-sm">
        该来源暂无推送规则，可在
        <router-link
          :to="{ path: `/sources/${identity.source_id}`, query: { section: 'setup' } }"
          class="text-primary underline-offset-2 hover:underline"
          >接入清单</router-link
        >
        中添加。
      </p>

      <SectionHeading title="历史值" :icon="HistoryIcon">
        <template #actions>
          <Tabs :model-value="field" @update:model-value="pickField">
            <TabsList class="no-scrollbar max-w-full overflow-x-auto">
              <TabsTrigger value="">全部字段</TabsTrigger>
              <TabsTrigger v-for="f in identity.fields" :key="f.key" :value="f.key">{{
                f.name || f.key
              }}</TabsTrigger>
            </TabsList>
          </Tabs>
        </template>
      </SectionHeading>

      <ol class="relative ml-1.5 grid gap-3 border-l-2 pl-6">
        <li v-for="v in versions" :key="v.id" class="relative">
          <span
            class="ring-background absolute top-4 -left-[1.95rem] size-3 rounded-full ring-4"
            :class="toneDot[v.current ? 'success' : (versionStatus[v.status]?.tone ?? 'neutral')]"
          />
          <Card size="sm" :class="v.current ? 'ring-primary/40 ring-2' : ''">
            <CardContent class="grid gap-2 text-sm">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-medium">{{ fieldName(v.field) }}</span>
                <StatusBadge v-if="v.current" tone="success" :icon="CheckIcon">生效中</StatusBadge>
                <StatusBadge v-else :tone="versionStatus[v.status]?.tone ?? 'neutral'" dot>{{
                  versionStatus[v.status]?.label ?? v.status
                }}</StatusBadge>
                <span class="text-muted-foreground ml-auto text-xs" :title="fmtTime(v.created_at)">
                  {{ fmtAgo(v.created_at) }} · 回传 {{ v.seen_count }} 次
                </span>
              </div>
              <div class="flex items-start gap-1">
                <code
                  class="bg-muted/70 flex-1 rounded-md border px-2 py-1.5 font-mono text-xs break-all"
                  >{{ shown(v) }}</code
                >
                <Button
                  v-if="v.masked"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="revealed[v.id] !== undefined ? '隐藏原值' : '显示原值'"
                  @click="toggleReveal(v)"
                >
                  <EyeOffIcon v-if="revealed[v.id] !== undefined" /><EyeIcon v-else />
                </Button>
              </div>
              <p
                v-if="v.reason"
                class="flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs"
                :class="
                  v.status === 'rejected'
                    ? 'border-destructive/25 bg-destructive/5 text-destructive'
                    : 'border-warning/30 bg-warning/5 text-warning'
                "
              >
                <ShieldAlertIcon class="size-3.5 shrink-0" />{{ v.reason }}
              </p>
              <div
                class="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs"
              >
                <span>最近回传 {{ fmtTime(v.last_seen_at) }}</span>
                <span v-if="v.activated_at">生效于 {{ fmtTime(v.activated_at) }}</span>
                <span v-if="expiry(v)" :class="toneText[expiry(v)!.tone]">{{
                  expiry(v)!.text
                }}</span>
                <div class="ml-auto flex gap-1">
                  <Button variant="ghost" size="xs" @click="copyValue(v)"><CopyIcon /> 复制</Button>
                  <template v-if="v.status === 'pending_review'">
                    <Button size="xs" :disabled="busy" @click="act(v, 'approve', '已通过')">
                      <CheckIcon /> 通过
                    </Button>
                    <Button
                      variant="destructive"
                      size="xs"
                      :disabled="busy"
                      @click="act(v, 'reject', '已驳回')"
                    >
                      <XIcon /> 驳回
                    </Button>
                  </template>
                  <ConfirmButton
                    v-else-if="v.status === 'valid' && !v.current"
                    title="恢复此值？"
                    description="设为当前值，并按推送规则重新推送。"
                    confirm-text="恢复"
                    :danger="false"
                    variant="outline"
                    size="xs"
                    @confirm="act(v, 'activate', '已恢复')"
                  >
                    <Undo2Icon /> 恢复
                  </ConfirmButton>
                </div>
              </div>
            </CardContent>
          </Card>
        </li>
        <li v-if="!versions.length" class="text-muted-foreground text-sm">暂无历史值</li>
      </ol>
      <div v-if="more" class="mt-3">
        <Button variant="outline" @click="loadVersions(true)">加载更多</Button>
      </div>
    </template>
    <ListSkeleton v-else :rows="2" class="md:grid-cols-2" item-class="h-32" />
    <template #footer>
      <Button variant="outline" @click="emit('close')">关闭</Button>
    </template>
  </DialogShell>
</template>
