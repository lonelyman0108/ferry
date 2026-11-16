<script setup lang="ts">
import {
  AlertTriangleIcon,
  ArrowRightIcon,
  GitCompareArrowsIcon,
  LoaderCircleIcon,
  ShieldAlertIcon,
} from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import DialogShell from '@/components/DialogShell.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import type {
  FieldDoc,
  MatcherDoc,
  OutputDoc,
  ProbeSpec,
  Repo,
  RepoItem,
  SourceImportApplyOutput,
} from '@/types'

const props = defineProps<{
  open: boolean
  repo: Repo | null
  item: RepoItem | null
}>()

const emit = defineEmits<{
  close: []
  updated: []
}>()

const acknowledge = ref(false)
const busy = ref(false)

watch(
  () => [props.open, props.item?.key],
  () => {
    acknowledge.value = false
  },
)

type DiffKind = 'added' | 'removed' | 'changed'

interface DiffRow {
  kind: DiffKind
  title: string
  before?: string
  after?: string
}

function stableJSON(v: unknown): string {
  return JSON.stringify(v ?? null)
}

function fmtMatcher(m: MatcherDoc): string {
  const parts = [m.method || '任意方法', m.host]
  if (m.path) parts.push(m.path)
  return parts.join(' · ')
}

function fmtField(f: FieldDoc): string {
  const parts: string[] = [
    `${f.in}: ${f.path}`,
    `类型 ${f.type || 'text'}`,
    f.sensitive ? '敏感' : '普通',
  ]
  if (f.matcher) parts.push(`规则 ${f.matcher}`)
  if (f.regex_on) parts.push(`正则 ${f.regex_on}`)
  if (f.min_len) parts.push(`最短 ${f.min_len}`)
  if (f.max_len) parts.push(`最长 ${f.max_len}`)
  if (f.pattern) parts.push(`格式 ${f.pattern}`)
  return parts.join(' · ')
}

function fmtOutput(o: OutputDoc): string {
  return o.template
}

function fmtProbe(p?: ProbeSpec): string {
  if (!p) return '未配置'
  const parts = [
    `${p.method || 'GET'} ${p.url}`,
    `期望：${p.expect}`,
    p.identity ? `身份：${p.identity}` : '',
    p.body ? `正文：${p.body}` : '',
    p.headers && Object.keys(p.headers).length ? `请求头：${JSON.stringify(p.headers)}` : '',
    p.timeout ? `超时：${p.timeout}s` : '',
  ].filter(Boolean)
  return parts.join('\n')
}

const sections = computed(() => {
  const remote = props.item?.source
  const local = props.item?.local_source
  if (!remote || !local) return []

  const matcherDiffs: DiffRow[] = []
  const oldM = new Map((local.matchers ?? []).map((m) => [m.name, m]))
  const newM = new Map((remote.matchers ?? []).map((m) => [m.name, m]))
  for (const [name, nm] of newM) {
    const om = oldM.get(name)
    if (!om) {
      matcherDiffs.push({ kind: 'added', title: name, after: fmtMatcher(nm) })
    } else if (stableJSON(om) !== stableJSON(nm)) {
      matcherDiffs.push({
        kind: 'changed',
        title: name,
        before: fmtMatcher(om),
        after: fmtMatcher(nm),
      })
    }
  }
  for (const [name, om] of oldM) {
    if (!newM.has(name)) {
      matcherDiffs.push({ kind: 'removed', title: name, before: fmtMatcher(om) })
    }
  }

  const fieldDiffs: DiffRow[] = []
  const oldF = new Map((local.fields ?? []).map((f) => [f.key, f]))
  const newF = new Map((remote.fields ?? []).map((f) => [f.key, f]))
  for (const [key, nf] of newF) {
    const of = oldF.get(key)
    if (!of) {
      fieldDiffs.push({
        kind: 'added',
        title: nf.name ? `${nf.name} (${key})` : key,
        after: fmtField(nf),
      })
    } else if (stableJSON(of) !== stableJSON(nf)) {
      fieldDiffs.push({
        kind: 'changed',
        title: nf.name ? `${nf.name} (${key})` : key,
        before: fmtField(of),
        after: fmtField(nf),
      })
    }
  }
  for (const [key, of] of oldF) {
    if (!newF.has(key)) {
      fieldDiffs.push({
        kind: 'removed',
        title: of.name ? `${of.name} (${key})` : key,
        before: fmtField(of),
      })
    }
  }

  const outputDiffs: DiffRow[] = []
  const oldO = new Map((local.outputs ?? []).map((o) => [o.name, o]))
  const newO = new Map((remote.outputs ?? []).map((o) => [o.name, o]))
  for (const [name, no] of newO) {
    const oo = oldO.get(name)
    if (!oo) {
      outputDiffs.push({ kind: 'added', title: name, after: fmtOutput(no) })
    } else if (stableJSON(oo) !== stableJSON(no)) {
      outputDiffs.push({
        kind: 'changed',
        title: name,
        before: fmtOutput(oo),
        after: fmtOutput(no),
      })
    }
  }
  for (const [name, oo] of oldO) {
    if (!newO.has(name)) {
      outputDiffs.push({ kind: 'removed', title: name, before: fmtOutput(oo) })
    }
  }

  const probeDiffs: DiffRow[] = []
  if (stableJSON(local.probe) !== stableJSON(remote.probe)) {
    if (!local.probe && remote.probe) {
      probeDiffs.push({ kind: 'added', title: '主动探测', after: fmtProbe(remote.probe) })
    } else if (local.probe && !remote.probe) {
      probeDiffs.push({ kind: 'removed', title: '主动探测', before: fmtProbe(local.probe) })
    } else {
      probeDiffs.push({
        kind: 'changed',
        title: '主动探测',
        before: fmtProbe(local.probe),
        after: fmtProbe(remote.probe),
      })
    }
  }

  const basicDiffs: DiffRow[] = []
  if ((local.name ?? '') !== (remote.name ?? '')) {
    basicDiffs.push({ kind: 'changed', title: '名称', before: local.name, after: remote.name })
  }
  if ((local.description ?? '') !== (remote.description ?? '')) {
    basicDiffs.push({
      kind: 'changed',
      title: '说明',
      before: local.description || '—',
      after: remote.description || '—',
    })
  }
  if ((local.notify ?? 'always') !== (remote.notify ?? 'always')) {
    basicDiffs.push({
      kind: 'changed',
      title: '通知时机',
      before: local.notify || 'always',
      after: remote.notify || 'always',
    })
  }
  if ((local.review ?? 'auto') !== (remote.review ?? 'auto')) {
    basicDiffs.push({
      kind: 'changed',
      title: '审核方式',
      before: local.review || 'auto',
      after: remote.review || 'auto',
    })
  }
  if (!!local.debug !== !!remote.debug) {
    basicDiffs.push({
      kind: 'changed',
      title: '调试模式',
      before: local.debug ? '开启' : '关闭',
      after: remote.debug ? '开启' : '关闭',
    })
  }

  return [
    { title: '匹配规则', rows: matcherDiffs },
    { title: '读取字段', rows: fieldDiffs },
    { title: '输出变量', rows: outputDiffs },
    { title: '有效性探测', rows: probeDiffs },
    ...(basicDiffs.length ? [{ title: '基础配置', rows: basicDiffs }] : []),
  ]
})

const hasViolations = computed(() => !!props.item?.violations?.length)
const canConfirm = computed(() => !busy.value && (!hasViolations.value || acknowledge.value))

const kindBadge: Record<DiffKind, { label: string; tone: 'success' | 'danger' | 'warning' }> = {
  added: { label: '新增', tone: 'success' },
  removed: { label: '移除', tone: 'danger' },
  changed: { label: '变更', tone: 'warning' },
}

async function applyUpdate() {
  if (!props.item?.source || !props.repo || !canConfirm.value) return
  busy.value = true
  try {
    const { data } = await api.post<SourceImportApplyOutput>('/sources/import', {
      items: [
        {
          meta: {
            version: props.item.version,
            author: props.item.author,
            homepage: props.item.homepage,
            tags: props.item.tags,
          },
          source: props.item.source,
          conflict: 'overwrite',
          acknowledge: acknowledge.value,
          origin: {
            repo_id: props.repo.id,
            repo_url: props.repo.url,
            path: props.item.path,
            version: props.item.version,
          },
        },
      ],
    })
    if (data.succeeded === 1) {
      toast.success(`已更新「${props.item.name}」至 v${props.item.version}`)
      emit('updated')
      emit('close')
    } else {
      const err = data.results[0]?.error || '更新失败'
      toast.error(err)
    }
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogShell
    :open="open"
    title="确认更新来源"
    description="核对定义变更后应用更新；已采集的身份、历史版本与推送规则将保留"
    :icon="GitCompareArrowsIcon"
    size="lg"
    @close="emit('close')"
  >
    <div v-if="item" class="grid gap-4">
      <div class="flex flex-wrap items-center gap-3 rounded-lg border p-3">
        <SourceAvatar :name="item.name" :skey="item.key" :icon="item.icon ?? ''" size="sm" />
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-semibold">{{ item.name }}</span>
            <span class="text-muted-foreground font-mono text-xs">{{ item.key }}</span>
          </div>
          <div class="text-muted-foreground mt-0.5 flex items-center gap-1.5 text-xs">
            <span>{{ repo?.name }}</span>
            <span>·</span>
            <span class="font-mono">v{{ item.local_version || '?' }}</span>
            <ArrowRightIcon class="size-3" />
            <span class="text-foreground font-mono font-medium">v{{ item.version }}</span>
          </div>
        </div>
      </div>

      <Alert v-if="item.local_modified" class="border-warning/40 bg-warning/5">
        <AlertTriangleIcon class="text-warning size-4" />
        <AlertDescription class="text-xs leading-relaxed">
          本地来源定义曾被修改，确认更新后将覆盖本地改动；已有身份、历史版本与推送规则不受影响。
        </AlertDescription>
      </Alert>

      <div
        v-if="item.violations?.length"
        class="border-destructive/50 bg-destructive/5 grid gap-2 rounded-lg border p-3 text-xs"
      >
        <div class="text-destructive flex items-center gap-1.5 font-medium">
          <ShieldAlertIcon class="size-4 shrink-0" />
          违反安全规则
        </div>
        <ul class="text-destructive/90 list-disc space-y-1 pl-4">
          <li v-for="(v, i) in item.violations" :key="i">{{ v.message }}</li>
        </ul>
        <div class="flex items-center gap-2 pt-1">
          <Checkbox
            id="diff-ack"
            :model-value="acknowledge"
            @update:model-value="(v) => (acknowledge = v === true)"
          />
          <Label for="diff-ack" class="cursor-pointer text-xs font-medium">
            我已了解风险并确认更新
          </Label>
        </div>
      </div>

      <div class="grid gap-3">
        <section v-for="sec in sections" :key="sec.title" class="grid gap-2 rounded-lg border p-3">
          <h3 class="text-sm font-medium">{{ sec.title }}</h3>
          <p v-if="!sec.rows.length" class="text-muted-foreground text-xs">无变化</p>
          <ul v-else class="grid gap-2">
            <li
              v-for="(row, idx) in sec.rows"
              :key="idx"
              class="bg-muted/30 grid gap-1.5 rounded-md border p-2.5 text-xs"
            >
              <div class="flex items-center gap-2">
                <StatusBadge :tone="kindBadge[row.kind].tone">{{
                  kindBadge[row.kind].label
                }}</StatusBadge>
                <span class="font-mono font-medium">{{ row.title }}</span>
              </div>
              <div
                v-if="row.before !== undefined"
                class="bg-destructive/5 text-destructive rounded px-2 py-1 font-mono whitespace-pre-wrap [overflow-wrap:anywhere]"
              >
                - {{ row.before }}
              </div>
              <div
                v-if="row.after !== undefined"
                class="bg-success/5 text-foreground rounded px-2 py-1 font-mono whitespace-pre-wrap [overflow-wrap:anywhere]"
              >
                + {{ row.after }}
              </div>
            </li>
          </ul>
        </section>
      </div>
    </div>

    <template #footer>
      <Button variant="outline" :disabled="busy" @click="emit('close')">取消</Button>
      <Button :disabled="!canConfirm" @click="applyUpdate">
        <LoaderCircleIcon v-if="busy" class="animate-spin" />
        确认更新
      </Button>
    </template>
  </DialogShell>
</template>
