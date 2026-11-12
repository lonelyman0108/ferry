<script setup lang="ts">
import {
  ArrowUpCircleIcon,
  CheckCircle2Icon,
  ClipboardCopyIcon,
  CpuIcon,
  DatabaseIcon,
  FileCogIcon,
  PackageIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
} from '@lucide/vue'
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { copyText, fmtAgo, fmtTime } from '@/format'
import type { ConfigSource, SystemInfo, UpdateInfo } from '@/types'

// 系统信息：版本、运行环境、数据库、配置从哪里来、数据量。打开时顺带查一次有没有新版本（服务端缓存一小时）。
// 页面上不出现任何密钥：连接串里的密码由后端打码，主密钥只说从哪里来。
const info = ref<SystemInfo | null>(null)
const update = ref<UpdateInfo | null>(null)
const checking = ref(false)

const repo = 'https://github.com/lonelyman0108/ferry'

async function load() {
  try {
    info.value = (await api.get<SystemInfo>('/system')).data
  } catch (e) {
    toast.error(errorText(e))
  }
}

async function checkUpdate(force = false) {
  checking.value = true
  try {
    update.value = (
      await api.get<UpdateInfo>('/system/update', { params: force ? { force: 1 } : {} })
    ).data
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    checking.value = false
  }
}

onMounted(() => {
  void load()
  void checkUpdate()
})

const isRelease = computed(() => /^v?\d+\.\d+\.\d+/.test(info.value?.build.version ?? ''))
const commitURL = computed(() =>
  info.value && /^[0-9a-f]{7,40}$/.test(info.value.build.commit)
    ? `${repo}/commit/${info.value.build.commit}`
    : '',
)

const driverName: Record<string, string> = {
  sqlite: 'SQLite',
  postgres: 'PostgreSQL',
  mysql: 'MySQL',
}

function size(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function uptime(since: string) {
  const sec = Math.floor((Date.now() - new Date(since).getTime()) / 1000)
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return d ? `${d} 天 ${h} 小时` : h ? `${h} 小时 ${m} 分钟` : `${m} 分钟`
}

function sourceText(s: ConfigSource) {
  if (s.from === 'env') return `环境变量 ${s.env}`
  if (s.from === 'file') return '配置文件'
  if (s.from === 'default') return '默认值'
  return '未设置'
}

const sections = computed(() => {
  const v = info.value
  if (!v) return []
  return [
    {
      title: '版本',
      icon: PackageIcon,
      rows: [
        { k: '版本号', v: v.build.version },
        { k: '提交', v: v.build.commit, mono: true, href: commitURL.value },
        { k: '构建时间', v: v.build.date === 'unknown' ? '—' : fmtTime(v.build.date) },
        { k: 'Go', v: v.build.go, mono: true },
      ],
    },
    {
      title: '运行环境',
      icon: CpuIcon,
      rows: [
        { k: '系统', v: `${v.build.os}/${v.build.arch}${v.runtime.container ? '（容器）' : ''}` },
        {
          k: '已运行',
          v: `${uptime(v.runtime.started_at)}，启动于 ${fmtTime(v.runtime.started_at)}`,
        },
        { k: '时区', v: v.runtime.timezone },
        { k: '监听地址', v: v.runtime.listen, mono: true },
      ],
    },
    {
      title: '数据库',
      icon: DatabaseIcon,
      rows: [
        {
          k: '类型',
          v: `${driverName[v.database.driver] ?? v.database.driver} ${v.database.version}`.trim(),
        },
        { k: v.database.driver === 'sqlite' ? '文件' : '连接', v: v.database.target, mono: true },
        ...(v.database.driver === 'sqlite' ? [{ k: '大小', v: size(v.database.size) }] : []),
      ],
    },
    {
      title: '配置来源',
      icon: FileCogIcon,
      rows: [
        {
          k: '配置文件',
          v: v.config.exists ? v.config.path : `${v.config.path}（不存在）`,
          mono: true,
        },
        { k: '监听地址', v: sourceText(v.config.sources.listen) },
        { k: '数据库', v: sourceText(v.config.sources.dsn) },
        { k: '主密钥', v: sourceText(v.config.sources.master_key) },
      ],
    },
  ]
})

const counts = computed(() => {
  const c = info.value?.counts
  if (!c) return []
  return [
    { k: '来源', v: c.sources },
    { k: '身份', v: c.identities },
    { k: '版本', v: c.versions },
    { k: '订阅', v: c.subscriptions },
    { k: '目的地', v: c.destinations },
    { k: '推送记录', v: c.pushes },
  ]
})

// 诊断信息：上面的内容拼成纯文本，方便贴进 issue；连接串已由后端打码，不含任何密钥。
async function copyDiagnostics() {
  const lines = ['Ferry 诊断信息', '']
  for (const s of sections.value) {
    lines.push(`## ${s.title}`)
    for (const r of s.rows) lines.push(`- ${r.k}：${r.v}`)
    lines.push('')
  }
  lines.push('## 数据量', counts.value.map((c) => `${c.k} ${c.v}`).join('，'))
  try {
    await copyText(lines.join('\n'))
    toast.success('诊断信息已复制，不含密钥')
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <PageHeader>
    <template #actions>
      <Button variant="outline" :disabled="!info" @click="copyDiagnostics"
        ><ClipboardCopyIcon /> 复制诊断信息</Button
      >
    </template>
  </PageHeader>

  <section
    class="bg-card mb-6 flex flex-wrap items-center gap-3 rounded-lg border p-4"
    aria-label="检查更新"
  >
    <template v-if="update?.newer">
      <ArrowUpCircleIcon class="text-primary size-5" />
      <span class="text-sm"
        >发现新版本 <b>{{ update.latest }}</b
        >，当前版本 {{ update.current }}</span
      >
      <a
        :href="update.url"
        target="_blank"
        rel="noopener"
        class="text-primary text-sm underline-offset-2 hover:underline"
        >查看更新说明</a
      >
    </template>
    <template v-else-if="update && !update.error && isRelease">
      <CheckCircle2Icon class="text-success size-5" />
      <span class="text-sm">已是最新版本 {{ update.current }}</span>
    </template>
    <template v-else-if="update">
      <ShieldAlertIcon class="text-muted-foreground size-5" />
      <span class="text-muted-foreground text-sm">
        <template v-if="update.error">检查更新：{{ update.error }}</template>
        <template v-else
          >当前为开发构建（{{ update.current }}），最新发布版本为 {{ update.latest }}</template
        >
      </span>
    </template>
    <span v-else class="text-muted-foreground text-sm">正在检查更新…</span>
    <span v-if="update" class="text-muted-foreground ml-auto text-xs"
      >最近检查 {{ fmtAgo(update.checked_at) }}</span
    >
    <Button
      variant="ghost"
      size="sm"
      :class="!update && 'ml-auto'"
      :disabled="checking"
      @click="checkUpdate(true)"
    >
      <RefreshCwIcon :class="checking && 'animate-spin'" /> 重新检查
    </Button>
  </section>

  <ListSkeleton v-if="!info" :rows="4" />
  <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
    <section
      v-for="s in sections"
      :key="s.title"
      class="bg-card grid grid-cols-1 content-start gap-3 rounded-lg border p-4"
    >
      <h2 class="flex items-center gap-2 font-semibold">
        <component :is="s.icon" class="text-muted-foreground size-4" />{{ s.title }}
      </h2>
      <dl class="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-sm">
        <template v-for="r in s.rows" :key="r.k">
          <dt class="text-muted-foreground">{{ r.k }}</dt>
          <dd class="break-all" :class="r.mono && 'font-mono'">
            <a
              v-if="r.href"
              :href="r.href"
              target="_blank"
              rel="noopener"
              class="text-primary underline-offset-2 hover:underline"
              >{{ r.v }}</a
            >
            <template v-else>{{ r.v }}</template>
          </dd>
        </template>
      </dl>
      <p
        v-if="s.title === '配置来源'"
        class="text-muted-foreground border-t pt-3 text-xs leading-relaxed"
      >
        配置项优先读取配置文件，配置文件中未设置的项读取环境变量。主密钥须与数据库分开备份：两者同时丢失将导致加密数据无法恢复，同时泄露将导致加密失效。
      </p>
    </section>

    <section class="bg-card grid grid-cols-1 gap-3 rounded-lg border p-4 lg:col-span-2">
      <h2 class="font-semibold">数据量</h2>
      <dl class="grid grid-cols-3 gap-3 sm:grid-cols-6">
        <div v-for="c in counts" :key="c.k">
          <dt class="text-muted-foreground text-xs">{{ c.k }}</dt>
          <dd class="text-lg font-medium tabular-nums">{{ c.v }}</dd>
        </div>
      </dl>
    </section>
  </div>
</template>
