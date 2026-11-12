import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import { type AttentionItem, type AttentionKind, clients, type PushJob } from '@/types'

// 收件箱条目的标签和颜色，总览浮层和详情顶部共用。
export const itemLabels: Record<
  AttentionKind,
  { label: string; tone: 'danger' | 'warning' | 'neutral' }
> = {
  push_failed: { label: '推送失败', tone: 'danger' },
  pending_review: { label: '新值待审核', tone: 'warning' },
  push_retrying: { label: '等待重试', tone: 'warning' },
  expired: { label: '已过期', tone: 'danger' },
  expiring: { label: '即将过期', tone: 'warning' },
  outdated: { label: '订阅规则过时', tone: 'warning' },
  never_connected: { label: '从未连通', tone: 'neutral' },
}

export const clientName = (v?: string) => clients.find((c) => c.value === v)?.label ?? v ?? ''

// 条目的唯一标识，用来记住“知道了”。
export const itemKey = (it: AttentionItem) =>
  `${it.kind}:${it.version_id ?? it.push_id ?? it.subscription_id ?? 0}:${it.identity_id ?? 0}`

// 「知道了」只在这台浏览器里收起提醒，不改后端；同一条再出现（比如换了新值后又快过期）key 会变。
const storageKey = 'ferry.inbox.dismissed'

export function readDismissed(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(storageKey) ?? '[]') as string[])
  } catch {
    return new Set()
  }
}

export function writeDismissed(keys: Set<string>) {
  try {
    localStorage.setItem(storageKey, JSON.stringify([...keys].slice(-200)))
  } catch {
    // 存不下时只在本次打开期间生效
  }
}

// 收件箱的就地操作。每个操作成功后返回 true，调用方据此刷新数据。
export function useInboxActions() {
  const router = useRouter()

  async function review(it: AttentionItem, approve: boolean) {
    try {
      await api.post(`/versions/${it.version_id}/${approve ? 'approve' : 'reject'}`, {})
      toast.success(approve ? '已通过，新值生效后会推送' : '已驳回')
      return true
    } catch (e) {
      toast.error(errorText(e))
      return false
    }
  }

  async function retry(it: AttentionItem) {
    try {
      const { data } = await api.post<PushJob>(`/pushes/${it.push_id}/retry`, {})
      if (data.status === 'success') toast.success(`已推送到 ${it.destination_name}`)
      else toast.error(`仍然没推出去：${data.last_error || '稍后自动重试'}`)
      return true
    } catch (e) {
      toast.error(errorText(e))
      return false
    }
  }

  // 一次重试多个失败的推送，返回成功的个数。
  async function retryAll(items: AttentionItem[]) {
    let ok = 0
    for (const it of items) {
      try {
        const { data } = await api.post<PushJob>(`/pushes/${it.push_id}/retry`, {})
        if (data.status === 'success') ok++
      } catch {
        // 单个失败不打断其余的
      }
    }
    if (ok === items.length) toast.success(`${ok} 个推送都已推出去`)
    else toast.error(`重试了 ${items.length} 个推送，${items.length - ok} 个仍然失败`)
    return ok
  }

  const openInstall = (it: AttentionItem) =>
    router.push({ path: '/subscriptions', query: { install: it.subscription_id } })
  const openDestination = () => router.push('/destinations')

  return { review, retry, retryAll, openInstall, openDestination }
}
