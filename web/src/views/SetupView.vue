<script setup lang="ts">
import {
  CheckIcon,
  CircleCheckIcon,
  CircleXIcon,
  CopyIcon,
  DatabaseIcon,
  DownloadIcon,
  GlobeIcon,
  KeyRoundIcon,
  ListChecksIcon,
  LoaderCircleIcon,
  ShieldCheckIcon,
  TriangleAlertIcon,
  UserRoundIcon,
} from '@lucide/vue'
import axios from 'axios'
import { computed, onMounted, reactive, ref, watch, type Component } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import BrandMark from '@/components/BrandMark.vue'
import FormField from '@/components/FormField.vue'
import IconTile from '@/components/IconTile.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { copyText } from '@/format'
import { useFormCheck } from '@/lib/form'
import { markSetupDone } from '@/router'

interface SetupStatus {
  needs_config: boolean
  needs_admin: boolean
}

// 配置文件或环境变量里已有的数据库与主密钥，向导据此回填；连接串里的密码已打码。
interface Source {
  from: '' | 'file' | 'env' | 'default'
  env?: string
}
interface Preset {
  driver: string
  dsn: string
  driver_from: Source
  dsn_from: Source
  master_key: boolean
  master_key_from: Source
  config_path: string
}

interface Step {
  id: 'token' | 'database' | 'admin' | 'endpoint' | 'review' | 'key'
  title: string
  description: string
  icon: Component
}

const router = useRouter()
const status = ref<SetupStatus | null>(null)
const step = ref(0)
const busy = ref(false)
const token = ref('')
const preset = ref<Preset | null>(null)

const sourceText = (src: Source) =>
  src.from === 'env'
    ? `环境变量 ${src.env}`
    : src.from === 'file'
      ? `配置文件 ${preset.value?.config_path ?? ''}`
      : ''

const db = reactive({ driver: 'sqlite', dsn: 'ferry.db' })
const admin = reactive({ username: 'admin', password: '', confirm: '' })
const endpoint = reactive({ name: '局域网', base_url: window.location.origin })
const result = ref<{ master_key?: string; config_path?: string } | null>(null)
const backedUp = ref(false)
const dbTest = ref<{ ok: boolean; message: string } | null>(null)

const drivers = [
  { value: 'sqlite', label: 'SQLite', hint: '推荐 · 单文件' },
  { value: 'postgres', label: 'PostgreSQL', hint: '' },
  { value: 'mysql', label: 'MySQL', hint: '' },
]
const dsnHint: Record<string, string> = {
  sqlite: '相对路径基于服务的工作目录',
  postgres: 'postgres://用户:密码@主机:5432/库名?sslmode=disable',
  mysql: '用户:密码@tcp(主机:3306)/库名?charset=utf8mb4&parseTime=True&loc=UTC',
}

// 向导接口用一次性令牌认证，令牌在服务启动日志里。直接用 axios，避免全局的 401 跳转。
function setupApi() {
  return axios.create({
    baseURL: '/api/v1',
    headers: { 'Content-Type': 'application/json', 'X-Ferry-Setup-Token': token.value.trim() },
  })
}

function wrap(e: unknown): string {
  if (axios.isAxiosError(e)) {
    return (e.response?.data as { error?: string })?.error ?? '请求失败'
  }
  return errorText(e)
}

const steps = computed<Step[]>(() => {
  const s: Step[] = [
    { id: 'token', title: '初始化令牌', description: '验证服务器所有权', icon: KeyRoundIcon },
  ]
  if (status.value?.needs_config) {
    s.push({ id: 'database', title: '数据库', description: '数据存储位置', icon: DatabaseIcon })
  }
  s.push(
    { id: 'admin', title: '管理员', description: '后台登录账号', icon: UserRoundIcon },
    { id: 'endpoint', title: '访问地址', description: '手机访问地址', icon: GlobeIcon },
    { id: 'review', title: '确认', description: '核对配置', icon: ListChecksIcon },
  )
  if (status.value?.needs_config && !preset.value?.master_key) {
    s.push({ id: 'key', title: '主密钥', description: '备份加密密钥', icon: ShieldCheckIcon })
  }
  return s
})
const current = computed(() => steps.value[step.value])
const needsConfig = computed(() => !!status.value?.needs_config)

const passwordError = computed(() => (admin.password.length < 8 ? '至少 8 位' : ''))
const confirmError = computed(() => (admin.confirm !== admin.password ? '两次密码不一致' : ''))

// 手机通过这个地址访问 Ferry，回环地址在手机上指向手机自己。
const loopback = computed(() => {
  try {
    const h = new URL(endpoint.base_url.trim()).hostname
    return h === 'localhost' || h.endsWith('.localhost') || h === '[::1]' || h.startsWith('127.')
  } catch {
    return false
  }
})
const urlValid = computed(() => /^https?:\/\/[^/\s]+/.test(endpoint.base_url.trim()))

// 汇总页里把连接串中的密码换成星号。
const maskedDsn = computed(() =>
  db.dsn.replace(/:\/\/([^:/@]+):[^@]*@/, '://$1:****@').replace(/^([^:/@]+):[^@]*@/, '$1:****@'),
)

watch(
  () => [db.driver, db.dsn],
  () => (dbTest.value = null),
)

onMounted(async () => {
  try {
    status.value = (await axios.get<SetupStatus>('/api/v1/setup/status')).data
    if (!status.value.needs_config && !status.value.needs_admin) {
      markSetupDone()
      await router.replace('/login')
    }
  } catch {
    toast.error('无法连接服务')
  }
})

// 每一步点“下一步”时校验，缺的项就地标出来。
const tokenCheck = useFormCheck(() => ({ token: !token.value.trim() && '请填写初始化令牌' }))
const adminCheck = useFormCheck(() => ({
  username: !admin.username.trim() && '请填写用户名',
  password: passwordError.value,
  confirm: confirmError.value,
}))
const endpointCheck = useFormCheck(() => ({
  name: !endpoint.name.trim() && '请填写名称',
  base_url: !urlValid.value && '地址需以 http:// 或 https:// 开头，后面跟主机名',
}))
const keyCheck = useFormCheck(() => ({
  backedUp: !backedUp.value && '先把主密钥复制或下载下来，再勾选这一项',
}))

async function next() {
  const check = { token: tokenCheck, admin: adminCheck, endpoint: endpointCheck }[
    current.value.id as 'token' | 'admin' | 'endpoint'
  ]
  if (check && !check.ok()) return
  if (current.value.id === 'token' && !(await loadPreset())) return
  step.value++
}

// 令牌这一步顺带取回填信息：令牌不对时就地报错，对了就把已有的数据库填进下一步。
async function loadPreset() {
  busy.value = true
  try {
    const p = (await setupApi().get<Preset>('/setup/preset')).data
    preset.value = p
    if (p.driver) {
      db.driver = p.driver
      db.dsn = p.dsn
    }
    return true
  } catch (e) {
    toast.error(wrap(e))
    return false
  } finally {
    busy.value = false
  }
}

function pickDriver(v: unknown) {
  db.driver = String(v)
  db.dsn = db.driver === 'sqlite' ? 'ferry.db' : ''
}

async function testDB() {
  busy.value = true
  try {
    await setupApi().post('/setup/database', db)
    dbTest.value = { ok: true, message: '连接成功' }
  } catch (e) {
    dbTest.value = { ok: false, message: wrap(e) }
  } finally {
    busy.value = false
  }
}

async function finish() {
  busy.value = true
  try {
    const body: Record<string, unknown> = {
      admin: { username: admin.username.trim(), password: admin.password },
      endpoint: { name: endpoint.name.trim(), base_url: endpoint.base_url.trim() },
    }
    if (status.value?.needs_config) body.database = db
    result.value = (await setupApi().post('/setup', body)).data
    markSetupDone()
    if (result.value?.master_key) {
      step.value++
    } else {
      await done()
    }
  } catch (e) {
    toast.error(wrap(e))
  } finally {
    busy.value = false
  }
}

async function done() {
  if (!keyCheck.ok()) return
  toast.success('初始化完成')
  await router.replace('/login')
}

async function copyKey() {
  try {
    await copyText(result.value?.master_key ?? '')
    toast.success('已复制')
  } catch (e) {
    toast.error(errorText(e))
  }
}

function downloadKey() {
  const text = `Ferry 主密钥\n\n${result.value?.master_key ?? ''}\n\n配置文件：${result.value?.config_path ?? ''}\n`
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
  a.download = 'ferry-master-key.txt'
  a.click()
  URL.revokeObjectURL(a.href)
}
</script>

<template>
  <div class="mx-auto grid max-w-4xl gap-6 px-4 py-[6vh]">
    <div>
      <h1 class="flex items-center gap-3 text-2xl font-semibold"><BrandMark />初始化 Ferry</h1>
      <p class="text-muted-foreground mt-1 text-sm">首次启动的一次性配置</p>
    </div>

    <div v-if="status" class="grid items-start gap-6 md:grid-cols-[13rem_1fr]">
      <!-- 步骤：宽屏竖排，窄屏横向进度条 -->
      <ol class="hidden gap-1 md:grid">
        <li
          v-for="(s, i) in steps"
          :key="s.id"
          class="flex items-center gap-3 rounded-lg px-2 py-2"
          :class="i === step ? 'bg-muted' : ''"
        >
          <span
            class="flex size-7 shrink-0 items-center justify-center rounded-full border text-xs"
            :class="
              i < step
                ? 'bg-primary text-primary-foreground border-primary'
                : i === step
                  ? 'border-primary text-primary font-semibold'
                  : 'text-muted-foreground'
            "
          >
            <CheckIcon v-if="i < step" class="size-3.5" />
            <template v-else>{{ i + 1 }}</template>
          </span>
          <div class="min-w-0">
            <div
              class="text-sm"
              :class="i === step ? 'font-medium' : i > step ? 'text-muted-foreground' : ''"
            >
              {{ s.title }}
            </div>
            <div class="text-muted-foreground truncate text-xs">{{ s.description }}</div>
          </div>
        </li>
      </ol>
      <div class="grid gap-2 md:hidden">
        <div class="text-muted-foreground text-xs">第 {{ step + 1 }} / {{ steps.length }} 步</div>
        <div class="flex gap-1">
          <span
            v-for="(s, i) in steps"
            :key="s.id"
            class="h-1 flex-1 rounded-full"
            :class="i <= step ? 'bg-primary' : 'bg-muted'"
          />
        </div>
      </div>

      <Card>
        <CardHeader class="flex items-center gap-3 border-b">
          <IconTile :icon="current.icon" tone="primary" />
          <div>
            <h2 class="font-semibold">{{ current.title }}</h2>
            <p class="text-muted-foreground text-sm">
              <template v-if="current.id === 'token'">见服务启动日志，每次启动重新生成</template>
              <template v-else-if="current.id === 'database'">数据库类型与连接</template>
              <template v-else-if="current.id === 'admin'">管理后台登录账号</template>
              <template v-else-if="current.id === 'endpoint'"
                >手机拉取取值规则、回传登录态用的地址；之后可在系统设置里添加更多</template
              >
              <template v-else-if="current.id === 'review'">写入配置并创建管理员</template>
              <template v-else
                >初始化完成。敏感字段以主密钥加密，丢失后无法解密；仅显示一次</template
              >
            </p>
          </div>
        </CardHeader>

        <CardContent class="grid gap-5">
          <template v-if="current.id === 'token'">
            <pre
              class="bg-muted overflow-x-auto rounded-md px-3 py-2 font-mono text-xs"
            >WARN 尚未初始化 … setup_token=<b class="text-foreground">&lt;令牌&gt;</b></pre>
            <FormField label="初始化令牌" for="setup-token" :error="tokenCheck.errors.token">
              <Input
                id="setup-token"
                v-model="token"
                placeholder="setup_token 的值"
                class="font-mono"
                autocomplete="off"
                @keydown.enter="next"
              />
            </FormField>
          </template>

          <template v-else-if="current.id === 'database'">
            <FormField label="类型">
              <RadioGroup
                :model-value="db.driver"
                class="grid gap-2 sm:grid-cols-3"
                @update:model-value="pickDriver"
              >
                <Label
                  v-for="d in drivers"
                  :key="d.value"
                  class="has-data-[state=checked]:border-primary has-data-[state=checked]:bg-primary/5 flex cursor-pointer items-start gap-2 rounded-lg border p-3 font-normal"
                >
                  <RadioGroupItem :value="d.value" class="mt-0.5" />
                  <span class="grid gap-0.5">
                    <span class="font-medium">{{ d.label }}</span>
                    <span v-if="d.hint" class="text-muted-foreground text-xs">{{ d.hint }}</span>
                  </span>
                </Label>
              </RadioGroup>
            </FormField>
            <FormField
              :label="db.driver === 'sqlite' ? '文件路径' : '连接串'"
              for="dsn"
              :hint="dsnHint[db.driver]"
            >
              <Input id="dsn" v-model="db.dsn" class="font-mono" autocomplete="off" />
            </FormField>
            <p
              v-if="preset?.driver && sourceText(preset.dsn_from)"
              class="text-muted-foreground -mt-2 text-sm"
            >
              已按{{ sourceText(preset.dsn_from) }}回填，密码显示为 ••••，不改就沿用原值。
              可以修改：保存后写进配置文件，以后以配置文件为准。
            </p>
            <div class="flex flex-wrap items-center gap-3">
              <Button variant="outline" :disabled="busy || !db.dsn.trim()" @click="testDB">
                <LoaderCircleIcon v-if="busy" class="animate-spin" />
                测试连接
              </Button>
              <span
                v-if="dbTest"
                class="flex items-center gap-1.5 text-sm"
                :class="dbTest.ok ? 'text-success' : 'text-destructive'"
              >
                <CircleCheckIcon v-if="dbTest.ok" class="size-4" />
                <CircleXIcon v-else class="size-4" />
                {{ dbTest.message }}
              </span>
            </div>
          </template>

          <template v-else-if="current.id === 'admin'">
            <FormField label="用户名" for="admin-user" :error="adminCheck.errors.username">
              <Input id="admin-user" v-model="admin.username" autocomplete="username" />
            </FormField>
            <div class="grid gap-5 sm:grid-cols-2">
              <FormField label="密码" for="admin-pass" :error="adminCheck.errors.password">
                <Input
                  id="admin-pass"
                  v-model="admin.password"
                  type="password"
                  placeholder="至少 8 位"
                  autocomplete="new-password"
                />
              </FormField>
              <FormField label="确认密码" for="admin-confirm" :error="adminCheck.errors.confirm">
                <Input
                  id="admin-confirm"
                  v-model="admin.confirm"
                  type="password"
                  autocomplete="new-password"
                  @keydown.enter="next"
                />
              </FormField>
            </div>
          </template>

          <template v-else-if="current.id === 'endpoint'">
            <div class="grid gap-5 sm:grid-cols-[10rem_1fr]">
              <FormField label="名称" for="ep-name" :error="endpointCheck.errors.name">
                <Input id="ep-name" v-model="endpoint.name" />
              </FormField>
              <FormField label="地址" for="ep-url" :error="endpointCheck.errors.base_url">
                <Input
                  id="ep-url"
                  v-model="endpoint.base_url"
                  class="font-mono"
                  placeholder="http://192.168.1.10:8080"
                />
              </FormField>
            </div>
            <Alert v-if="loopback" class="border-warning/40 text-warning">
              <TriangleAlertIcon />
              <AlertTitle>回环地址对手机不可达</AlertTitle>
              <AlertDescription>改用局域网 IP 或域名，如 http://192.168.1.10:8080</AlertDescription>
            </Alert>
          </template>

          <template v-else-if="current.id === 'review'">
            <dl class="divide-y rounded-lg border text-sm">
              <div v-if="needsConfig" class="grid gap-1 px-4 py-3 sm:grid-cols-[7rem_1fr]">
                <dt class="text-muted-foreground">数据库</dt>
                <dd class="min-w-0">
                  {{ drivers.find((d) => d.value === db.driver)?.label }}
                  <div class="text-muted-foreground font-mono text-xs break-all">
                    {{ maskedDsn }}
                  </div>
                </dd>
              </div>
              <div v-if="needsConfig" class="grid gap-1 px-4 py-3 sm:grid-cols-[7rem_1fr]">
                <dt class="text-muted-foreground">主密钥</dt>
                <dd class="min-w-0">
                  <template v-if="preset?.master_key">
                    沿用{{ sourceText(preset.master_key_from) }}里的主密钥
                    <div
                      v-if="preset.master_key_from.from === 'env'"
                      class="text-muted-foreground text-xs"
                    >
                      不会写进配置文件；以后去掉这个环境变量，需要把主密钥写进配置文件
                    </div>
                  </template>
                  <template v-else>完成后生成，只显示一次</template>
                </dd>
              </div>
              <div class="grid gap-1 px-4 py-3 sm:grid-cols-[7rem_1fr]">
                <dt class="text-muted-foreground">管理员</dt>
                <dd>{{ admin.username }}</dd>
              </div>
              <div class="grid gap-1 px-4 py-3 sm:grid-cols-[7rem_1fr]">
                <dt class="text-muted-foreground">访问地址</dt>
                <dd class="min-w-0">
                  {{ endpoint.name }}
                  <div class="text-muted-foreground font-mono text-xs break-all">
                    {{ endpoint.base_url }}
                  </div>
                </dd>
              </div>
            </dl>
          </template>

          <template v-else-if="current.id === 'key' && result?.master_key">
            <div class="bg-muted/50 grid gap-3 rounded-lg border p-4">
              <code class="font-mono text-sm break-all select-all">{{ result.master_key }}</code>
              <div class="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" @click="copyKey"><CopyIcon /> 复制</Button>
                <Button variant="outline" size="sm" @click="downloadKey"
                  ><DownloadIcon /> 下载 .txt</Button
                >
              </div>
            </div>
            <div class="grid gap-1 text-sm">
              <span class="text-muted-foreground">已写入配置文件，需一并备份</span>
              <code class="font-mono text-xs break-all">{{ result.config_path }}</code>
            </div>
            <FormField :error="keyCheck.errors.backedUp">
              <Label class="font-normal"><Checkbox v-model="backedUp" /> 已备份主密钥</Label>
            </FormField>
          </template>
        </CardContent>

        <CardFooter class="justify-end gap-2 border-t">
          <template v-if="current.id === 'key'">
            <Button @click="done">进入登录</Button>
          </template>
          <template v-else>
            <Button v-if="step > 0" variant="outline" :disabled="busy" @click="step--"
              >上一步</Button
            >
            <Button v-if="current.id !== 'review'" @click="next">下一步</Button>
            <Button v-else :disabled="busy" @click="finish">
              <LoaderCircleIcon v-if="busy" class="animate-spin" />
              完成初始化
            </Button>
          </template>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
