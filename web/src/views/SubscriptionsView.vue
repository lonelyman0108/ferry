<script setup lang="ts">
import {
  ClockIcon,
  DownloadIcon,
  EllipsisIcon,
  EyeIcon,
  FileCodeIcon,
  GlobeIcon,
  LinkIcon,
  LockIcon,
  LockOpenIcon,
  PlusIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
} from '@lucide/vue'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import BrandIcon from '@/components/BrandIcon.vue'
import ChoiceCards from '@/components/ChoiceCards.vue'
import DialogShell from '@/components/DialogShell.vue'
import EmptyState from '@/components/EmptyState.vue'
import FormDialog from '@/components/FormDialog.vue'
import FormField from '@/components/FormField.vue'
import FormSection from '@/components/FormSection.vue'
import HintText from '@/components/HintText.vue'
import IconTile from '@/components/IconTile.vue'
import ListRow from '@/components/ListRow.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ToggleChips from '@/components/ToggleChips.vue'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { copyText, fmtAgo, fmtTime } from '@/format'
import { useFormCheck } from '@/lib/form'
import { icons } from '@/lib/icons'
import { useOverview } from '@/stores/overview'
import {
  clients,
  supportsSealed,
  type Endpoint,
  type SourceSummary,
  type Subscription,
  type SubscriptionInput,
  subscriptionStatus,
} from '@/types'

import SubscriptionInstallDialog from './SubscriptionInstallDialog.vue'

const overview = useOverview()
const list = ref<Subscription[] | null>(null)
const endpoints = ref<Endpoint[]>([])
const sources = ref<SourceSummary[]>([])
const editing = ref<number | null>(null)
const showForm = ref(false)
const installFor = ref<Subscription | null>(null)
const busy = ref(false)

function blank(): SubscriptionInput {
  return {
    name: '',
    client: 'surge',
    endpoint_id: endpoints.value[0]?.id ?? 0,
    fallback_id: 0,
    all_sources: false,
    source_keys: [],
    transport_mode: 'sealed',
    script_url: '',
  }
}
const form = reactive<SubscriptionInput>(blank())
// QX 不支持加密，选它时自动改成明文。
watch(
  () => form.client,
  (c) => {
    if (!supportsSealed(c)) form.transport_mode = 'plain'
  },
)
const sealedSwitch = computed({
  get: () => form.transport_mode === 'sealed',
  set: (v: boolean) => (form.transport_mode = v ? 'sealed' : 'plain'),
})
const endpointURL = (id: number | null) => endpoints.value.find((e) => e.id === id)?.base_url ?? ''
const isHTTP = (id: number | null) => endpointURL(id).startsWith('http://')

const clientOptions = clients.map((c) => ({ ...c, brand: c.value }))
const endpointName = (id: number | null) => endpoints.value.find((e) => e.id === id)?.name ?? '—'
const sourceName = (k: string) => sources.value.find((s) => s.key === k)?.name ?? k

async function load() {
  try {
    const [s, e, src] = await Promise.all([
      api.get<Subscription[]>('/subscriptions'),
      api.get<Endpoint[]>('/endpoints'),
      api.get<SourceSummary[]>('/sources'),
    ])
    list.value = s.data
    endpoints.value = e.data
    sources.value = src.data
  } catch (e) {
    toast.error(errorText(e))
  }
}
// 从总览的收件箱带 ?install=<订阅 ID> 进来时，直接打开那个订阅的安装引导。
const route = useRoute()
const router = useRouter()
onMounted(async () => {
  await load()
  const id = Number(route.query.install)
  const target = id ? list.value?.find((s) => s.id === id) : undefined
  if (target) {
    installFor.value = target
    void router.replace({ query: { ...route.query, install: undefined } })
  }
})

const noEndpoints = '暂无访问地址，请在“系统设置”中添加'
const formCheck = useFormCheck(() => ({
  name: !form.name.trim() && '请填写名称',
  endpoint_id: !form.endpoint_id && (endpoints.value.length ? '请选择主地址' : noEndpoints),
  sources:
    !form.all_sources &&
    !form.source_keys.length &&
    (sources.value.length
      ? '请至少选择一个来源，或开启“包含全部来源”'
      : '暂无来源，可开启“包含全部来源”'),
}))

function openCreate() {
  formCheck.reset()
  editing.value = null
  Object.assign(form, blank())
  showForm.value = true
}

function openEdit(s: Subscription) {
  formCheck.reset()
  editing.value = s.id
  Object.assign(form, {
    name: s.name,
    client: s.client,
    endpoint_id: s.endpoint_id,
    fallback_id: s.fallback_id ?? 0,
    all_sources: s.all_sources,
    source_keys: [...s.source_keys],
    transport_mode: s.transport.mode,
    script_url: s.script_url,
  })
  showForm.value = true
}

const editingMode = computed(() => list.value?.find((x) => x.id === editing.value)?.transport.mode)
const changedMode = computed(() => !!editingMode.value && editingMode.value !== form.transport_mode)

const fallbackOptions = computed(() => endpoints.value.filter((e) => e.id !== form.endpoint_id))

// 新建的订阅保存后直接打开安装引导。
async function submit() {
  if (!formCheck.ok()) return
  busy.value = true
  try {
    const body = {
      ...form,
      fallback_id: form.fallback_id || null,
      source_keys: form.all_sources ? [] : form.source_keys,
    }
    if (editing.value) {
      await api.put(`/subscriptions/${editing.value}`, body)
      toast.success('已保存')
    } else {
      installFor.value = (await api.post<Subscription>('/subscriptions', body)).data
    }
    showForm.value = false
    await Promise.all([load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function remove(s: Subscription) {
  try {
    await api.delete(`/subscriptions/${s.id}`)
    toast.success('已删除')
    await Promise.all([load(), overview.load()])
  } catch (e) {
    toast.error(errorText(e))
  }
}

const sealed = (s: Subscription) => s.transport.mode === 'sealed'

// 安装引导里刷新到的状态同步回列表。
function onInstallUpdated(s: Subscription) {
  const i = list.value?.findIndex((x) => x.id === s.id) ?? -1
  if (list.value && i >= 0) list.value[i] = s
}
function closeInstall() {
  installFor.value = null
  void overview.load()
}

async function copy(text: string, label: string) {
  try {
    await copyText(text)
    toast.success(`已复制${label}`)
  } catch (e) {
    toast.error(errorText(e))
  }
}

// ---- 预览订阅文件 ----
const preview = ref<{
  name: string
  file_name: string
  content: string
  hostnames: string[]
} | null>(null)
async function openPreview(s: Subscription) {
  try {
    const r = (
      await api.get<{ file_name: string; content: string; hostnames: string[] }>(
        `/subscriptions/${s.id}/preview`,
      )
    ).data
    preview.value = { name: s.name, ...r }
  } catch (e) {
    toast.error(errorText(e))
  }
}

// ---- 更换令牌、密钥：从“更多”菜单里发起，先确认 ----
const confirm = ref<{ title: string; description: string; action: string; run: () => void } | null>(
  null,
)

function askRotate(s: Subscription) {
  confirm.value = {
    title: '更换令牌？',
    description: '原链接将立即失效，需在客户端中重新安装订阅。',
    action: '更换令牌',
    run: () => void rotate(s),
  }
}
async function rotate(s: Subscription) {
  try {
    const v = (await api.post<Subscription>(`/subscriptions/${s.id}/rotate`, {})).data
    toast.success('令牌已更换，请在客户端中重新安装订阅')
    await load()
    installFor.value = v
  } catch (e) {
    toast.error(errorText(e))
  }
}

function askRotateKey(s: Subscription) {
  confirm.value = {
    title: '更换传输密钥？',
    description: '原密钥将立即失效，需在客户端的模块参数中填入新密钥，模块无需重新安装。',
    action: '更换密钥',
    run: () => void rotateKey(s),
  }
}
async function rotateKey(s: Subscription) {
  try {
    const v = (await api.post<Subscription>(`/subscriptions/${s.id}/transport-key/rotate`, {})).data
    toast.success('传输密钥已更换，请在客户端中填入新密钥')
    await load()
    installFor.value = v
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <PageHeader>
    <template #actions>
      <Button @click="openCreate"><PlusIcon /> 新建订阅</Button>
    </template>
  </PageHeader>

  <ListSkeleton v-if="!list" :rows="3" />
  <EmptyState
    v-else-if="!list.length && !endpoints.length"
    :icon="GlobeIcon"
    title="暂无访问地址"
    description="订阅需指定访问地址，设备通过该地址拉取读取规则、回传登录态。"
  >
    <Button as-child
      ><RouterLink to="/settings"><PlusIcon /> 添加访问地址</RouterLink></Button
    >
  </EmptyState>
  <EmptyState
    v-else-if="!list.length"
    :icon="icons.subscription"
    title="暂无订阅"
    description="每台设备上的每个代理客户端需单独安装订阅，安装后开始回传登录态。"
  >
    <Button @click="openCreate"><PlusIcon /> 新建订阅</Button>
  </EmptyState>

  <div class="grid gap-3">
    <ListRow v-for="s in list" :key="s.id">
      <template #icon><BrandIcon :brand="s.client" /></template>
      <template #title
        ><router-link :to="`/subscriptions/${s.id}`" class="hover:underline" title="查看链路">{{
          s.name
        }}</router-link></template
      >
      <template #badges>
        <StatusBadge :tone="subscriptionStatus[s.status].tone" dot>{{
          subscriptionStatus[s.status].label
        }}</StatusBadge>
        <StatusBadge v-if="s.outdated" tone="warning" :icon="RefreshCwIcon">待更新</StatusBadge>
        <template v-if="sealed(s)">
          <StatusBadge v-if="s.transport.verified_at" tone="success" :icon="LockIcon"
            >加密传输</StatusBadge
          >
          <StatusBadge v-else tone="warning" :icon="LockIcon">待填写密钥</StatusBadge>
        </template>
        <StatusBadge v-else-if="isHTTP(s.endpoint_id)" tone="warning" :icon="LockOpenIcon"
          >明文传输</StatusBadge
        >
      </template>
      <template #description>
        <span class="flex items-center gap-1"
          ><GlobeIcon class="size-3.5" />{{ endpointName(s.endpoint_id)
          }}<template v-if="s.fallback_id">
            / 备用 {{ endpointName(s.fallback_id) }}</template
          ></span
        >
        <span class="flex items-center gap-1"
          ><component :is="icons.source" class="size-3.5" />{{
            s.all_sources ? '全部来源' : s.source_keys.map(sourceName).join('、')
          }}</span
        >
        <span v-if="s.last_seen_at" class="flex items-center gap-1" :title="fmtTime(s.last_seen_at)"
          ><ClockIcon class="size-3.5" />最近连接 {{ fmtAgo(s.last_seen_at) }}</span
        >
      </template>
      <template #actions>
        <RowActions
          :name="s.name"
          delete-title="删除订阅？"
          delete-description="令牌将立即失效，已安装的客户端将无法回传登录态。"
          @edit="openEdit(s)"
          @remove="remove(s)"
        >
          <Button
            :variant="s.outdated || s.status === 'not_installed' ? 'default' : 'outline'"
            size="sm"
            @click="installFor = s"
          >
            <DownloadIcon /> {{ s.outdated ? '更新订阅' : '安装订阅' }}
          </Button>
          <DropdownMenu :modal="false">
            <DropdownMenuTrigger as-child>
              <Button variant="ghost" size="icon-sm" aria-label="更多" title="更多"
                ><EllipsisIcon
              /></Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="w-44">
              <DropdownMenuItem @click="copy(s.links.subscription_url, '订阅链接')"
                ><LinkIcon /> 复制订阅链接</DropdownMenuItem
              >
              <DropdownMenuItem @click="copy(s.links.script_url, '脚本链接')"
                ><FileCodeIcon /> 复制脚本链接</DropdownMenuItem
              >
              <DropdownMenuItem @click="openPreview(s)"><EyeIcon /> 预览订阅文件</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem @click="askRotate(s)"
                ><component :is="icons.rotateToken" /> 更换令牌</DropdownMenuItem
              >
              <DropdownMenuItem v-if="sealed(s)" @click="askRotateKey(s)"
                ><LockIcon /> 更换密钥</DropdownMenuItem
              >
            </DropdownMenuContent>
          </DropdownMenu>
        </RowActions>
      </template>
    </ListRow>
  </div>

  <SubscriptionInstallDialog
    :subscription="installFor"
    :sources="sources"
    @close="closeInstall"
    @updated="onInstallUpdated"
  />

  <DialogShell
    :open="!!preview"
    :title="`预览：${preview?.name ?? ''}`"
    :description="preview?.file_name"
    :icon="FileCodeIcon"
    size="lg"
    @close="preview = null"
  >
    <div v-if="preview" class="grid gap-3">
      <div class="grid gap-1.5">
        <span class="text-muted-foreground text-xs">MITM 域名</span>
        <div class="flex flex-wrap gap-1.5">
          <code
            v-for="h in preview.hostnames"
            :key="h"
            class="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
            >{{ h }}</code
          >
          <span v-if="!preview.hostnames.length" class="text-muted-foreground text-sm">无</span>
        </div>
      </div>
      <!-- 长行（脚本规则、hostname 列表）自动折行，不靠横向滚动，macOS 默认隐藏滚动条时也看得全 -->
      <pre
        class="bg-muted/50 max-h-[50vh] overflow-y-auto rounded-lg border p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap"
        >{{ preview.content }}</pre>
    </div>
    <template #footer>
      <Button variant="outline" @click="preview = null">关闭</Button>
    </template>
  </DialogShell>

  <AlertDialog :open="!!confirm" @update:open="(v: boolean) => !v && (confirm = null)">
    <AlertDialogContent class="sm:max-w-md">
      <AlertDialogHeader class="flex flex-row items-start gap-3 text-left">
        <IconTile :icon="TriangleAlertIcon" tone="warning" />
        <div class="grid gap-1">
          <AlertDialogTitle>{{ confirm?.title }}</AlertDialogTitle>
          <AlertDialogDescription>{{ confirm?.description }}</AlertDialogDescription>
        </div>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>取消</AlertDialogCancel>
        <AlertDialogAction @click="confirm?.run()">{{ confirm?.action }}</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>

  <FormDialog
    v-model:open="showForm"
    :title="editing ? '编辑订阅' : '新建订阅'"
    :description="editing ? form.name : '每台设备上的每个代理客户端对应一份订阅，令牌相互独立'"
    :brand="form.client"
    :icon="icons.subscription"
    size="lg"
    :busy="busy"
    @submit="submit"
  >
    <FormSection title="基本" :cols="1">
      <FormField label="名称" for="sub-name" :error="formCheck.errors.name">
        <Input id="sub-name" v-model="form.name" placeholder="设备 · 客户端，如 iPhone · Surge" />
      </FormField>
      <FormField label="客户端">
        <ChoiceCards v-model="form.client" :options="clientOptions" :cols="3" label="客户端" />
      </FormField>
    </FormSection>

    <FormSection title="访问地址" description="设备拉取读取规则、回传登录态所用的地址">
      <FormField
        label="主地址"
        for="sub-endpoint"
        :hint="endpoints.length ? '' : noEndpoints"
        :error="formCheck.errors.endpoint_id"
      >
        <NativeSelect id="sub-endpoint" v-model.number="form.endpoint_id" class="w-full">
          <NativeSelectOption v-for="e in endpoints" :key="e.id" :value="e.id">{{
            e.name
          }}</NativeSelectOption>
        </NativeSelect>
      </FormField>
      <FormField label="备用地址" for="sub-fallback" optional>
        <NativeSelect id="sub-fallback" v-model.number="form.fallback_id" class="w-full">
          <NativeSelectOption :value="0">无</NativeSelectOption>
          <NativeSelectOption v-for="e in fallbackOptions" :key="e.id" :value="e.id">{{
            e.name
          }}</NativeSelectOption>
        </NativeSelect>
      </FormField>
      <FormField
        label="脚本地址"
        for="sub-script"
        optional
        full
        :hint="
          isHTTP(form.endpoint_id) && !form.script_url.trim()
            ? '主地址使用 http，脚本在传输过程中可能被篡改；建议填写 https 地址（与 Ferry 版本一致的 ferry.js）'
            : '留空时使用主地址下的 /script/ferry.js'
        "
      >
        <Input
          id="sub-script"
          v-model="form.script_url"
          placeholder="https://…/ferry.js"
          class="font-mono text-xs"
        />
      </FormField>
    </FormSection>

    <FormSection
      title="传输加密"
      :description="form.transport_mode === 'sealed' ? '已开启' : '未开启'"
      :cols="1"
    >
      <Label class="font-normal"
        ><Switch v-model="sealedSwitch" :disabled="!supportsSealed(form.client)" />
        加密回传内容（访问地址为 http 时，内容也无法被读取或篡改）</Label
      >
      <HintText v-if="!supportsSealed(form.client)">
        Quantumult X 不支持模块参数，无法配置传输密钥，仅支持明文传输；建议使用 https 访问地址。
      </HintText>
      <HintText v-else-if="form.transport_mode === 'sealed'">
        保存后将打开安装引导，按步骤将传输密钥填入客户端的模块参数。
      </HintText>
      <HintText v-else-if="isHTTP(form.endpoint_id)">
        主地址使用 http，未加密时同一网络中的其他设备可读取回传的登录态。
      </HintText>
      <HintText v-if="editing && changedMode">
        切换传输方式后，需在客户端中重新安装订阅。
      </HintText>
    </FormSection>

    <FormSection
      title="来源范围"
      :description="form.all_sources ? '全部来源' : `已选 ${form.source_keys.length} 个`"
      :cols="1"
    >
      <Label class="font-normal"
        ><Switch v-model="form.all_sources" /> 包含全部来源（含后续新增）</Label
      >
      <FormField
        v-if="!form.all_sources"
        :hint="sources.length ? '' : '暂无来源。开启“包含全部来源”后，新增的来源将自动纳入'"
        :error="formCheck.errors.sources"
      >
        <ToggleChips
          v-if="sources.length"
          v-model="form.source_keys"
          :options="sources.map((s) => ({ value: s.key, label: s.name }))"
          label="来源"
        />
      </FormField>
    </FormSection>
  </FormDialog>
</template>
