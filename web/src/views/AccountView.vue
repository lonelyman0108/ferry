<script setup lang="ts">
import {
  KeyRoundIcon,
  LockKeyholeIcon,
  MonitorIcon,
  MoonIcon,
  PaletteIcon,
  PlusIcon,
  SunIcon,
} from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import { setThemeMode, themeMode, type ThemeMode } from '@/lib/theme'
import ChoiceCards from '@/components/ChoiceCards.vue'
import ConfirmButton from '@/components/ConfirmButton.vue'
import CopyField from '@/components/CopyField.vue'
import EmptyState from '@/components/EmptyState.vue'
import FormDialog from '@/components/FormDialog.vue'
import FormField from '@/components/FormField.vue'
import HintText from '@/components/HintText.vue'
import IconTile from '@/components/IconTile.vue'
import ListRow from '@/components/ListRow.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fmtAgo, fmtTime } from '@/format'
import { useFormCheck } from '@/lib/form'
import { useAuth } from '@/stores/auth'
import type { ApiToken } from '@/types'

// 当前登录用户自己的设置：登录密码和 API 令牌。系统级的配置（访问地址、审计日志）在系统设置里。
const route = useRoute()
const router = useRouter()
const auth = useAuth()
const tabs = ['password', 'tokens', 'appearance']

// 深浅色存在这台浏览器里，换一台设备要重新选。
const themeOptions = [
  {
    value: 'system' as const,
    label: '跟随系统',
    description: '随系统深浅色设置自动切换',
    icon: MonitorIcon,
  },
  { value: 'light' as const, label: '浅色', icon: SunIcon },
  { value: 'dark' as const, label: '深色', icon: MoonIcon },
]
const theme = computed<ThemeMode>({ get: () => themeMode.value, set: (v) => setThemeMode(v) })
const tab = ref(
  typeof route.query.tab === 'string' && tabs.includes(route.query.tab)
    ? route.query.tab
    : 'password',
)
const busy = ref(false)

function pickTab(v: string | number) {
  tab.value = String(v)
  void router.replace({ query: { tab: tab.value } })
}

// ---- 修改密码 ----
const pw = reactive({ old: '', next: '', confirm: '' })
const pwCheck = useFormCheck(() => ({
  old: !pw.old && '请填写当前密码',
  next: pw.next.length < 8 && '新密码至少 8 个字符',
  confirm: pw.next !== pw.confirm && '两次输入的新密码不一致',
}))

async function changePassword() {
  if (!pwCheck.ok()) return
  busy.value = true
  try {
    await api.post('/auth/password', { old_password: pw.old, new_password: pw.next })
    Object.assign(pw, { old: '', next: '', confirm: '' })
    pwCheck.reset()
    toast.success('密码已修改')
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

// ---- API 令牌 ----
const tokens = ref<ApiToken[] | null>(null)
const tokenName = ref('')
const tokenOpen = ref(false)
const tokenCheck = useFormCheck(() => ({ name: !tokenName.value.trim() && '请填写名称' }))

function openToken() {
  tokenName.value = ''
  tokenCheck.reset()
  tokenOpen.value = true
}
const newToken = ref('')

async function loadTokens() {
  tokens.value = (await api.get<ApiToken[]>('/tokens')).data
}

async function createToken() {
  if (!tokenCheck.ok()) return
  busy.value = true
  try {
    newToken.value = (
      await api.post<{ token: string }>('/tokens', { name: tokenName.value })
    ).data.token
    tokenOpen.value = false
    await loadTokens()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function revokeToken(t: ApiToken) {
  try {
    await api.delete(`/tokens/${t.id}`)
    toast.success('已吊销')
    await loadTokens()
  } catch (e) {
    toast.error(errorText(e))
  }
}

onMounted(async () => {
  try {
    await loadTokens()
  } catch (e) {
    toast.error(errorText(e))
  }
})
</script>

<template>
  <PageHeader />

  <Tabs :model-value="tab" @update:model-value="pickTab">
    <div class="mb-2 flex flex-wrap items-center gap-2">
      <TabsList class="mr-auto">
        <TabsTrigger value="password"><LockKeyholeIcon /> 修改密码</TabsTrigger>
        <TabsTrigger value="tokens"><KeyRoundIcon /> API 令牌</TabsTrigger>
        <TabsTrigger value="appearance"><PaletteIcon /> 外观</TabsTrigger>
      </TabsList>
      <Button v-if="tab === 'tokens'" @click="openToken"
        ><PlusIcon /><span class="max-sm:sr-only">新建令牌</span></Button
      >
    </div>

    <TabsContent value="password">
      <Card class="max-w-md">
        <CardHeader>
          <CardTitle class="flex items-center gap-2"
            ><LockKeyholeIcon class="text-muted-foreground size-4" />修改密码 ·
            {{ auth.user?.username }}</CardTitle
          >
          <CardDescription>修改后其他登录会话将失效。</CardDescription>
        </CardHeader>
        <CardContent>
          <form class="grid gap-4" @submit.prevent="changePassword">
            <input
              type="text"
              :value="auth.user?.username"
              name="username"
              autocomplete="username"
              class="hidden"
              readonly
            />
            <FormField label="当前密码" for="pw-old" :error="pwCheck.errors.old">
              <Input id="pw-old" v-model="pw.old" type="password" autocomplete="current-password" />
            </FormField>
            <FormField label="新密码" for="pw-new" :error="pwCheck.errors.next">
              <Input
                id="pw-new"
                v-model="pw.next"
                type="password"
                autocomplete="new-password"
                placeholder="至少 8 个字符"
              />
            </FormField>
            <FormField label="确认新密码" for="pw-confirm" :error="pwCheck.errors.confirm">
              <Input
                id="pw-confirm"
                v-model="pw.confirm"
                type="password"
                autocomplete="new-password"
              />
            </FormField>
            <div><Button type="submit" :disabled="busy">修改密码</Button></div>
          </form>
        </CardContent>
      </Card>
    </TabsContent>

    <TabsContent value="appearance">
      <Card class="max-w-xl">
        <CardHeader>
          <CardTitle>深浅色</CardTitle>
          <CardDescription>仅对当前浏览器生效。</CardDescription>
        </CardHeader>
        <CardContent>
          <ChoiceCards v-model="theme" :options="themeOptions" :cols="3" label="深浅色" />
        </CardContent>
      </Card>
    </TabsContent>

    <TabsContent value="tokens" class="grid gap-3">
      <HintText>
        API 令牌为只读权限，可读取来源、身份、历史值与审计记录；不支持写入，不返回敏感原值。认证头：
        <code class="bg-muted rounded px-1 font-mono">Authorization: Bearer &lt;token&gt;</code>
      </HintText>
      <Alert v-if="newToken" class="border-warning/40 bg-warning/5">
        <AlertDescription class="grid gap-2">
          <p>新令牌仅显示一次，请立即复制：</p>
          <CopyField :value="newToken" label="令牌" />
        </AlertDescription>
      </Alert>
      <ListSkeleton v-if="!tokens" :rows="2" />
      <EmptyState
        v-else-if="!tokens.length"
        :icon="KeyRoundIcon"
        title="暂无 API 令牌"
        description="供监控脚本等只读调用方使用；未使用 API 时无需创建。"
      >
        <Button @click="openToken"><PlusIcon /> 新建令牌</Button>
      </EmptyState>
      <template v-else>
        <ListRow v-for="t in tokens" :key="t.id" :dim="!!t.revoked_at">
          <template #icon><IconTile :icon="KeyRoundIcon" tone="slate" /></template>
          <template #title>{{ t.name }}</template>
          <template #badges>
            <StatusBadge
              v-if="t.revoked_at"
              tone="neutral"
              dot
              :title="`吊销于 ${fmtTime(t.revoked_at)}`"
              >已吊销</StatusBadge
            >
          </template>
          <template #description>
            <span class="font-mono">{{ t.prefix }}…</span>
            <span>创建于 {{ fmtTime(t.created_at) }}</span>
            <span :title="t.last_used_at ? fmtTime(t.last_used_at) : ''">{{
              t.last_used_at ? `最近使用 ${fmtAgo(t.last_used_at)}` : '从未使用'
            }}</span>
          </template>
          <template v-if="!t.revoked_at" #actions>
            <ConfirmButton
              title="吊销令牌？"
              description="令牌将立即失效，且无法恢复。"
              confirm-text="吊销"
              variant="outline"
              size="sm"
              class="text-destructive hover:bg-destructive/10 hover:text-destructive"
              @confirm="revokeToken(t)"
            >
              吊销
            </ConfirmButton>
          </template>
        </ListRow>
      </template>
    </TabsContent>
  </Tabs>

  <FormDialog
    v-model:open="tokenOpen"
    title="新建令牌"
    description="只读权限，生成后仅显示一次"
    :icon="KeyRoundIcon"
    tone="slate"
    size="sm"
    submit-text="生成令牌"
    :busy="busy"
    @submit="createToken"
  >
    <FormField label="名称" for="token-name" :error="tokenCheck.errors.name">
      <Input id="token-name" v-model="tokenName" placeholder="如 monitor" />
    </FormField>
  </FormDialog>
</template>
