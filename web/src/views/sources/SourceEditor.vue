<script setup lang="ts">
import {
  CircleCheckIcon,
  CircleXIcon,
  DownloadIcon,
  EraserIcon,
  FileTextIcon,
  FlaskConicalIcon,
  ImageUpIcon,
  KeyRoundIcon,
  LoaderCircleIcon,
  LockIcon,
  PlayIcon,
  PlusIcon,
  RadarIcon,
  RouteIcon,
  Trash2Icon,
  TriangleAlertIcon,
  VariableIcon,
} from '@lucide/vue'
import { createReusableTemplate, useMediaQuery, useScroll } from '@vueuse/core'
import { computed, nextTick, onMounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { toast } from 'vue-sonner'

import { api, ApiError, errorText } from '@/api'
import ConfirmButton from '@/components/ConfirmButton.vue'
import FormField from '@/components/FormField.vue'
import HintText from '@/components/HintText.vue'
import DialogShell from '@/components/DialogShell.vue'
import IconTile from '@/components/IconTile.vue'
import KeyValueEditor from '@/components/KeyValueEditor.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import SourceSetup from './SourceSetup.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { fmtTime, secondaryKey } from '@/format'
import { icons } from '@/lib/icons'
import type { Tone } from '@/lib/tone'
import { useSourceIcons } from '@/stores/sourceIcons'
import type { FieldDoc, ProbeSpec, SourceDoc, SourceView, TryResult, TryValue } from '@/types'

// 来源编辑器弹窗。左侧分区导航，中间表单，右侧（宽屏）是共用的样例请求：
// 任何改动都会用当前草稿重新试跑，匹配、取值、输出、检测各处就地显示结果。
// initialSection 是打开时停在哪个分区，例如新建保存后直接到接入清单。
const props = defineProps<{ id?: string; initialSection?: string }>()
const emit = defineEmits<{ close: []; saved: [id: number] }>()

const isNew = computed(() => !props.id)
const loaded = ref(false)
const saving = ref(false)
const problems = ref<string[]>([])
const section = ref(props.initialSection || 'basic')
const confirmClose = ref(false)
const wide = useMediaQuery('(min-width: 1280px)')
// 窄屏时分区导航是一行横向滚动的标签：切换分区或刚打开时把当前项滚到中间，
// 两端还有未显示的分区时用渐隐提示可以继续滑动。
const vertical = useMediaQuery('(min-width: 1024px)')
const navEl = useTemplateRef<HTMLElement>('nav')
const { arrivedState } = useScroll(navEl)
const navMask = computed(() => {
  if (vertical.value) return undefined
  const l = arrivedState.left ? 'black' : 'transparent'
  const r = arrivedState.right ? 'black' : 'transparent'
  const g = `linear-gradient(to right, ${l}, black 2rem, black calc(100% - 2rem), ${r})`
  return { maskImage: g, webkitMaskImage: g }
})
watch([section, () => loaded.value], async () => {
  if (vertical.value || !loaded.value) return
  await nextTick()
  navEl.value
    ?.querySelector('[aria-current]')
    ?.scrollIntoView({ inline: 'center', block: 'nearest' })
})
// 样例请求面板在宽屏放右栏，窄屏作为一个分区，两处共用同一份模板。
const [DefineSample, ReuseSample] = createReusableTemplate()

function blank(): SourceDoc {
  return {
    key: '',
    name: '',
    description: '',
    icon: '',
    enabled: true,
    identity_field: '',
    merge_window: 60,
    notify: 'always',
    min_interval: 60,
    debug: false,
    review: 'auto',
    matchers: [{ name: 'main', host: '', path: '', method: '' }],
    fields: [],
    outputs: [],
  }
}

const doc = reactive<SourceDoc>(blank())
const probeOn = ref(false)
const probe = reactive<ProbeSpec>({ method: 'GET', url: '', expect: 'status == 200' })
const saved = ref('')

const inOptions = [
  {
    value: 'header',
    label: '请求头',
    hint: '请求头名称，不区分大小写',
    placeholder: 'Authorization',
  },
  { value: 'cookie', label: 'Cookie', hint: 'Cookie 名称', placeholder: 'session_id' },
  { value: 'query', label: 'URL 参数', hint: 'URL 中 ? 之后的参数名', placeholder: 'token' },
  {
    value: 'body_json',
    label: 'JSON 正文',
    hint: '以点号分隔层级，数组使用下标，如 data.token、list.0.id',
    placeholder: 'data.token',
  },
  { value: 'body_form', label: '表单正文', hint: '表单字段名', placeholder: 'token' },
  {
    value: 'regex',
    label: '正则',
    hint: '读取第一个捕获组（括号）中的内容',
    placeholder: 'token=([^&]+)',
  },
]
const typeOptions: { value: string; label: string; short: string; tone: Tone }[] = [
  { value: 'text', label: '文本', short: '文本', tone: 'neutral' },
  { value: 'jwt', label: 'JWT（自动识别过期时间）', short: 'JWT', tone: 'violet' },
  { value: 'cookie', label: 'Cookie', short: 'Cookie', tone: 'amber' },
  { value: 'json', label: 'JSON', short: 'JSON', tone: 'sky' },
  { value: 'number', label: '数字', short: '数字', tone: 'indigo' },
  { value: 'bool', label: '布尔', short: '布尔', tone: 'emerald' },
]
const typeOf = (f: FieldDoc) => typeOptions.find((o) => o.value === f.type) ?? typeOptions[0]
const inOf = (f: FieldDoc) => inOptions.find((o) => o.value === f.in) ?? inOptions[0]
const methods = ['', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']
// 模板语法和 Vue 的插值撞了，示例放在这里再输出。
const examples = {
  ref: '{{.字段名}}',
  output: 'pt_key={{.pt_key}};pt_pin={{.pt_pin}};',
  header: 'Bearer {{.token}}',
}
const expectPresets = [
  { label: '状态码 200', value: 'status == 200' },
  { label: '状态码 200 且 code 为 0', value: 'status == 200 && json.code == 0' },
]

const sections = computed(() => {
  const s = [
    { value: 'basic', label: '基本', icon: FileTextIcon },
    { value: 'matchers', label: '请求匹配', icon: RouteIcon, count: doc.matchers.length },
    { value: 'fields', label: '字段', icon: KeyRoundIcon, count: doc.fields.length },
    { value: 'outputs', label: '输出变量', icon: VariableIcon, count: doc.outputs?.length ?? 0 },
    { value: 'probe', label: '有效性检测', icon: RadarIcon, on: probeOn.value },
    { value: 'setup', label: '接入清单', icon: icons.checklist },
  ]
  if (!wide.value) s.push({ value: 'sample', label: '样例测试', icon: FlaskConicalIcon })
  return s
})
watch(wide, (w) => {
  if (w && section.value === 'sample') section.value = 'basic'
})

// 匹配规则在下拉框里显示成“名称 · 域名路径”，比只看名称直观。
const matcherLabel = (name: string) => {
  const m = doc.matchers.find((x) => x.name === name)
  return m ? `${m.name} · ${m.host || '未填域名'}${m.path ? ' ' + m.path : ''}` : name
}

// 自定义图标：选一张图片读成 data URI 存进定义里。类型和大小的限制与后端校验一致。
const iconTypes = ['image/png', 'image/jpeg', 'image/webp', 'image/svg+xml']
const maxIconBytes = 64 * 1024
const iconInput = useTemplateRef<HTMLInputElement>('iconInput')
const iconError = ref('')

function pickIcon() {
  iconInput.value?.click()
}

function readIcon(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!iconTypes.includes(file.type)) {
    iconError.value = '仅支持 PNG、JPEG、WebP 或 SVG 图片'
    return
  }
  if (file.size > maxIconBytes) {
    iconError.value = `图片为 ${Math.ceil(file.size / 1024)} KB，超过 64 KB 上限，请压缩后重试`
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    doc.icon = String(reader.result)
    iconError.value = ''
  }
  reader.onerror = () => {
    iconError.value = '读取图片失败'
  }
  reader.readAsDataURL(file)
}

function fill(v: SourceDoc) {
  Object.assign(doc, blank(), v)
  doc.matchers = v.matchers.map((m) => ({ path: '', method: '', ...m }))
  doc.fields = v.fields.map((f) => ({
    name: '',
    matcher: '',
    regex_on: '',
    type: 'text',
    sensitive: false,
    min_len: undefined,
    max_len: undefined,
    pattern: '',
    ...f,
  }))
  doc.outputs = (v.outputs ?? []).map((o) => ({ ...o }))
  probeOn.value = !!v.probe
  Object.assign(
    probe,
    {
      method: 'GET',
      url: '',
      headers: undefined,
      body: '',
      expect: 'status == 200',
      identity: '',
      timeout: undefined,
    },
    v.probe ?? {},
  )
  saved.value = JSON.stringify(payload())
}

// payload 生成提交给后端的文档：去掉空的可选项，数字转成整数。
function payload(): SourceDoc {
  const num = (x: unknown) => {
    const n = Number(x)
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined
  }
  const opt = (s?: string) => (s && s.trim() ? s.trim() : undefined)
  const out: SourceDoc = {
    key: doc.key.trim(),
    name: doc.name.trim(),
    description: opt(doc.description),
    icon: opt(doc.icon),
    enabled: doc.enabled,
    identity_field: opt(doc.identity_field),
    merge_window: Number(doc.merge_window) || 0,
    notify: doc.notify,
    min_interval: Number(doc.min_interval) || 0,
    debug: doc.debug || undefined,
    review: doc.review,
    matchers: doc.matchers.map((m) => ({
      name: m.name.trim(),
      host: m.host.trim(),
      path: opt(m.path),
      method: opt(m.method),
    })),
    fields: doc.fields.map((f) => ({
      key: f.key.trim(),
      name: opt(f.name),
      matcher: opt(f.matcher),
      in: f.in,
      path: f.path,
      regex_on: f.in === 'regex' ? f.regex_on || 'url' : undefined,
      type: f.type,
      sensitive: f.sensitive || undefined,
      min_len: num(f.min_len),
      max_len: num(f.max_len),
      pattern: opt(f.pattern),
    })),
    outputs: (doc.outputs ?? []).map((o) => ({ name: o.name.trim(), template: o.template })),
  }
  if (probeOn.value) {
    out.probe = {
      method: opt(probe.method),
      url: probe.url.trim(),
      headers: probe.headers,
      body: opt(probe.body),
      expect: probe.expect,
      identity: opt(probe.identity),
      timeout: num(probe.timeout),
    }
  }
  return out
}

const dirty = computed(() => loaded.value && JSON.stringify(payload()) !== saved.value)

onMounted(async () => {
  if (props.id) {
    try {
      fill((await api.get<SourceView>(`/sources/${props.id}`)).data)
    } catch (e) {
      toast.error(errorText(e))
      emit('close')
      return
    }
  } else {
    saved.value = JSON.stringify(payload())
  }
  loadSample()
  loaded.value = true
})

async function save() {
  saving.value = true
  problems.value = []
  try {
    const body = payload()
    // 匹配域名变了，订阅的 MITM 域名也会变，手机上的订阅要更新。
    const hosts = (ms: { host: string }[]) =>
      ms
        .map((m) => m.host.trim().toLowerCase())
        .sort()
        .join(',')
    const hostsChanged =
      !isNew.value && hosts(JSON.parse(saved.value).matchers) !== hosts(body.matchers)
    const res = isNew.value
      ? await api.post<SourceView>('/sources', body)
      : await api.put<SourceView>(`/sources/${props.id}`, body)
    fill(res.data)
    if (hostsChanged)
      toast.success('已保存', {
        description: 'MITM 域名已变更，读取该来源的订阅需在客户端中更新',
        action: { label: '接入清单', onClick: () => (section.value = 'setup') },
      })
    else toast.success('已保存')
    if (isNew.value) storeSample(String(res.data.id))
    void useSourceIcons().load()
    emit('saved', res.data.id)
  } catch (e) {
    if (e instanceof ApiError && e.details.length) problems.value = e.details
    toast.error(e instanceof ApiError ? e.message : errorText(e))
  } finally {
    saving.value = false
  }
}

async function remove() {
  try {
    await api.delete(`/sources/${props.id}`)
    toast.success('已删除')
    emit('saved', 0)
    emit('close')
  } catch (e) {
    toast.error(errorText(e))
  }
}

function requestClose() {
  if (dirty.value) confirmClose.value = true
  else emit('close')
}

function addField() {
  doc.fields.push({
    key: '',
    name: '',
    matcher: '',
    in: 'header',
    path: '',
    regex_on: '',
    type: 'text',
    sensitive: false,
    min_len: undefined,
    max_len: undefined,
    pattern: '',
  })
}

function insertVar(i: number, key: string) {
  doc.outputs![i].template += '{{.' + key + '}}'
}

watch(
  () => doc.fields.map((f) => f.key),
  (keys) => {
    if (doc.identity_field && !keys.includes(doc.identity_field)) doc.identity_field = ''
  },
)

// ---- 样例请求与实时试跑 ----
const sample = reactive({ method: 'GET', url: '', headers: '', body: '' })
const trying = ref(false)
const tr = ref<TryResult | null>(null)
const tryError = ref('')
const probing = ref(false)
const probeResult = ref<TryResult['probe'] | null>(null)

// 样例里常有真实的请求头，只存在这个标签页里（sessionStorage），按来源区分，关掉标签页就没了。
const samplePrefix = 'ferry:sample:'
const sampleKey = (id?: string) => samplePrefix + (id ?? 'new')
const emptySample = () => ({ method: 'GET', url: '', headers: '', body: '' })
function loadSample() {
  try {
    // 以前存在 localStorage 里、一直不清，打开编辑器时顺手删掉。
    for (const k of Object.keys(localStorage)) {
      if (k.startsWith(samplePrefix)) localStorage.removeItem(k)
    }
    const raw = sessionStorage.getItem(sampleKey(props.id))
    if (raw) Object.assign(sample, JSON.parse(raw))
  } catch {
    // 浏览器禁用存储时忽略
  }
}
function storeSample(id = props.id) {
  try {
    if (sample.url.trim() || sample.headers.trim() || sample.body.trim())
      sessionStorage.setItem(sampleKey(id), JSON.stringify(sample))
    else sessionStorage.removeItem(sampleKey(id))
  } catch {
    // 同上
  }
}
function clearSample() {
  Object.assign(sample, emptySample())
  tr.value = null
}

// 请求头一行一个，“名称: 值”。直接从抓包工具里复制过来就能用。
function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const i = line.indexOf(':')
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}

function tryBody(withProbe: boolean) {
  return {
    source: payload(),
    method: sample.method,
    url: sample.url.trim(),
    headers: parseHeaders(sample.headers),
    body: sample.body,
    probe: withProbe,
  }
}

async function runTry() {
  if (!sample.url.trim()) {
    tr.value = null
    return
  }
  trying.value = true
  try {
    tr.value = (await api.post<TryResult>('/sources/try', tryBody(false))).data
    tryError.value = ''
  } catch (e) {
    tryError.value = errorText(e)
  } finally {
    trying.value = false
  }
}

async function runProbe() {
  probing.value = true
  try {
    probeResult.value = (await api.post<TryResult>('/sources/try', tryBody(true))).data.probe
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    probing.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => [JSON.stringify(payload()), JSON.stringify(sample)],
  () => {
    if (!loaded.value) return
    storeSample()
    clearTimeout(timer)
    timer = setTimeout(runTry, 400)
  },
)
watch(loaded, (v) => v && runTry())

const fieldResult = (f: FieldDoc): TryValue | undefined =>
  tr.value && f.key.trim() ? tr.value.fields[f.key.trim()] : undefined
const outputResult = (name: string): TryValue | undefined =>
  tr.value && name.trim() ? tr.value.outputs[name.trim()] : undefined
const fieldsHit = computed(() =>
  tr.value ? Object.values(tr.value.fields).filter((v) => v.ok).length : 0,
)
</script>

<template>
  <DefineSample>
    <div class="grid gap-4">
      <div class="flex items-start gap-3">
        <IconTile :icon="FlaskConicalIcon" size="sm" />
        <div class="grid gap-0.5">
          <span class="text-sm font-semibold">样例请求</span>
          <span class="text-muted-foreground text-xs"
            >各分区实时显示匹配与读取结果；仅保存在当前标签页，关闭后清除</span
          >
        </div>
        <Button
          v-if="sample.url || sample.headers || sample.body"
          variant="ghost"
          size="sm"
          class="text-muted-foreground ml-auto shrink-0"
          @click="clearSample"
          ><EraserIcon /> 清除样例</Button
        >
      </div>
      <div class="grid grid-cols-[6.5rem_1fr] gap-2">
        <NativeSelect v-model="sample.method" aria-label="请求方法" class="w-full">
          <NativeSelectOption v-for="x in methods.slice(1)" :key="x" :value="x">{{
            x
          }}</NativeSelectOption>
        </NativeSelect>
        <Input
          v-model="sample.url"
          aria-label="URL"
          class="font-mono text-xs"
          placeholder="https://api.example.com/user?id=1"
        />
      </div>
      <FormField label="请求头" for="s-headers" hint="每行一条，格式为「名称: 值」">
        <Textarea
          id="s-headers"
          v-model="sample.headers"
          rows="6"
          class="font-mono text-xs"
          placeholder="Authorization: Bearer …&#10;Cookie: a=1; b=2"
        />
      </FormField>
      <FormField label="请求体" for="s-body" optional>
        <Textarea id="s-body" v-model="sample.body" rows="4" class="font-mono text-xs" />
      </FormField>

      <div v-if="sample.url.trim()" class="grid gap-2 rounded-lg border p-3 text-sm">
        <div class="flex items-center gap-2 font-medium">
          结果
          <LoaderCircleIcon v-if="trying" class="text-muted-foreground size-3.5 animate-spin" />
        </div>
        <p v-if="tryError" class="text-destructive text-xs">{{ tryError }}</p>
        <template v-else-if="tr">
          <div class="flex items-center gap-2">
            <RouteIcon class="text-muted-foreground size-4" />
            <span v-if="tr.matched.length" class="text-success"
              >命中 {{ tr.matched.length }} 条匹配规则：<span class="font-mono">{{
                tr.matched.join('、')
              }}</span></span
            >
            <span v-else class="text-muted-foreground">未命中任何匹配规则</span>
          </div>
          <div class="flex items-center gap-2">
            <KeyRoundIcon class="text-muted-foreground size-4" />
            <span :class="fieldsHit ? 'text-success' : 'text-muted-foreground'"
              >已读取 {{ fieldsHit }} / {{ Object.keys(tr.fields).length }} 个字段</span
            >
          </div>
          <div
            v-for="(v, k) in tr.fields"
            :key="k"
            class="grid grid-cols-[auto_1fr] items-start gap-x-2 pl-6 text-xs"
          >
            <CircleCheckIcon v-if="v.ok && !v.invalid" class="text-success mt-0.5 size-3.5" />
            <TriangleAlertIcon v-else-if="v.ok" class="text-warning mt-0.5 size-3.5" />
            <CircleXIcon v-else class="text-muted-foreground mt-0.5 size-3.5" />
            <span class="min-w-0">
              <span class="font-mono font-medium">{{ k }}</span>
              <span class="text-muted-foreground block truncate font-mono">{{
                v.ok ? (v.invalid ? v.invalid : v.value) : v.error
              }}</span>
            </span>
          </div>
        </template>
      </div>
    </div>
  </DefineSample>

  <DialogShell
    :open="true"
    :title="isNew ? '新建来源' : doc.name || doc.key"
    :icon="icons.source"
    size="full"
    bare
    :enabled="loaded ? doc.enabled : undefined"
    enabled-label="启用来源"
    @update:enabled="(v) => (doc.enabled = v)"
    @close="requestClose"
  >
    <template v-if="doc.name || doc.icon" #icon>
      <SourceAvatar
        :name="doc.name"
        :skey="doc.key"
        :icon="doc.icon ?? ''"
        :disabled="doc.enabled === false"
      />
    </template>
    <template #badges>
      <StatusBadge v-if="dirty" tone="warning" dot>未保存</StatusBadge>
    </template>
    <template #description>
      <template v-if="isNew">请求匹配、字段读取与输出变量</template>
      <template v-else
        ><span class="font-mono">{{ doc.key }}</span> · {{ doc.matchers.length }} 条匹配规则 ·
        {{ doc.fields.length }} 个字段 · {{ doc.outputs?.length ?? 0 }} 个输出变量</template
      >
    </template>

    <div
      v-if="loaded"
      class="grid h-full min-h-0 grid-rows-[auto_1fr] lg:grid-cols-[12rem_1fr] lg:grid-rows-1 xl:grid-cols-[12rem_1fr_24rem]"
    >
      <!-- 分区导航 -->
      <nav
        ref="nav"
        aria-label="编辑分区"
        class="no-scrollbar flex gap-1 overflow-x-auto border-b p-2 lg:flex-col lg:border-r lg:border-b-0 lg:p-3"
        :style="navMask"
      >
        <button
          v-for="s in sections"
          :key="s.value"
          type="button"
          class="focus-visible:ring-ring/50 flex shrink-0 items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition outline-none focus-visible:ring-3 max-md:min-h-11"
          :class="
            section === s.value
              ? 'bg-primary/10 text-primary font-medium'
              : 'text-muted-foreground hover:bg-muted hover:text-foreground'
          "
          :aria-current="section === s.value ? 'true' : undefined"
          @click="section = s.value"
        >
          <component :is="s.icon" class="size-4" />
          <span class="mr-auto">{{ s.label }}</span>
          <span
            v-if="s.count !== undefined"
            class="bg-muted-foreground/15 min-w-5 rounded-full px-1.5 text-center text-xs tabular-nums"
            >{{ s.count }}</span
          >
          <template v-if="s.on !== undefined">
            <span
              aria-hidden="true"
              class="size-1.5 rounded-full"
              :class="s.on ? 'bg-success' : 'bg-muted-foreground/40'"
            />
            <span class="sr-only">{{ s.on ? '已开启' : '未开启' }}</span>
          </template>
        </button>
      </nav>

      <!-- 表单 -->
      <div class="min-h-0 overflow-y-auto">
        <div class="mx-auto grid max-w-4xl gap-5 p-5">
          <Alert v-if="problems.length" variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>定义校验未通过</AlertTitle>
            <AlertDescription>
              <ul class="list-disc pl-4">
                <li v-for="p in problems" :key="p">{{ p }}</li>
              </ul>
            </AlertDescription>
          </Alert>

          <!-- 基本 -->
          <template v-if="section === 'basic'">
            <section class="grid gap-4">
              <div class="border-b pb-2">
                <h3 class="text-sm font-semibold">基本信息</h3>
              </div>
              <div class="grid gap-4 sm:grid-cols-2">
                <FormField
                  label="来源标识"
                  for="src-key"
                  :hint="
                    isNew
                      ? '以小写字母开头，可包含小写字母、数字、- 和 _；用于变量备注 ferry:<标识>；创建后不可修改'
                      : '创建后不可修改'
                  "
                >
                  <Input
                    id="src-key"
                    v-model="doc.key"
                    :disabled="!isNew"
                    class="font-mono"
                    placeholder="如 my_app"
                  />
                </FormField>
                <FormField label="名称" for="src-name">
                  <Input id="src-name" v-model="doc.name" placeholder="显示名称" />
                </FormField>
                <FormField
                  label="图标"
                  for="src-icon"
                  optional
                  full
                  :error="iconError"
                  hint="PNG、JPEG、WebP 或 SVG，不超过 64 KB；未设置时显示名称首字"
                >
                  <div class="flex items-center gap-3">
                    <SourceAvatar
                      :name="doc.name || doc.key || '?'"
                      :skey="doc.key"
                      :icon="doc.icon ?? ''"
                      size="lg"
                    />
                    <Button id="src-icon" type="button" variant="outline" @click="pickIcon"
                      ><ImageUpIcon /> {{ doc.icon ? '更换图片' : '选择图片' }}</Button
                    >
                    <Button
                      v-if="doc.icon"
                      type="button"
                      variant="ghost"
                      @click="((doc.icon = ''), (iconError = ''))"
                      ><EraserIcon /> 清除</Button
                    >
                    <input
                      ref="iconInput"
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/svg+xml"
                      class="hidden"
                      tabindex="-1"
                      aria-hidden="true"
                      @change="readIcon"
                    />
                  </div>
                </FormField>
                <FormField label="说明" for="src-desc" optional full>
                  <Textarea
                    id="src-desc"
                    v-model="doc.description"
                    rows="3"
                    placeholder="触发更新的页面或操作"
                  />
                </FormField>
              </div>
            </section>

            <section class="grid gap-4">
              <div class="border-b pb-2">
                <h3 class="text-sm font-semibold">客户端脚本</h3>
                <p class="text-muted-foreground text-xs">通知策略与回传频率</p>
              </div>
              <div class="grid gap-4 sm:grid-cols-2">
                <FormField label="设备通知" for="src-notify">
                  <NativeSelect id="src-notify" v-model="doc.notify" class="w-full">
                    <NativeSelectOption value="always">每次回传</NativeSelectOption>
                    <NativeSelectOption value="changed">值变化时</NativeSelectOption>
                    <NativeSelectOption value="first">仅首次</NativeSelectOption>
                  </NativeSelect>
                </FormField>
                <FormField
                  label="最短回传间隔（秒）"
                  for="src-interval"
                  hint="间隔内相同值不重复回传"
                >
                  <Input
                    id="src-interval"
                    v-model.number="doc.min_interval"
                    type="number"
                    min="0"
                    max="86400"
                  />
                </FormField>
                <FormField full hint="在客户端脚本日志中输出匹配与读取过程">
                  <Label class="font-normal"><Switch v-model="doc.debug" /> 调试日志</Label>
                </FormField>
              </div>
            </section>

            <section class="grid gap-4">
              <div class="border-b pb-2">
                <h3 class="text-sm font-semibold">回传处理</h3>
                <p class="text-muted-foreground text-xs">合并、校验与生效策略</p>
              </div>
              <div class="grid gap-4 sm:grid-cols-2">
                <FormField label="生效方式" for="src-review" hint="校验失败的值自动驳回">
                  <NativeSelect id="src-review" v-model="doc.review" class="w-full">
                    <NativeSelectOption value="auto">校验通过后自动生效</NativeSelectOption>
                    <NativeSelectOption value="manual">人工审核后生效</NativeSelectOption>
                  </NativeSelect>
                </FormField>
                <FormField
                  label="合并窗口（秒）"
                  for="src-merge"
                  hint="自首次回传起计时，窗口内的回传合并为一批；字段齐全时提前处理；0 表示立即处理"
                >
                  <Input
                    id="src-merge"
                    v-model.number="doc.merge_window"
                    type="number"
                    min="0"
                    max="3600"
                  />
                </FormField>
                <FormField
                  label="身份字段"
                  for="src-identity"
                  full
                  hint="用于区分多个身份（如多账号）的字段；留空表示单一身份"
                >
                  <NativeSelect id="src-identity" v-model="doc.identity_field" class="w-full">
                    <NativeSelectOption value="">无（单一身份）</NativeSelectOption>
                    <NativeSelectOption
                      v-for="f in doc.fields.filter((x) => x.key)"
                      :key="f.key"
                      :value="f.key"
                    >
                      {{ f.name || f.key }}{{ secondaryKey(f.name, f.key) ? `（${f.key}）` : '' }}
                    </NativeSelectOption>
                  </NativeSelect>
                </FormField>
              </div>
            </section>
          </template>

          <!-- 请求匹配 -->
          <template v-else-if="section === 'matchers'">
            <HintText>
              命中任一匹配规则的请求进入字段读取；匹配规则的域名写入订阅的 MITM 列表。
            </HintText>
            <div v-for="(m, i) in doc.matchers" :key="i" class="grid gap-4 rounded-xl border p-4">
              <div class="flex flex-wrap items-center gap-2">
                <IconTile :icon="RouteIcon" size="sm" />
                <span class="font-mono text-sm font-medium">{{ m.name || '未命名' }}</span>
                <template v-if="tr?.matchers[i]">
                  <StatusBadge v-if="tr.matchers[i].matched" tone="success" :icon="CircleCheckIcon"
                    >样例命中</StatusBadge
                  >
                  <StatusBadge v-else tone="neutral" :icon="CircleXIcon"
                    >未命中：{{ tr.matchers[i].reason }}</StatusBadge
                  >
                </template>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="ml-auto"
                  aria-label="删除匹配规则"
                  :disabled="doc.matchers.length <= 1"
                  @click="doc.matchers.splice(i, 1)"
                >
                  <Trash2Icon />
                </Button>
              </div>
              <div class="grid gap-4 sm:grid-cols-[1fr_2fr]">
                <FormField
                  label="名称"
                  :for="`m-name-${i}`"
                  hint="字段通过该名称指定所属的匹配规则"
                >
                  <Input :id="`m-name-${i}`" v-model="m.name" class="font-mono" />
                </FormField>
                <FormField
                  label="域名"
                  :for="`m-host-${i}`"
                  hint="精确域名，或以 *.example.com 形式通配子域名"
                >
                  <Input
                    :id="`m-host-${i}`"
                    v-model="m.host"
                    class="font-mono"
                    placeholder="api.example.com"
                  />
                </FormField>
                <FormField label="请求方法" :for="`m-method-${i}`">
                  <NativeSelect :id="`m-method-${i}`" v-model="m.method" class="w-full">
                    <NativeSelectOption v-for="x in methods" :key="x" :value="x">{{
                      x || '任意'
                    }}</NativeSelectOption>
                  </NativeSelect>
                </FormField>
                <FormField
                  label="路径"
                  :for="`m-path-${i}`"
                  optional
                  hint="正则表达式，仅匹配路径（不含查询参数）；留空表示匹配全部路径"
                >
                  <Input
                    :id="`m-path-${i}`"
                    v-model="m.path"
                    class="font-mono"
                    placeholder="^/api/user/"
                  />
                </FormField>
              </div>
            </div>
            <div>
              <Button
                variant="outline"
                @click="
                  doc.matchers.push({
                    name: `rule${doc.matchers.length + 1}`,
                    host: '',
                    path: '',
                    method: '',
                  })
                "
              >
                <PlusIcon /> 添加匹配规则
              </Button>
            </div>
          </template>

          <!-- 字段 -->
          <template v-else-if="section === 'fields'">
            <HintText> 客户端仅回传已声明的字段；服务端按相同的读取规则复核。 </HintText>
            <div v-for="(f, i) in doc.fields" :key="i" class="grid gap-4 rounded-xl border p-4">
              <div class="flex flex-wrap items-center gap-2">
                <IconTile :icon="KeyRoundIcon" size="sm" />
                <span class="font-mono text-sm font-medium">{{ f.key || '未命名' }}</span>
                <span v-if="f.name" class="text-muted-foreground text-sm">{{ f.name }}</span>
                <StatusBadge :tone="typeOf(f).tone">{{ typeOf(f).short }}</StatusBadge>
                <StatusBadge v-if="f.sensitive" tone="warning" :icon="LockIcon">敏感</StatusBadge>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="ml-auto"
                  aria-label="删除字段"
                  @click="doc.fields.splice(i, 1)"
                >
                  <Trash2Icon />
                </Button>
              </div>

              <div class="grid gap-4 sm:grid-cols-2">
                <FormField
                  label="字段标识"
                  :for="`f-key-${i}`"
                  hint="以小写字母开头，可包含小写字母、数字和 _；作为输出变量中的引用名"
                >
                  <Input
                    :id="`f-key-${i}`"
                    v-model="f.key"
                    class="font-mono"
                    placeholder="如 token"
                  />
                </FormField>
                <FormField label="名称" :for="`f-name-${i}`" optional>
                  <Input :id="`f-name-${i}`" v-model="f.name" placeholder="如 登录令牌" />
                </FormField>
                <FormField
                  label="匹配规则"
                  :for="`f-matcher-${i}`"
                  full
                  hint="仅从命中该匹配规则的请求中读取"
                >
                  <NativeSelect :id="`f-matcher-${i}`" v-model="f.matcher" class="w-full">
                    <NativeSelectOption value="">任一匹配规则</NativeSelectOption>
                    <NativeSelectOption
                      v-for="m in doc.matchers.filter((x) => x.name)"
                      :key="m.name"
                      :value="m.name"
                      >{{ matcherLabel(m.name) }}</NativeSelectOption
                    >
                  </NativeSelect>
                </FormField>
                <FormField label="读取位置" :for="`f-in-${i}`">
                  <NativeSelect :id="`f-in-${i}`" v-model="f.in" class="w-full">
                    <NativeSelectOption v-for="o in inOptions" :key="o.value" :value="o.value">{{
                      o.label
                    }}</NativeSelectOption>
                  </NativeSelect>
                </FormField>
                <FormField
                  :label="f.in === 'regex' ? '正则表达式' : '键名 / 路径'"
                  :for="`f-path-${i}`"
                  :hint="inOf(f).hint"
                >
                  <Input
                    :id="`f-path-${i}`"
                    v-model="f.path"
                    class="font-mono"
                    :placeholder="inOf(f).placeholder"
                  />
                </FormField>
                <FormField v-if="f.in === 'regex'" label="匹配对象" :for="`f-regex-on-${i}`">
                  <NativeSelect :id="`f-regex-on-${i}`" v-model="f.regex_on" class="w-full">
                    <NativeSelectOption value="url">完整 URL</NativeSelectOption>
                    <NativeSelectOption value="body">请求正文</NativeSelectOption>
                  </NativeSelect>
                </FormField>
              </div>

              <div class="grid gap-4 border-t pt-4 sm:grid-cols-2">
                <FormField
                  label="类型"
                  :for="`f-type-${i}`"
                  :hint="
                    f.type === 'jwt'
                      ? '解析 exp：到期前提醒，已过期的值自动驳回'
                      : '类型不符的值自动驳回'
                  "
                >
                  <NativeSelect :id="`f-type-${i}`" v-model="f.type" class="w-full">
                    <NativeSelectOption v-for="o in typeOptions" :key="o.value" :value="o.value">{{
                      o.label
                    }}</NativeSelectOption>
                  </NativeSelect>
                </FormField>
                <div class="grid grid-cols-2 gap-3">
                  <FormField label="最小长度" :for="`f-min-${i}`" optional>
                    <Input
                      :id="`f-min-${i}`"
                      v-model.number="f.min_len"
                      type="number"
                      min="0"
                      placeholder="不限"
                    />
                  </FormField>
                  <FormField label="最大长度" :for="`f-max-${i}`" optional>
                    <Input
                      :id="`f-max-${i}`"
                      v-model.number="f.max_len"
                      type="number"
                      min="0"
                      placeholder="不限"
                    />
                  </FormField>
                </div>
                <FormField
                  label="校验正则"
                  :for="`f-pattern-${i}`"
                  optional
                  full
                  hint="不匹配的值自动驳回"
                >
                  <Input
                    :id="`f-pattern-${i}`"
                    v-model="f.pattern"
                    class="font-mono"
                    placeholder="如 ^[A-Za-z0-9._-]+$"
                  />
                </FormField>
                <FormField full hint="加密存储；界面中脱敏显示，查看原值记入审计；通知中不含字段值">
                  <Label class="font-normal"><Switch v-model="f.sensitive" /> 敏感字段</Label>
                </FormField>
              </div>

              <!-- 样例取值结果 -->
              <div
                v-if="fieldResult(f)"
                class="flex items-start gap-2 rounded-lg border px-3 py-2 text-sm"
                :class="
                  fieldResult(f)!.ok && !fieldResult(f)!.invalid
                    ? 'border-success/25 bg-success/5'
                    : fieldResult(f)!.ok
                      ? 'border-warning/30 bg-warning/5'
                      : 'border-border bg-muted/40'
                "
              >
                <FlaskConicalIcon class="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <div class="grid min-w-0 gap-0.5">
                  <template v-if="fieldResult(f)!.ok">
                    <span class="font-mono break-all">{{ fieldResult(f)!.value }}</span>
                    <span v-if="fieldResult(f)!.invalid" class="text-warning text-xs"
                      >校验未通过：{{ fieldResult(f)!.invalid }}</span
                    >
                    <span
                      v-else-if="fieldResult(f)!.expires_at"
                      class="text-muted-foreground text-xs"
                      >过期时间 {{ fmtTime(fieldResult(f)!.expires_at!) }}</span
                    >
                  </template>
                  <span v-else class="text-muted-foreground">{{ fieldResult(f)!.error }}</span>
                </div>
              </div>
            </div>
            <div>
              <Button variant="outline" @click="addField"><PlusIcon /> 添加字段</Button>
            </div>
          </template>

          <!-- 输出变量 -->
          <template v-else-if="section === 'outputs'">
            <HintText>
              推送至目的地的环境变量；值由字段与固定文本组成，字段引用格式为
              <code class="bg-muted rounded px-1 font-mono">{{ examples.ref }}</code
              >，例如
              <code class="bg-muted rounded px-1 font-mono">{{ examples.output }}</code>
            </HintText>
            <div v-for="(o, i) in doc.outputs" :key="i" class="grid gap-4 rounded-xl border p-4">
              <div class="flex items-center gap-2">
                <IconTile :icon="VariableIcon" size="sm" />
                <span class="font-mono text-sm font-medium">{{ o.name || '未命名' }}</span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="ml-auto"
                  aria-label="删除输出变量"
                  @click="doc.outputs!.splice(i, 1)"
                >
                  <Trash2Icon />
                </Button>
              </div>
              <div class="grid gap-4 sm:grid-cols-[1fr_2fr]">
                <FormField label="变量名" :for="`o-name-${i}`">
                  <Input
                    :id="`o-name-${i}`"
                    v-model="o.name"
                    class="font-mono"
                    placeholder="如 MY_APP_TOKEN"
                  />
                </FormField>
                <FormField label="变量值" :for="`o-tpl-${i}`">
                  <Input :id="`o-tpl-${i}`" v-model="o.template" class="font-mono" />
                  <div class="flex flex-wrap items-center gap-1">
                    <span class="text-muted-foreground text-xs">插入字段</span>
                    <Button
                      v-for="f in doc.fields.filter((x) => x.key)"
                      :key="f.key"
                      variant="outline"
                      size="xs"
                      class="font-mono"
                      @click="insertVar(i, f.key)"
                    >
                      {{ f.key }}
                    </Button>
                    <span
                      v-if="!doc.fields.some((x) => x.key)"
                      class="text-muted-foreground text-xs"
                      >（暂无字段）</span
                    >
                  </div>
                </FormField>
              </div>
              <div
                v-if="outputResult(o.name)"
                class="flex items-start gap-2 rounded-lg border px-3 py-2 text-sm"
                :class="
                  outputResult(o.name)!.ok
                    ? 'border-success/25 bg-success/5'
                    : 'border-border bg-muted/40'
                "
              >
                <FlaskConicalIcon class="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <span v-if="outputResult(o.name)!.ok" class="font-mono break-all">{{
                  outputResult(o.name)!.value
                }}</span>
                <span v-else class="text-muted-foreground">{{ outputResult(o.name)!.error }}</span>
              </div>
            </div>
            <div>
              <Button variant="outline" @click="doc.outputs!.push({ name: '', template: '' })">
                <PlusIcon /> 添加输出变量
              </Button>
            </div>
          </template>

          <!-- 有效性检测 -->
          <template v-else-if="section === 'probe'">
            <div class="flex items-start gap-3 rounded-xl border p-4">
              <IconTile :icon="RadarIcon" :tone="probeOn ? 'success' : 'neutral'" size="sm" />
              <div class="mr-auto grid gap-0.5">
                <span class="text-sm font-medium">有效性检测</span>
                <span class="text-muted-foreground text-xs"
                  >使用新值请求校验接口，判定通过后生效，用于过滤已失效的登录态</span
                >
              </div>
              <Label class="shrink-0 font-normal"><Switch v-model="probeOn" /> 启用检测</Label>
            </div>
            <template v-if="probeOn">
              <div class="grid gap-4 sm:grid-cols-[8rem_1fr]">
                <FormField label="请求方法" for="p-method">
                  <NativeSelect id="p-method" v-model="probe.method" class="w-full">
                    <NativeSelectOption
                      v-for="x in ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD']"
                      :key="x"
                      :value="x"
                      >{{ x }}</NativeSelectOption
                    >
                  </NativeSelect>
                </FormField>
                <FormField label="接口地址" for="p-url">
                  <Input
                    id="p-url"
                    v-model="probe.url"
                    class="font-mono"
                    placeholder="https://api.example.com/user/info"
                  />
                </FormField>
              </div>
              <FormField label="请求头" optional>
                <KeyValueEditor v-model="probe.headers" />
                <template #hint>
                  接口地址、请求头、请求体支持字段引用
                  <code class="font-mono">{{ examples.ref }}</code
                  >，如
                  <code class="font-mono">{{ examples.header }}</code>
                </template>
              </FormField>
              <FormField label="请求体" for="p-body" optional>
                <Textarea id="p-body" v-model="probe.body" rows="3" class="font-mono text-xs" />
              </FormField>
              <FormField label="成功条件" for="p-expect">
                <div class="flex flex-wrap gap-1">
                  <Button
                    v-for="p in expectPresets"
                    :key="p.value"
                    :variant="probe.expect === p.value ? 'secondary' : 'outline'"
                    size="xs"
                    @click="probe.expect = p.value"
                    >{{ p.label }}</Button
                  >
                </div>
                <Input id="p-expect" v-model="probe.expect" class="font-mono" />
                <template #hint>
                  布尔表达式。变量：<code class="font-mono">status</code> 状态码、<code
                    class="font-mono"
                    >headers</code
                  >
                  响应头（小写键）、<code class="font-mono">body</code> 响应正文、<code
                    class="font-mono"
                    >json</code
                  >
                  解析后的 JSON
                </template>
              </FormField>
              <div class="grid gap-4 sm:grid-cols-[1fr_8rem]">
                <FormField
                  label="身份表达式"
                  for="p-identity"
                  optional
                  hint="从响应中读取身份标识，如 json.data.userId"
                >
                  <Input
                    id="p-identity"
                    v-model="probe.identity"
                    class="font-mono"
                    placeholder="json.data.userId"
                  />
                </FormField>
                <FormField label="超时（秒）" for="p-timeout">
                  <Input
                    id="p-timeout"
                    v-model.number="probe.timeout"
                    type="number"
                    min="0"
                    max="30"
                    placeholder="5"
                  />
                </FormField>
              </div>
              <div class="grid gap-2 rounded-xl border p-4">
                <div class="flex flex-wrap items-center gap-3">
                  <Button
                    variant="outline"
                    :disabled="probing || !sample.url.trim()"
                    @click="runProbe"
                  >
                    <LoaderCircleIcon v-if="probing" class="animate-spin" /><PlayIcon v-else />
                    使用样例测试
                  </Button>
                  <span v-if="!sample.url.trim()" class="text-muted-foreground text-xs"
                    >需填写样例请求</span
                  >
                  <span v-else class="text-muted-foreground text-xs"
                    >使用样例读取的值发起真实请求</span
                  >
                </div>
                <div
                  v-if="probeResult"
                  class="flex items-start gap-2 text-sm"
                  :class="probeResult.ok ? 'text-success' : 'text-destructive'"
                >
                  <CircleCheckIcon v-if="probeResult.ok" class="mt-0.5 size-4 shrink-0" />
                  <CircleXIcon v-else class="mt-0.5 size-4 shrink-0" />
                  <span>
                    <template v-if="probeResult.error">检测失败：{{ probeResult.error }}</template>
                    <template v-else
                      >{{ probeResult.ok ? '检测通过' : '检测未通过' }}（{{
                        probeResult.detail
                      }}）<template v-if="probeResult.identity">
                        · 身份
                        <span class="font-mono">{{ probeResult.identity }}</span></template
                      ></template
                    >
                  </span>
                </div>
              </div>
            </template>
          </template>

          <!-- 窄屏时样例请求作为一个分区 -->
          <!-- 接入清单：加入订阅、在客户端里安装、推送到目的地、等第一次回传 -->
          <template v-else-if="section === 'setup'">
            <HintText v-if="isNew">保存来源后，可在此处将其加入订阅并配置推送规则。</HintText>
            <SourceSetup
              v-else
              :source="{ id: Number(id), key: doc.key, name: doc.name, enabled: !!doc.enabled }"
            />
          </template>

          <ReuseSample v-else-if="section === 'sample'" />
        </div>
      </div>

      <!-- 样例请求（宽屏常驻） -->
      <aside v-if="wide" class="min-h-0 overflow-y-auto border-l">
        <div class="p-4"><ReuseSample /></div>
      </aside>
    </div>

    <template #footer-start>
      <template v-if="!isNew">
        <Button variant="outline" as="a" :href="`/api/v1/sources/${id}/export`" download>
          <DownloadIcon /> 导出
        </Button>
        <ConfirmButton
          title="删除来源？"
          description="将同时删除该来源的登录态与推送规则，订阅的 MITM 列表随之更新。此操作不可撤销。"
          confirm-text="删除"
          variant="outline"
          class="text-destructive hover:bg-destructive/10 hover:text-destructive"
          @confirm="remove"
        >
          <Trash2Icon /> 删除
        </ConfirmButton>
      </template>
    </template>
    <template #footer>
      <Button variant="outline" @click="requestClose">取消</Button>
      <Button :disabled="saving || !loaded" @click="save">
        <LoaderCircleIcon v-if="saving" class="animate-spin" /> 保存
      </Button>
    </template>
  </DialogShell>

  <AlertDialog v-model:open="confirmClose">
    <AlertDialogContent class="sm:max-w-md">
      <AlertDialogHeader class="flex flex-row items-start gap-3 text-left">
        <IconTile :icon="TriangleAlertIcon" tone="warning" />
        <div class="grid gap-1">
          <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
          <AlertDialogDescription>未保存的修改将丢失。</AlertDialogDescription>
        </div>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>继续编辑</AlertDialogCancel>
        <AlertDialogAction @click="emit('close')">放弃修改</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
