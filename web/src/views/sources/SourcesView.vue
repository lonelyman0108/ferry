<script setup lang="ts">
import {
  BoxesIcon,
  ChevronRightIcon,
  FileUpIcon,
  KeyRoundIcon,
  PlusIcon,
  RouteIcon,
  SparklesIcon,
  VariableIcon,
} from '@lucide/vue'
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import DialogShell from '@/components/DialogShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import FormField from '@/components/FormField.vue'
import FormDialog from '@/components/FormDialog.vue'
import IconTile from '@/components/IconTile.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import SourceEditor from './SourceEditor.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { fmtAgo } from '@/format'
import { useFormCheck } from '@/lib/form'
import { useOverview } from '@/stores/overview'
import type { Example, SourceSummary, SourceView, Subscription } from '@/types'

const props = defineProps<{ editing?: string }>()
const router = useRouter()
const route = useRoute()
const list = ref<SourceSummary[] | null>(null)
const examples = ref<Example[]>([])
const showImport = ref(false)
const showExamples = ref(false)
const yaml = ref('')
const busy = ref(false)
const yamlCheck = useFormCheck(() => ({
  yaml: !yaml.value.trim() && '请粘贴 YAML 内容或选择文件',
}))

function openImport() {
  yamlCheck.reset()
  showImport.value = true
}

// 卡片上的接入状态：读取它的订阅数来自订阅列表，推送规则数和最近回传来自总览。
const overview = useOverview()
const subs = ref<Subscription[]>([])
const subsOf = (s: SourceSummary) =>
  subs.value.filter((x) => x.all_sources || x.source_keys.includes(s.key)).length
const statOf = (s: SourceSummary) => overview.data?.sources.find((x) => x.id === s.id)

async function load() {
  try {
    const [l, sb] = await Promise.all([
      api.get<SourceSummary[]>('/sources'),
      api.get<Subscription[]>('/subscriptions'),
      overview.load(),
    ])
    list.value = l.data
    subs.value = sb.data
  } catch (e) {
    toast.error(errorText(e))
  }
}
onMounted(load)

// 新建的来源保存后换成它的地址，编辑器随之按新 id 重新加载。
async function onSaved(id: number) {
  await load()
  // 新建的来源保存后进入它的详情，并打开接入清单。
  if (props.editing === 'new' && id)
    await router.replace({ path: `/sources/${id}`, query: { edit: 'setup' } })
}

// 卡片上的启用开关：来源按整份文档读写，先取完整定义再改 enabled 保存。
const toggling = ref(0)
async function toggle(s: SourceSummary, enabled: boolean) {
  toggling.value = s.id
  try {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { id, updated_at, ...doc } = (await api.get<SourceView>(`/sources/${s.id}`)).data
    await api.put(`/sources/${s.id}`, { ...doc, enabled })
    s.enabled = enabled
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = 0
  }
}

async function openExamples() {
  showExamples.value = true
  try {
    examples.value = (await api.get<Example[]>('/examples')).data
  } catch (e) {
    toast.error(errorText(e))
  }
}

async function importExample(key: string) {
  busy.value = true
  try {
    const s = (await api.post<SourceView>(`/examples/${key}/import`, {})).data
    toast.success(`已导入「${s.name}」`)
    await router.push({ path: `/sources/${s.id}`, query: { edit: 'basic' } })
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function readFile(ev: Event) {
  const f = (ev.target as HTMLInputElement).files?.[0]
  if (f) yaml.value = await f.text()
}

async function importYAML() {
  if (!yamlCheck.ok()) return
  busy.value = true
  try {
    const s = (await api.post<SourceView>('/sources/import', { yaml: yaml.value })).data
    toast.success(`已导入「${s.name}」`)
    showImport.value = false
    await router.push({ path: `/sources/${s.id}`, query: { edit: 'basic' } })
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <PageHeader>
    <template #actions>
      <Button variant="outline" @click="openExamples"><SparklesIcon /> 从示例导入</Button>
      <Button variant="outline" @click="openImport"><FileUpIcon /> 导入 YAML</Button>
      <Button @click="router.push('/sources/new')"><PlusIcon /> 新建来源</Button>
    </template>
  </PageHeader>

  <ListSkeleton v-if="!list" :rows="3" class="md:grid-cols-2 xl:grid-cols-3" item-class="h-40" />
  <EmptyState
    v-else-if="!list.length"
    :icon="BoxesIcon"
    title="暂无来源"
    description="来源定义目标 App 的请求匹配、字段读取规则与输出变量。可从内置示例导入。"
  >
    <Button @click="openExamples"><SparklesIcon /> 从示例导入</Button>
    <Button variant="outline" @click="router.push('/sources/new')"><PlusIcon /> 新建来源</Button>
  </EmptyState>

  <div v-else-if="list" class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
    <div
      v-for="s in list"
      :key="s.id"
      role="link"
      tabindex="0"
      class="group bg-card ring-foreground/10 hover:ring-primary/40 focus-visible:ring-ring flex cursor-pointer flex-col gap-4 rounded-xl p-4 text-left ring-1 transition outline-none hover:shadow-sm"
      @click="router.push(`/sources/${s.id}`)"
      @keydown.enter="router.push(`/sources/${s.id}`)"
    >
      <div class="flex items-start gap-3">
        <IconTile :icon="BoxesIcon" :tone="s.enabled ? 'violet' : 'neutral'" />
        <div class="min-w-0 flex-1" :class="s.enabled ? '' : 'opacity-60'">
          <div class="truncate font-semibold">{{ s.name }}</div>
          <div class="text-muted-foreground truncate font-mono text-xs">{{ s.key }}</div>
        </div>
        <RowActions
          :enabled="s.enabled"
          :editable="false"
          :toggling="toggling === s.id"
          @toggle="(v) => toggle(s, v)"
        />
      </div>
      <div
        class="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs"
        :class="s.enabled ? '' : 'opacity-60'"
      >
        <span class="flex items-center gap-1"
          ><RouteIcon class="size-3.5" />{{ s.matchers }} 条匹配规则</span
        >
        <span class="flex items-center gap-1"
          ><KeyRoundIcon class="size-3.5" />{{ s.fields }} 个字段</span
        >
        <span class="flex items-center gap-1"
          ><VariableIcon class="size-3.5" />{{ s.outputs }} 个输出变量</span
        >
      </div>
      <!-- 接入状态：没加入订阅、没有推送规则的标黄，点“接入清单”直接去补 -->
      <div class="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 border-t pt-3 text-xs">
        <span :class="subsOf(s) ? 'text-muted-foreground' : 'text-warning'">{{
          subsOf(s) ? `${subsOf(s)} 个订阅` : '未加入订阅'
        }}</span>
        <span :class="statOf(s)?.bindings ? 'text-muted-foreground' : 'text-warning'">{{
          statOf(s)?.bindings ? `${statOf(s)?.bindings} 条推送规则` : '无推送规则'
        }}</span>
        <span class="text-muted-foreground">{{
          statOf(s)?.last_seen_at ? `最近回传 ${fmtAgo(statOf(s)?.last_seen_at)}` : '未回传'
        }}</span>
        <button
          type="button"
          class="text-primary ml-auto flex items-center gap-0.5 hover:underline"
          @click.stop="router.push({ path: `/sources/${s.id}`, query: { edit: 'setup' } })"
        >
          接入清单<ChevronRightIcon class="size-3.5" />
        </button>
      </div>
    </div>
  </div>

  <DialogShell
    :open="showExamples"
    title="从示例导入"
    description="同一来源标识仅可导入一次"
    :icon="SparklesIcon"
    tone="violet"
    @close="showExamples = false"
  >
    <div class="grid gap-2">
      <div v-for="e in examples" :key="e.key" class="flex items-start gap-3 rounded-lg border p-3">
        <IconTile :icon="BoxesIcon" tone="violet" size="sm" />
        <div class="min-w-0 flex-1">
          <div class="font-medium">
            {{ e.name }} <span class="text-muted-foreground font-mono text-xs">{{ e.key }}</span>
          </div>
          <p class="text-muted-foreground mt-1 text-sm whitespace-pre-line">
            {{ e.description }}
          </p>
        </div>
        <Button size="sm" :disabled="busy" @click="importExample(e.key)">导入</Button>
      </div>
    </div>
    <template #footer>
      <Button variant="outline" @click="showExamples = false">关闭</Button>
    </template>
  </DialogShell>

  <FormDialog
    v-model:open="showImport"
    title="导入 YAML"
    description="粘贴或上传来源定义文件"
    :icon="FileUpIcon"
    tone="violet"
    submit-text="导入"
    size="lg"
    :busy="busy"
    @submit="importYAML"
  >
    <FormField label="文件" for="yaml-file" optional>
      <input
        id="yaml-file"
        type="file"
        accept=".yaml,.yml,text/yaml"
        class="text-sm file:mr-3 file:rounded-md file:border file:bg-transparent file:px-2 file:py-1"
        @change="readFile"
      />
    </FormField>
    <FormField label="YAML" for="yaml-text" :error="yamlCheck.errors.yaml">
      <Textarea
        id="yaml-text"
        v-model="yaml"
        rows="20"
        class="font-mono text-xs"
        placeholder="key: example&#10;name: 示例&#10;matchers: …"
      />
    </FormField>
  </FormDialog>

  <SourceEditor
    v-if="editing"
    :id="editing === 'new' ? undefined : editing"
    :key="editing"
    :initial-section="typeof route.query.section === 'string' ? route.query.section : undefined"
    @close="router.push('/sources')"
    @saved="onSaved"
  />
</template>
