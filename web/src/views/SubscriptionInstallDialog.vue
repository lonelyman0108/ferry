<script setup lang="ts">
import { CheckIcon, DownloadIcon, EyeIcon, LoaderCircleIcon, TriangleAlertIcon } from '@lucide/vue'
import { computed, onUnmounted, ref, watch } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import CopyField from '@/components/CopyField.vue'
import DialogShell from '@/components/DialogShell.vue'
import QrCode from '@/components/QrCode.vue'
import { Button } from '@/components/ui/button'
import { fmtAgo } from '@/format'
import { cn } from '@/lib/utils'
import { clients, type SourceSummary, type Subscription } from '@/types'

// 安装引导：按客户端分四步（安装、开 MITM、填传输密钥、打开 App 验证），每步用这个客户端里的叫法。
// 打开期间每 3 秒刷新一次订阅，下载、连通的状态就地变化；刷新到的结果通过 updated 交给列表。
const props = defineProps<{ subscription: Subscription | null; sources: SourceSummary[] }>()
const emit = defineEmits<{ close: []; updated: [s: Subscription] }>()

const sub = ref<Subscription | null>(null)
const key = ref('')
watch(
  () => props.subscription,
  (s) => {
    sub.value = s
    key.value = ''
  },
  { immediate: true },
)

const client = computed(() => clients.find((c) => c.value === sub.value?.client))
const term = computed(() => client.value?.term ?? '模块')
const sealed = computed(() => sub.value?.transport.mode === 'sealed')
const sourceNames = computed(() => {
  const s = sub.value
  if (!s) return ''
  return props.sources
    .filter((x) => x.enabled && (s.all_sources || s.source_keys.includes(x.key)))
    .map((x) => x.name)
    .join('、')
})

let timer: ReturnType<typeof setInterval> | undefined
watch(
  () => props.subscription?.id,
  (id) => {
    clearInterval(timer)
    if (id) timer = setInterval(refresh, 3000)
  },
  { immediate: true },
)
onUnmounted(() => clearInterval(timer))

async function refresh() {
  if (!sub.value) return
  try {
    const s = (await api.get<Subscription>(`/subscriptions/${sub.value.id}`)).data
    sub.value = s
    emit('updated', s)
  } catch {
    // 定时刷新失败不打扰，下一轮再试
  }
}

// 传输密钥只在点了“显示”之后才向后端要，后端会写一条审计。
async function revealKey() {
  if (!sub.value) return
  try {
    key.value = (
      await api.post<{ key: string }>(`/subscriptions/${sub.value.id}/transport-key/reveal`, {})
    ).data.key
  } catch (e) {
    toast.error(errorText(e))
  }
}

// 管理界面本身经不可信的 http 打开时，显示出来的密钥在网络上也是明文。
const adminInsecure = (() => {
  const { protocol, hostname: h } = window.location
  if (protocol !== 'http:') return false
  return !(
    h === 'localhost' ||
    h.endsWith('.local') ||
    /^(127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(h) ||
    h === '[::1]'
  )
})()

const keyWhere: Record<string, string> = {
  surge: '在 Surge 的模块列表中找到该模块，编辑参数，将 key 设为下方的密钥。',
  shadowrocket: '在 Shadowrocket 的模块列表中找到该模块，编辑参数，将 key 设为下方的密钥。',
  loon: '在 Loon 的插件列表中打开该插件，在参数中填写“传输密钥”，其余参数保持不变。',
  stash: '在 Stash 的覆写列表中打开该覆写，在“参数”中填写“传输密钥”。',
  egern: '在 Egern 的配置中，为该模块的引用添加 compat_arguments，将 key 设为下方的密钥。',
}

// 每一步是否已经完成：装好了、连通了、密钥验证过了。开 MITM 这一步看不到，连通了就算完成。
const done = computed(() => {
  const s = sub.value
  return {
    install: !!s && s.status !== 'not_installed' && !s.outdated,
    mitm: s?.status === 'connected',
    key: !!s?.transport.verified_at,
    verify: s?.status === 'connected',
  }
})
const stepClass = (ok: boolean) =>
  cn(
    'flex size-7 shrink-0 items-center justify-center rounded-full border text-xs font-semibold',
    ok ? 'border-success bg-success text-white' : 'text-muted-foreground',
  )
</script>

<template>
  <DialogShell
    :open="!!sub"
    :title="`在 ${client?.label ?? ''} 中安装`"
    :description="sub?.name"
    :brand="sub?.client"
    size="lg"
    @close="emit('close')"
  >
    <ol v-if="sub && client" class="grid gap-6">
      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.install)"
          ><CheckIcon v-if="done.install" class="size-4" /><template v-else>1</template></span
        >
        <div class="grid min-w-0 gap-3">
          <div>
            <div class="font-medium">安装{{ term }}</div>
            <p class="text-muted-foreground text-sm">
              在已安装 {{ client.label }} 的设备上打开以下链接，或使用相机扫描二维码。
            </p>
          </div>
          <div class="flex flex-wrap items-start gap-4">
            <QrCode :value="sub.links.install_url" class="w-36 shrink-0" />
            <div class="grid min-w-0 flex-1 basis-60 gap-2">
              <CopyField :value="sub.links.install_url" label="安装链接" />
              <div class="md:hidden">
                <Button as="a" :href="sub.links.install_url" size="sm"
                  ><DownloadIcon /> 在本机打开</Button
                >
              </div>
              <p v-if="sub.outdated" class="text-warning flex gap-1.5 text-sm">
                <TriangleAlertIcon class="mt-0.5 size-4 shrink-0" />
                MITM 域名已变更，需更新设备上的{{ term }}：重新打开上方链接，或在
                {{ client.label }} 中更新该{{ term }}。
              </p>
              <p
                v-else-if="sub.file_fetched_at"
                class="text-success flex items-center gap-1.5 text-sm"
              >
                <CheckIcon class="size-4 shrink-0" />{{ client.label }} 已下载订阅（{{
                  fmtAgo(sub.file_fetched_at)
                }}）
              </p>
            </div>
          </div>
        </div>
      </li>

      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.mitm)"
          ><CheckIcon v-if="done.mitm" class="size-4" /><template v-else>2</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div class="font-medium">开启 MITM 并信任证书</div>
          <p class="text-muted-foreground text-sm">
            在 {{ client.label }} 中开启 MITM，生成并安装 CA 证书，然后在 iOS 的“设置 → 通用 →
            关于本机 → 证书信任设置”中启用对该证书的完全信任。{{ term }}将自动添加以下域名：
          </p>
          <div v-if="sub.hostnames.length" class="flex flex-wrap gap-1.5">
            <code
              v-for="h in sub.hostnames"
              :key="h"
              class="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
              >{{ h }}</code
            >
          </div>
          <p v-else class="text-warning text-sm">该订阅未包含任何已启用的来源。</p>
        </div>
      </li>

      <li v-if="sealed" class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.key)"
          ><CheckIcon v-if="done.key" class="size-4" /><template v-else>3</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div class="font-medium">填写传输密钥</div>
          <p class="text-muted-foreground text-sm">
            {{ keyWhere[sub.client] }}密钥不包含在订阅文件中，填写前不回传登录态。
          </p>
          <CopyField v-if="key" :value="key" label="传输密钥" />
          <Button v-else variant="outline" size="sm" class="justify-self-start" @click="revealKey"
            ><EyeIcon /> 显示密钥</Button
          >
          <p v-if="key && adminInsecure" class="text-warning text-xs">
            当前通过 http 访问管理界面，密钥在网络传输中可能被截获；建议改用 https
            或在局域网内操作。
          </p>
          <p
            v-if="sub.transport.verified_at"
            class="text-success flex items-center gap-1.5 text-sm"
          >
            <CheckIcon class="size-4 shrink-0" />已收到使用该密钥加密的请求（{{
              fmtAgo(sub.transport.verified_at)
            }}）
          </p>
        </div>
      </li>

      <li class="grid grid-cols-[1.75rem_1fr] gap-3">
        <span :class="stepClass(done.verify)"
          ><CheckIcon v-if="done.verify" class="size-4" /><template v-else>{{
            sealed ? 4 : 3
          }}</template></span
        >
        <div class="grid min-w-0 gap-2">
          <div class="font-medium">打开 App 验证</div>
          <p class="text-muted-foreground text-sm">
            在设备上打开{{ sourceNames ? `“${sourceNames}”对应的` : '对应的' }} App。脚本首次连接
            Ferry 后，状态将变为“已连通”。
          </p>
          <div
            class="flex items-center gap-2 rounded-lg border p-3 text-sm"
            :class="done.verify ? 'border-success/30 bg-success/5' : ''"
          >
            <template v-if="done.verify">
              <CheckIcon class="text-success size-4" />已连通 · 最近连接
              {{ fmtAgo(sub.last_seen_at) }}
            </template>
            <template v-else>
              <LoaderCircleIcon class="text-muted-foreground size-4 animate-spin" />等待设备连接…
            </template>
          </div>
        </div>
      </li>
    </ol>
    <template #footer>
      <Button variant="outline" @click="emit('close')">关闭</Button>
    </template>
  </DialogShell>
</template>
