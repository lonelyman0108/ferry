<script setup lang="ts">
import {
  AlertTriangleIcon,
  CheckCircle2Icon,
  FlaskConicalIcon,
  GlobeIcon,
  NetworkIcon,
  ServerIcon,
} from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import ChoiceCards from '@/components/ChoiceCards.vue'
import FormField from '@/components/FormField.vue'
import HintText from '@/components/HintText.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useFormCheck } from '@/lib/form'
import { cn } from '@/lib/utils'
import type { NetworkMirrorPreset, NetworkMode, NetworkSettings, NetworkTestResult } from '@/types'

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const presets = ref<NetworkMirrorPreset[]>([])
const testResult = ref<NetworkTestResult | null>(null)

const form = reactive<{
  mode: NetworkMode
  mirror_prefix: string
  proxy_url: string
}>({
  mode: 'direct',
  mirror_prefix: 'https://ghfast.top',
  proxy_url: '',
})

const modeOptions = [
  {
    value: 'direct' as const,
    label: '直连',
    description: '直接连接 GitHub 官方域名',
    icon: GlobeIcon,
  },
  {
    value: 'mirror' as const,
    label: '镜像前缀',
    description: '为 GitHub 链接添加镜像前缀',
    icon: ServerIcon,
  },
  {
    value: 'proxy' as const,
    label: '网络代理',
    description: '经 HTTP 或 SOCKS5 代理连接',
    icon: NetworkIcon,
  },
]

const selectedPreset = computed(() =>
  presets.value.find((p) => p.prefix === form.mirror_prefix.trim().replace(/\/+$/, '')),
)

const formCheck = useFormCheck(() => ({
  mirror_prefix:
    form.mode === 'mirror' &&
    (!form.mirror_prefix.trim()
      ? '请填写镜像前缀地址'
      : !form.mirror_prefix.trim().startsWith('https://') && '镜像前缀须为 https:// 地址'),
  proxy_url:
    form.mode === 'proxy' &&
    (!form.proxy_url.trim()
      ? '请填写代理地址'
      : !/^(https?|socks5h?):\/\//i.test(form.proxy_url.trim()) &&
        '代理地址须以 http://、https://、socks5:// 或 socks5h:// 开头'),
}))

async function loadSettings() {
  loading.value = true
  try {
    const { data } = await api.get<NetworkSettings>('/settings/network')
    form.mode = data.mode || 'direct'
    form.mirror_prefix = data.mirror_prefix || data.presets?.[0]?.prefix || 'https://ghfast.top'
    form.proxy_url = data.proxy_url || ''
    presets.value = data.presets || []
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loading.value = false
  }
}

function pickPreset(url: string) {
  form.mirror_prefix = url
  testResult.value = null
}

async function saveSettings() {
  if (!formCheck.ok()) return
  saving.value = true
  try {
    const { data } = await api.put<NetworkSettings>('/settings/network', {
      mode: form.mode,
      mirror_prefix: form.mirror_prefix.trim(),
      proxy_url: form.proxy_url.trim(),
    })
    form.mode = data.mode
    form.mirror_prefix = data.mirror_prefix || form.mirror_prefix
    form.proxy_url = data.proxy_url || ''
    presets.value = data.presets || presets.value
    toast.success('已保存')
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    saving.value = false
  }
}

async function testConnection() {
  if (!formCheck.ok()) return
  testing.value = true
  testResult.value = null
  try {
    const { data } = await api.post<NetworkTestResult>('/settings/network/test', {
      mode: form.mode,
      mirror_prefix: form.mirror_prefix.trim(),
      proxy_url: form.proxy_url.trim(),
    })
    testResult.value = data
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    testing.value = false
  }
}

onMounted(() => {
  void loadSettings()
})
</script>

<template>
  <PageHeader />

  <div class="grid gap-4">
    <HintText>
      仅作用于访问 GitHub 域名（github.com、raw.githubusercontent.com、objects.githubusercontent.com
      等），其他地址保持直连。
    </HintText>

    <ListSkeleton v-if="loading" :rows="3" />
    <Card v-else class="max-w-2xl">
      <CardHeader>
        <CardTitle>GitHub 连接方式</CardTitle>
        <CardDescription
          >用于检查版本更新、拉取来源仓库索引与 GitHub 在线来源链接。</CardDescription
        >
      </CardHeader>
      <CardContent>
        <form class="grid gap-5" @submit.prevent="saveSettings">
          <FormField label="连接模式">
            <ChoiceCards
              v-model="form.mode"
              :options="modeOptions"
              :cols="3"
              label="连接模式"
              @update:model-value="testResult = null"
            />
          </FormField>

          <template v-if="form.mode === 'mirror'">
            <FormField label="预置镜像">
              <div class="grid gap-2 sm:grid-cols-3">
                <button
                  v-for="p in presets"
                  :key="p.prefix"
                  type="button"
                  :class="
                    cn(
                      'flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition',
                      selectedPreset?.prefix === p.prefix
                        ? 'border-primary bg-primary/5 ring-primary/30 ring-2'
                        : 'hover:border-foreground/25 hover:bg-muted/40',
                    )
                  "
                  @click="pickPreset(p.prefix)"
                >
                  <span class="font-mono text-sm font-medium">{{ p.label }}</span>
                  <span class="text-muted-foreground text-xs leading-snug">{{ p.note }}</span>
                </button>
              </div>
            </FormField>

            <FormField
              label="镜像前缀地址"
              for="net-mirror-prefix"
              hint="可选择上方预置镜像，或填写自定义 https:// 镜像前缀地址"
              :error="formCheck.errors.mirror_prefix"
            >
              <Input
                id="net-mirror-prefix"
                v-model="form.mirror_prefix"
                class="font-mono"
                placeholder="https://ghfast.top"
                @input="testResult = null"
              />
            </FormField>
          </template>

          <template v-else-if="form.mode === 'proxy'">
            <FormField
              label="代理地址"
              for="net-proxy-url"
              hint="支持 http://、https://、socks5://、socks5h://；密码由主密钥加密保存"
              :error="formCheck.errors.proxy_url"
            >
              <Input
                id="net-proxy-url"
                v-model="form.proxy_url"
                class="font-mono"
                placeholder="socks5://127.0.0.1:1080 或 http://user:pass@127.0.0.1:7890"
                @input="testResult = null"
              />
            </FormField>
          </template>

          <div
            v-if="testResult"
            :class="
              cn(
                'flex items-start gap-2.5 rounded-lg border p-3 text-xs',
                testResult.ok
                  ? 'border-success/40 bg-success/5 text-foreground'
                  : 'border-destructive/40 bg-destructive/5 text-destructive',
              )
            "
          >
            <CheckCircle2Icon v-if="testResult.ok" class="text-success mt-0.5 size-4 shrink-0" />
            <AlertTriangleIcon v-else class="text-destructive mt-0.5 size-4 shrink-0" />
            <div class="grid min-w-0 flex-1 gap-0.5">
              <span v-if="testResult.ok" class="font-medium">
                连接正常 · 耗时 {{ testResult.latency_ms }} ms
              </span>
              <span v-else class="font-medium break-words">
                {{ testResult.error }}（耗时 {{ testResult.latency_ms }} ms）
              </span>
              <span class="text-muted-foreground font-mono break-all">{{ testResult.target }}</span>
            </div>
          </div>

          <div class="flex flex-wrap items-center gap-2">
            <Button type="submit" :disabled="saving">
              {{ saving ? '正在保存…' : '保存设置' }}
            </Button>
            <Button type="button" variant="outline" :disabled="testing" @click="testConnection">
              <FlaskConicalIcon />
              {{ testing ? '正在测试…' : '测试连接' }}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
