// 与后端 internal/server 各 handler 的请求、响应结构一一对应。

import type { Tone } from '@/lib/tone'

export interface MatcherDoc {
  name: string
  host: string
  path?: string
  method?: string
}

export interface FieldDoc {
  key: string
  name?: string
  matcher?: string
  in: string
  path: string
  regex_on?: string
  type?: string
  sensitive?: boolean
  min_len?: number
  max_len?: number
  pattern?: string
}

export interface OutputDoc {
  name: string
  template: string
}

export interface ProbeSpec {
  method?: string
  url: string
  headers?: Record<string, string>
  body?: string
  expect: string
  identity?: string
  timeout?: number
}

export interface SourceDoc {
  key: string
  name: string
  description?: string
  enabled?: boolean
  identity_field?: string
  merge_window?: number
  notify?: string
  min_interval?: number
  debug?: boolean
  review?: string
  probe?: ProbeSpec
  matchers: MatcherDoc[]
  fields: FieldDoc[]
  outputs?: OutputDoc[]
}

export interface SourceView extends SourceDoc {
  id: number
  updated_at: string
}

export interface SourceSummary {
  id: number
  key: string
  name: string
  enabled: boolean
  matchers: number
  fields: number
  outputs: number
  updated_at: string
}

export interface Example {
  key: string
  name: string
  description: string
}

export interface TryValue {
  ok: boolean
  value?: string
  error?: string
  // 取到了值、但没通过字段校验的原因
  invalid?: string
  expires_at?: string
}

export interface TryResult {
  matched: string[]
  matchers: { name: string; matched: boolean; reason?: string }[]
  fields: Record<string, TryValue>
  outputs: Record<string, TryValue>
  probe?: { ok: boolean; detail?: string; identity?: string; error?: string }
}

export interface Endpoint {
  id: number
  name: string
  base_url: string
  note: string
}

export interface SubscriptionInput {
  name: string
  client: string
  endpoint_id: number
  fallback_id: number | null
  all_sources: boolean
  source_keys: string[]
  transport_mode: TransportMode
  // 自定义的 https 脚本地址；空串表示用主访问地址下的 /script/ferry.js。
  script_url: string
}

// plain：订阅令牌认证、正文明文；sealed：用传输密钥加密整个报文。
export type TransportMode = 'plain' | 'sealed'

// 安装状态：没下载过订阅文件、也没连上过；下载过订阅文件；手机上的脚本连上过平台。
export type SubscriptionStatus = 'not_installed' | 'installed' | 'connected'

export const subscriptionStatus: Record<SubscriptionStatus, { label: string; tone: Tone }> = {
  not_installed: { label: '未安装', tone: 'neutral' },
  installed: { label: '已安装', tone: 'info' },
  connected: { label: '已连通', tone: 'success' },
}

export interface Subscription extends SubscriptionInput {
  id: number
  token_prefix: string
  last_fetched_at: string | null
  status: SubscriptionStatus
  // 手机上那份订阅的 MITM 域名已经过时，要在客户端里更新
  outdated: boolean
  file_fetched_at: string | null
  // 手机最近一次连上平台：拉规则或走加密通道
  last_seen_at: string | null
  hostnames: string[]
  links: { subscription_url: string; install_url: string; script_url: string }
  // verified_at：最近一次换密钥后，平台第一次解开这台手机的请求的时间。
  transport: {
    mode: TransportMode
    kid: string
    verified_at: string | null
    last_at: string | null
  }
}

// term 是订阅在这个客户端里的叫法。
export const clients = [
  { value: 'surge', label: 'Surge', term: '模块' },
  { value: 'shadowrocket', label: 'Shadowrocket', term: '模块' },
  { value: 'loon', label: 'Loon', term: '插件' },
  { value: 'qx', label: 'Quantumult X', term: '重写' },
  { value: 'stash', label: 'Stash', term: '覆写' },
  { value: 'egern', label: 'Egern', term: '模块' },
]

// 加密传输要把密钥填进模块参数；QX 没有模块参数，不支持。
export const supportsSealed = (client: string) => client !== 'qx'

export interface Version {
  id: number
  field: string
  value: string
  masked: boolean
  status: 'pending' | 'pending_review' | 'valid' | 'rejected'
  reason?: string
  current: boolean
  expires_at: string | null
  seen_count: number
  last_seen_at: string
  activated_at: string | null
  created_at: string
}

export interface IdentityField {
  key: string
  name: string
  sensitive: boolean
  current: Version | null
  pending: number
}

export interface Identity {
  id: number
  source_id: number
  source_key: string
  source_name: string
  key: string
  last_seen_at: string
  created_at: string
  // 只在列表里有：待审核的版本数、当前值里最早的过期时间、推送状态（没有推送规则时没有）、最近推送成功的时间
  pending?: number
  expires_at?: string | null
  push_status?: PushState
  pushed_at?: string | null
  fields?: IdentityField[]
}

export const versionStatus: Record<string, { label: string; tone: Tone }> = {
  pending: { label: '收集中', tone: 'info' },
  pending_review: { label: '待审核', tone: 'warning' },
  valid: { label: '已替换', tone: 'neutral' },
  rejected: { label: '已驳回', tone: 'danger' },
}

export interface DestConfig {
  base_url?: string
  client_id?: string
  client_secret?: string
  url?: string
  method?: string
  headers?: Record<string, string>
  body?: string
  secret?: string
}

export interface Destination {
  id: number
  name: string
  kind: 'qinglong' | 'webhook'
  enabled: boolean
  config: DestConfig
  secrets_set: Record<string, boolean>
  updated_at: string
  // 只在列表里有：推到这里的最近一次任务；最近一次推送失败或在等待重试的数量
  last_push?: PushJob
  problems?: number
}

export const destinationKinds: Record<Destination['kind'], string> = {
  qinglong: '青龙',
  webhook: 'Webhook',
}

export interface Binding {
  id: number
  source_id: number
  destination_id: number
  mode: 'per_identity' | 'joined'
  rename: Record<string, string>
  enabled: boolean
}

export const bindingModes: Record<Binding['mode'], string> = {
  per_identity: '每个身份一条',
  joined: '合并为一条',
}

export interface PushJob {
  id: number
  binding_id: number
  identity_id: number | null
  status: 'queued' | 'running' | 'success' | 'failed' | 'skipped'
  attempts: number
  trigger: string
  last_error?: string
  vars?: string
  next_attempt_at: string
  created_at: string
  finished_at: string | null
}

// 界面上的推送状态：任务状态之外，排队中带着错误的单列为等待重试，执行中并入排队中；none 是还没推过。
export type PushState = 'success' | 'failed' | 'retrying' | 'queued' | 'skipped' | 'none'

export const pushStates: Record<PushState, { label: string; tone: Tone }> = {
  success: { label: '成功', tone: 'success' },
  failed: { label: '失败', tone: 'danger' },
  retrying: { label: '等待重试', tone: 'warning' },
  queued: { label: '排队中', tone: 'info' },
  skipped: { label: '跳过', tone: 'neutral' },
  none: { label: '未推送', tone: 'neutral' },
}

export function pushStateOf(j: PushJob): PushState {
  if (j.status === 'queued' && j.last_error) return 'retrying'
  if (j.status === 'running') return 'queued'
  return j.status
}

// 身份所属来源的一条推送规则，以及推给这个身份的最近一次任务。
export interface IdentityPush {
  binding_id: number
  destination_id: number
  destination_name: string
  destination_kind: 'qinglong' | 'webhook'
  mode: 'per_identity' | 'joined'
  // 推送规则和目的地都启用
  enabled: boolean
  last: PushJob | null
}

// 推送任务的触发原因。早先的记录里存的是中文说明，也一并对应到新说法；不认识的原样显示。
export const pushTrigger: Record<string, string> = {
  'value.updated': '新值生效',
  approve: '审核通过',
  activate: '恢复旧值',
  manual: '手动推送',
  人工审核通过: '审核通过',
  回滚到旧版本: '恢复旧值',
}

export interface ChannelConfig {
  server?: string
  device_key?: string
  bot_token?: string
  chat_id?: string
  api_base?: string
  url?: string
  secret?: string
}

export interface Channel {
  id: number
  name: string
  kind: 'bark' | 'telegram' | 'webhook'
  enabled: boolean
  config: ChannelConfig
  secrets_set: Record<string, boolean>
}

export const channelKinds: Record<Channel['kind'], string> = {
  bark: 'Bark',
  telegram: 'Telegram',
  webhook: 'Webhook',
}

export interface NotifyRule {
  id: number
  name: string
  events: string[]
  source_keys: string[]
  channel_ids: number[]
  enabled: boolean
}

export const eventTypes: { value: string; label: string; tone: Tone }[] = [
  { value: 'identity.new', label: '新身份', tone: 'info' },
  { value: 'value.updated', label: '登录态已更新', tone: 'success' },
  { value: 'value.rejected', label: '新值被驳回', tone: 'danger' },
  { value: 'value.pending_review', label: '新值待审核', tone: 'warning' },
  { value: 'push.failed', label: '推送失败', tone: 'danger' },
  { value: 'value.expiring', label: '登录态即将过期', tone: 'warning' },
  { value: 'subscription.mitm_changed', label: '订阅待更新', tone: 'indigo' },
]

export interface ApiToken {
  id: number
  name: string
  prefix: string
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

export type AuditCategory = 'login' | 'config' | 'sync' | 'push' | 'notify' | 'reveal'

export const auditCategories: { value: AuditCategory; label: string }[] = [
  { value: 'login', label: '登录' },
  { value: 'config', label: '配置' },
  { value: 'sync', label: '回传' },
  { value: 'push', label: '推送' },
  { value: 'notify', label: '通知' },
  { value: 'reveal', label: '查看原值' },
]

// 审计记录。action_label、category、outcome 和两个名称由后端按原始记录算出；
// outcome 为空表示这类操作没有成败之分（如修改配置）。
export interface AuditLog {
  id: number
  actor: string
  action: string
  target: string
  detail: string
  ip: string
  at: string
  action_label: string
  category: AuditCategory | ''
  outcome: 'success' | 'failure' | 'warning' | ''
  actor_kind: 'user' | 'token' | 'subscription' | 'system' | 'anonymous' | ''
  actor_name: string
  target_name: string
}

// 收集中的批次：手机已回传、还在等合并窗口结束的值。
export interface Batch {
  id: number
  source_id: number
  source_key: string
  source_name: string
  subscription_name: string
  created_at: string
  closes_at: string
  values: (Version & { name: string })[]
  missing: string[]
}

// 总览和侧栏角标用的数据，口径见后端 internal/server/overview.go。
export interface OverviewAttention {
  pending_review: number
  push_failed: number
  push_retrying: number
  expiring: number
  expired: number
  collecting: number
  never_connected: number
  outdated: number
}

export interface OverviewCounts {
  sources: number
  sources_enabled: number
  subscriptions: number
  subscriptions_connected: number
  identities: number
  destinations: number
  destinations_enabled: number
  bindings: number
  channels: number
  channels_enabled: number
  rules: number
  rules_enabled: number
}

export interface OverviewSource {
  id: number
  key: string
  name: string
  enabled: boolean
  identities: number
  last_seen_at: string | null
  pending_review: number
  rejected: number
  push_failed: number
  push_retrying: number
  expiring: number
  expired: number
  bindings: number
  // 各身份当前值里最早的过期时间
  expires_at: string | null
}

export interface Overview {
  window_days: number
  attention: OverviewAttention
  counts: OverviewCounts
  sources: OverviewSource[]
  items: AttentionItem[]
}

// 收件箱里的一条待处理，带着就地处理用的 id；action 要动手处理，notice 是提醒。
export type AttentionKind =
  | 'pending_review'
  | 'push_failed'
  | 'push_retrying'
  | 'expired'
  | 'expiring'
  | 'outdated'
  | 'never_connected'

export interface AttentionItem {
  kind: AttentionKind
  severity: 'action' | 'notice'
  source_id?: number
  source_name?: string
  identity_id?: number
  identity_key?: string
  field?: string
  field_name?: string
  version_id?: number
  value?: string
  masked?: boolean
  push_id?: number
  binding_id?: number
  destination_id?: number
  destination_name?: string
  subscription_id?: number
  subscription_name?: string
  client?: string
  error?: string
  attempts?: number
  at: string | null
}

// 流向图：设备（订阅）→ 来源 → 目的地，口径见后端 internal/server/flow.go。
export interface FlowDevice {
  id: number
  name: string
  client: string
  status: SubscriptionStatus
  outdated: boolean
  last_seen_at: string | null
  transport_mode: TransportMode
  source_ids: number[]
}

// state 为空表示停用或没有可用的推送规则。
export interface FlowDestination {
  id: number
  name: string
  kind: 'qinglong' | 'webhook'
  enabled: boolean
  bindings: number
  state: PushState | ''
  last_error?: string
  last_at: string | null
}

export interface FlowBinding {
  id: number
  source_id: number
  destination_id: number
  mode: string
  enabled: boolean
  state: PushState | ''
  push_id?: number
  last_error?: string
  last_at: string | null
}

export interface Flow {
  sources: OverviewSource[]
  devices: FlowDevice[]
  destinations: FlowDestination[]
  bindings: FlowBinding[]
}

// 来源详情一次取回的整条链路。
export interface LaneIdentity extends Identity {
  fields: IdentityField[]
  pushes: IdentityPush[]
}

export interface LaneBinding extends FlowBinding {
  destination_name: string
  destination_kind: 'qinglong' | 'webhook'
}

export interface Lane {
  source: OverviewSource
  review: string
  probe: boolean
  identity_field: string
  identities: LaneIdentity[]
  bindings: LaneBinding[]
  devices: FlowDevice[]
  items: AttentionItem[]
  recent: ActivityItem[]
}

// 记录页时间线上的一条：回传、审核、推送、通知。
export interface ActivityItem {
  kind: 'sync' | 'review' | 'push' | 'notify'
  at: string
  outcome: 'success' | 'warning' | 'failure' | ''
  title: string
  detail?: string
  source_id?: number
  source_name?: string
  identity_id?: number
  identity_key?: string
  device_id?: number
  device_name?: string
  destination_id?: number
  destination_name?: string
  batch_id?: number
  push_id?: number
}

export interface ActivityPage {
  items: ActivityItem[]
  next: string | null
}

// 系统信息，口径见后端 internal/server/system.go。来源是 file、env、default，没给出时为空。
export interface ConfigSource {
  from: '' | 'file' | 'env' | 'default'
  env?: string
}

export interface SystemInfo {
  build: { version: string; commit: string; date: string; go: string; os: string; arch: string }
  runtime: { container: boolean; started_at: string; timezone: string; listen: string }
  database: { driver: string; version: string; target: string; size: number }
  config: {
    path: string
    exists: boolean
    sources: {
      listen: ConfigSource
      driver: ConfigSource
      dsn: ConfigSource
      master_key: ConfigSource
    }
  }
  counts: {
    sources: number
    identities: number
    versions: number
    subscriptions: number
    destinations: number
    pushes: number
  }
}

export interface UpdateInfo {
  current: string
  latest: string
  url: string
  newer: boolean
  checked_at: string
  error?: string
}
