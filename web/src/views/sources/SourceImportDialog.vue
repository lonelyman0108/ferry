<script setup lang="ts">
import {
  CircleAlertIcon,
  EraserIcon,
  FileUpIcon,
  GlobeIcon,
  LoaderCircleIcon,
  ShieldAlertIcon,
  TriangleAlertIcon,
} from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import DialogShell from '@/components/DialogShell.vue'
import FormField from '@/components/FormField.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import type {
  SourceImportApplyItem,
  SourceImportApplyOutput,
  SourceImportPreviewItem,
  SourceImportPreviewOutput,
} from '@/types'

const props = defineProps<{
  open: boolean
  initialUrl?: string
}>()

const emit = defineEmits<{
  close: []
  imported: [singleId?: number]
}>()

type InputMode = 'text' | 'url'
type Step = 'input' | 'preview'

interface PickedFile {
  name: string
  content: string
}

interface ItemDecision {
  conflict: 'overwrite' | 'rename'
  newKey: string
  acknowledge: boolean
  applyError: string
  done: boolean
}

const mode = ref<InputMode>('text')
const step = ref<Step>('input')
const jsonText = ref('')
const urlInput = ref('')
const pickedFiles = ref<PickedFile[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const inputError = ref('')
const busy = ref(false)

const previewData = ref<SourceImportPreviewOutput | null>(null)
const states = ref<ItemDecision[]>([])

function resetState() {
  step.value = 'input'
  inputError.value = ''
  previewData.value = null
  states.value = []
  if (props.initialUrl) {
    mode.value = 'url'
    urlInput.value = props.initialUrl
  } else {
    mode.value = 'text'
    urlInput.value = ''
  }
  jsonText.value = ''
  pickedFiles.value = []
}

watch(
  () => [props.open, props.initialUrl] as const,
  ([isOpen, initUrl]) => {
    if (!isOpen) return
    resetState()
    if (initUrl?.trim()) {
      void runPreview()
    }
  },
  { immediate: true },
)

async function onPickFiles(ev: Event) {
  const input = ev.target as HTMLInputElement
  const files = input.files ? Array.from(input.files) : []
  input.value = ''
  if (!files.length) return
  inputError.value = ''
  try {
    const loaded: PickedFile[] = []
    for (const f of files) {
      loaded.push({ name: f.name, content: await f.text() })
    }
    if (loaded.length === 1) {
      pickedFiles.value = [loaded[0]!]
      jsonText.value = loaded[0]!.content
    } else {
      pickedFiles.value = loaded
      jsonText.value = ''
    }
  } catch (e) {
    toast.error(errorText(e))
  }
}

function clearFiles() {
  pickedFiles.value = []
  jsonText.value = ''
}

function initDecisions(items: SourceImportPreviewItem[]) {
  states.value = items.map((it) => ({
    conflict: 'overwrite',
    newKey: it.conflict ? `${it.source.key}_2` : '',
    acknowledge: false,
    applyError: '',
    done: false,
  }))
}

async function runPreview() {
  inputError.value = ''
  const payload: { text?: string; texts?: string[]; url?: string } = {}
  if (mode.value === 'url') {
    const u = urlInput.value.trim()
    if (!u) {
      inputError.value = '请填写来源 JSON 链接'
      return
    }
    payload.url = u
  } else if (pickedFiles.value.length > 1) {
    payload.texts = pickedFiles.value.map((f) => f.content)
  } else {
    const t = jsonText.value.trim()
    if (!t) {
      inputError.value = '请粘贴 JSON 内容或选择文件'
      return
    }
    payload.text = t
  }

  busy.value = true
  try {
    const { data } = await api.post<SourceImportPreviewOutput>('/sources/import/preview', payload)
    previewData.value = data
    initDecisions(data.items)
    step.value = 'preview'
  } catch (e) {
    inputError.value = errorText(e)
  } finally {
    busy.value = false
  }
}

const pendingItems = computed(() => {
  if (!previewData.value) return []
  return previewData.value.items
    .map((item, idx) => ({ item, state: states.value[idx]!, idx }))
    .filter((x) => !x.state?.done)
})

const canApply = computed(() => {
  const list = pendingItems.value
  if (!list.length) return false
  for (const { item, state } of list) {
    if (!item.valid) return false
    if (item.violations?.length && !state.acknowledge) return false
    if (item.conflict && state.conflict === 'rename' && !state.newKey.trim()) return false
  }
  return true
})

async function applyImport() {
  if (!previewData.value || !canApply.value) return
  const active = pendingItems.value
  const reqItems: SourceImportApplyItem[] = active.map(({ item, state }) => ({
    meta: item.meta,
    source: item.source,
    conflict: item.conflict ? state.conflict : '',
    new_key: item.conflict && state.conflict === 'rename' ? state.newKey.trim() : undefined,
    acknowledge: state.acknowledge,
  }))

  for (const { state } of active) {
    state.applyError = ''
  }

  busy.value = true
  try {
    const { data } = await api.post<SourceImportApplyOutput>('/sources/import', {
      items: reqItems,
    })
    let lastCreatedId: number | undefined
    data.results.forEach((res, i) => {
      const target = active[i]
      if (!target) return
      if (res.ok) {
        target.state.done = true
        if (res.source?.id) lastCreatedId = res.source.id
      } else {
        const detail = res.details?.length ? `：${res.details.join('；')}` : ''
        target.state.applyError = (res.error || '导入失败') + detail
      }
    })

    if (data.failed === 0) {
      toast.success(
        data.succeeded === 1
          ? `已导入「${data.results[0]?.name}」`
          : `已导入 ${data.succeeded} 个来源`,
      )
      emit('imported', data.succeeded === 1 ? lastCreatedId : undefined)
      emit('close')
    } else if (data.succeeded > 0) {
      toast.warning(`已导入 ${data.succeeded} 个来源，${data.failed} 个未通过`)
      emit('imported')
    } else {
      toast.error('导入失败，请核对条目提示')
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
    title="导入来源"
    :description="
      step === 'input'
        ? '支持粘贴 JSON、选择单个或多个文件、填写在线链接'
        : '核对来源匹配域名、读取字段与安全规则后确认导入'
    "
    :icon="FileUpIcon"
    size="lg"
    @close="emit('close')"
  >
    <template v-if="step === 'input'">
      <Tabs v-model="mode" class="grid gap-4" @update:model-value="inputError = ''">
        <TabsList class="grid w-full grid-cols-2">
          <TabsTrigger value="text">粘贴或文件</TabsTrigger>
          <TabsTrigger value="url">在线链接</TabsTrigger>
        </TabsList>

        <TabsContent value="text" class="mt-0 grid gap-4">
          <FormField label="JSON 文件" for="import-json-file" optional>
            <div class="flex flex-wrap items-center gap-2">
              <Button
                id="import-json-file"
                type="button"
                variant="outline"
                @click="fileInput?.click()"
              >
                <FileUpIcon /> 选择 JSON 文件
              </Button>
              <Button
                v-if="pickedFiles.length"
                type="button"
                variant="ghost"
                size="sm"
                @click="clearFiles"
              >
                <EraserIcon /> 清除文件
              </Button>
              <span v-if="pickedFiles.length === 1" class="text-muted-foreground text-xs">
                已读取：{{ pickedFiles[0]?.name }}
              </span>
              <span v-else-if="pickedFiles.length > 1" class="text-muted-foreground text-xs">
                已选择 {{ pickedFiles.length }} 个文件：{{
                  pickedFiles.map((f) => f.name).join('、')
                }}
              </span>
              <input
                ref="fileInput"
                type="file"
                accept=".json,application/json"
                multiple
                class="hidden"
                tabindex="-1"
                aria-hidden="true"
                @change="onPickFiles"
              />
            </div>
          </FormField>

          <FormField
            v-if="pickedFiles.length <= 1"
            label="JSON 内容"
            for="import-json-text"
            :error="mode === 'text' ? inputError : ''"
            hint="支持单个来源外壳对象或外壳数组"
          >
            <Textarea
              id="import-json-text"
              v-model="jsonText"
              rows="14"
              class="font-mono text-xs"
              placeholder='{"format": 1, "meta": {"version": 1}, "source": {"key": "example", ...}}'
            />
          </FormField>
          <div
            v-else-if="inputError"
            class="border-destructive/40 bg-destructive/5 text-destructive rounded-lg border p-3 text-xs"
          >
            {{ inputError }}
          </div>
        </TabsContent>

        <TabsContent value="url" class="mt-0 grid gap-4">
          <FormField
            label="来源 JSON 链接"
            for="import-json-url"
            :error="mode === 'url' ? inputError : ''"
            hint="由服务端拉取并解析，仅支持 http 与 https 地址"
          >
            <Input
              id="import-json-url"
              v-model="urlInput"
              type="url"
              class="font-mono text-sm"
              placeholder="https://raw.githubusercontent.com/.../source.json"
              @keydown.enter.prevent="runPreview"
            />
          </FormField>
        </TabsContent>
      </Tabs>
    </template>

    <template v-else-if="previewData">
      <div class="grid gap-3">
        <div
          v-if="previewData.url && !previewData.github_url"
          class="bg-muted/50 text-muted-foreground flex items-center gap-2 rounded-lg border px-3 py-2 text-xs"
        >
          <GlobeIcon class="size-4 shrink-0" />
          <span>该链接非 GitHub 地址，服务端直接连接拉取，不适用 GitHub 网络设置。</span>
        </div>

        <div
          v-for="(item, idx) in previewData.items"
          :key="idx"
          class="grid gap-3 rounded-xl border p-4"
          :class="[
            !item.valid || item.violations?.length
              ? 'border-destructive/50 bg-destructive/[0.02]'
              : '',
            states[idx]?.done ? 'opacity-60' : '',
          ]"
        >
          <div class="flex flex-wrap items-start gap-3">
            <SourceAvatar
              :name="item.source.name || item.source.key || '?'"
              :skey="item.source.key || 'source'"
              :icon="item.source.icon ?? ''"
              size="sm"
            />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-semibold">{{ item.source.name || '未命名来源' }}</span>
                <span class="text-muted-foreground font-mono text-xs">{{
                  item.source.key || '—'
                }}</span>
                <StatusBadge>v{{ item.meta.version }}</StatusBadge>
                <span v-if="item.meta.author" class="text-muted-foreground text-xs"
                  >作者：{{ item.meta.author }}</span
                >
              </div>
              <p v-if="item.source.description" class="text-muted-foreground mt-1 text-xs">
                {{ item.source.description }}
              </p>
              <div v-if="item.meta.tags?.length" class="mt-1.5 flex flex-wrap gap-1">
                <StatusBadge v-for="tag in item.meta.tags" :key="tag">{{ tag }}</StatusBadge>
              </div>
            </div>
            <div class="flex items-center gap-1.5">
              <StatusBadge v-if="states[idx]?.done" tone="success" dot>已导入</StatusBadge>
              <StatusBadge v-else-if="!item.valid" tone="danger" dot>定义有误</StatusBadge>
              <StatusBadge v-else-if="item.violations?.length" tone="danger" dot
                >违反安全规则</StatusBadge
              >
              <StatusBadge v-else-if="item.conflict" tone="warning" dot>标识冲突</StatusBadge>
              <StatusBadge v-else tone="success" dot>可导入</StatusBadge>
            </div>
          </div>

          <dl class="bg-muted/30 grid gap-2 rounded-lg p-3 text-xs sm:grid-cols-2">
            <div>
              <dt class="text-muted-foreground">匹配域名</dt>
              <dd class="mt-0.5 font-mono break-all">
                {{ item.hosts?.length ? item.hosts.join('、') : '—' }}
              </dd>
            </div>
            <div>
              <dt class="text-muted-foreground">有效性检测域名</dt>
              <dd class="mt-0.5 font-mono break-all">{{ item.probe_host || '未配置' }}</dd>
            </div>
            <div>
              <dt class="text-muted-foreground">读取字段</dt>
              <dd class="mt-0.5 font-mono break-all">
                {{
                  item.source.fields?.length ? item.source.fields.map((f) => f.key).join('、') : '—'
                }}
              </dd>
            </div>
            <div>
              <dt class="text-muted-foreground">输出变量</dt>
              <dd class="mt-0.5 font-mono break-all">
                {{
                  item.source.outputs?.length
                    ? item.source.outputs.map((o) => o.name).join('、')
                    : '无'
                }}
              </dd>
            </div>
          </dl>

          <div
            v-if="!item.valid && item.details?.length"
            class="border-destructive/40 bg-destructive/5 text-destructive grid gap-1 rounded-lg border p-3 text-xs"
          >
            <div class="flex items-center gap-1.5 font-medium">
              <CircleAlertIcon class="size-4 shrink-0" />
              <span>来源定义有误，修正后方可导入：</span>
            </div>
            <ul class="ml-5 list-disc space-y-0.5">
              <li v-for="(d, di) in item.details" :key="di">{{ d }}</li>
            </ul>
          </div>

          <div
            v-if="item.violations?.length && states[idx]"
            class="border-destructive/40 bg-destructive/5 text-destructive grid gap-2 rounded-lg border p-3 text-xs"
          >
            <div class="flex items-center gap-1.5 font-medium">
              <ShieldAlertIcon class="size-4 shrink-0" />
              <span>该来源违反安全规则：</span>
            </div>
            <ul class="ml-5 list-disc space-y-0.5">
              <li v-for="(v, vi) in item.violations" :key="vi">
                <span v-if="v.path" class="font-mono">{{ v.path }}：</span>{{ v.message }}
              </li>
            </ul>
            <Label
              v-if="item.valid && !states[idx].done"
              class="text-foreground mt-1 flex cursor-pointer items-center gap-2 font-medium"
            >
              <Checkbox v-model="states[idx].acknowledge" />
              <span>我已了解风险</span>
            </Label>
          </div>

          <div
            v-if="item.warnings?.length"
            class="border-warning/40 bg-warning/5 text-warning grid gap-1 rounded-lg border p-3 text-xs"
          >
            <div class="flex items-center gap-1.5 font-medium">
              <TriangleAlertIcon class="size-4 shrink-0" />
              <span>注意事项：</span>
            </div>
            <ul class="ml-5 list-disc space-y-0.5">
              <li v-for="(w, wi) in item.warnings" :key="wi">{{ w.message }}</li>
            </ul>
          </div>

          <div
            v-if="item.valid && item.conflict && states[idx] && !states[idx].done"
            class="bg-muted/40 grid gap-2.5 rounded-lg border p-3 text-xs"
          >
            <div class="font-medium">
              已存在相同标识的来源「{{ item.existing_name }}」（<span class="font-mono">{{
                item.source.key
              }}</span
              >）
            </div>
            <p class="text-muted-foreground">
              {{
                item.local_modified
                  ? '本地定义与导入内容存在差异。覆盖更新将替换匹配规则、字段与输出变量，保留现有身份、历史版本与推送规则。'
                  : '本地定义与导入内容一致。覆盖更新保留现有身份、历史版本与推送规则。'
              }}
            </p>
            <div class="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                size="sm"
                :variant="states[idx].conflict === 'overwrite' ? 'default' : 'outline'"
                @click="states[idx].conflict = 'overwrite'"
              >
                覆盖更新
              </Button>
              <Button
                type="button"
                size="sm"
                :variant="states[idx].conflict === 'rename' ? 'default' : 'outline'"
                @click="states[idx].conflict = 'rename'"
              >
                改用新标识
              </Button>
            </div>
            <FormField
              v-if="states[idx].conflict === 'rename'"
              label="新来源标识"
              :for="`rename-key-${idx}`"
              hint="仅允许小写字母、数字、- 和 _，须以字母开头"
            >
              <Input
                :id="`rename-key-${idx}`"
                v-model="states[idx].newKey"
                class="font-mono text-xs"
                :placeholder="`${item.source.key}_2`"
              />
            </FormField>
          </div>

          <div
            v-if="states[idx]?.applyError"
            class="border-destructive/40 bg-destructive/5 text-destructive rounded-lg border px-3 py-2 text-xs"
          >
            {{ states[idx].applyError }}
          </div>
        </div>
      </div>
    </template>

    <template #footer-start>
      <Button v-if="step === 'preview'" variant="outline" :disabled="busy" @click="step = 'input'">
        返回修改
      </Button>
    </template>
    <template #footer>
      <Button variant="outline" :disabled="busy" @click="emit('close')">取消</Button>
      <Button v-if="step === 'input'" :disabled="busy" @click="runPreview">
        <LoaderCircleIcon v-if="busy" class="animate-spin" /> 预览
      </Button>
      <Button v-else :disabled="busy || !canApply" @click="applyImport">
        <LoaderCircleIcon v-if="busy" class="animate-spin" /> 确认导入
      </Button>
    </template>
  </DialogShell>
</template>
