// 来源定义里的出错位置（JSON 路径，如 key、matchers[0].host、probe.url）对应到编辑器：
// 所在分区、输入框的 id，以及在错误汇总里显示的名称。

export interface Spot {
  section: string
  // 出错时聚焦的元素；不是输入框时聚焦其中第一个可交互的控件
  id: string
  label: string
  // 同一分区内的先后，用于排序
  order: number
}

type Parts = Record<string, [id: string, label: string]>

const basic: Parts = {
  key: ['src-key', '来源标识'],
  name: ['src-name', '名称'],
  icon: ['src-icon', '图标'],
  description: ['src-desc', '说明'],
  notify: ['src-notify', '设备通知'],
  min_interval: ['src-interval', '最短回传间隔'],
  review: ['src-review', '生效方式'],
  merge_window: ['src-merge', '合并窗口'],
  identity_field: ['src-identity', '身份字段'],
}

// 列表类分区：每一项的输入框 id 是「前缀-下标」，如 m-host-0。
const lists: Record<
  string,
  { section: string; noun: string; unit: string; add: string; parts: Parts }
> = {
  matchers: {
    section: 'matchers',
    noun: '匹配规则',
    unit: '条',
    add: 'add-matcher',
    parts: {
      name: ['m-name', '名称'],
      host: ['m-host', '域名'],
      method: ['m-method', '请求方法'],
      path: ['m-path', '路径'],
    },
  },
  fields: {
    section: 'fields',
    noun: '字段',
    unit: '个',
    add: 'add-field',
    parts: {
      key: ['f-key', '字段标识'],
      name: ['f-name', '名称'],
      matcher: ['f-matcher', '匹配规则'],
      in: ['f-in', '读取位置'],
      path: ['f-path', '键名 / 路径'],
      regex_on: ['f-regex-on', '匹配对象'],
      type: ['f-type', '类型'],
      min_len: ['f-min', '最小长度'],
      max_len: ['f-max', '最大长度'],
      pattern: ['f-pattern', '校验正则'],
    },
  },
  outputs: {
    section: 'outputs',
    noun: '输出变量',
    unit: '个',
    add: 'add-output',
    parts: { name: ['o-name', '变量名'], template: ['o-tpl', '变量值'] },
  },
}

const probe: Parts = {
  method: ['p-method', '请求方法'],
  url: ['p-url', '接口地址'],
  headers: ['p-headers', '请求头'],
  body: ['p-body', '请求体'],
  expect: ['p-expect', '成功条件'],
  identity: ['p-identity', '身份表达式'],
  timeout: ['p-timeout', '超时'],
}

const indexOf = (parts: Parts, k: string) => Object.keys(parts).indexOf(k)

// locate 返回位置对应的分区和输入框；无法对应时返回 undefined，由调用方放进错误汇总的通用部分。
export function locate(path: string): Spot | undefined {
  if (path in basic) {
    const [id, label] = basic[path]
    return { section: 'basic', id, label, order: indexOf(basic, path) }
  }
  if (path.startsWith('probe.')) {
    const k = path.slice('probe.'.length)
    if (!(k in probe)) return undefined
    const [id, label] = probe[k]
    return { section: 'probe', id, label: `有效性检测 · ${label}`, order: indexOf(probe, k) }
  }
  const m = /^(\w+)(?:\[(\d+)\]\.(\w+))?$/.exec(path)
  const list = m && lists[m[1]]
  if (!m || !list) return undefined
  // 整个列表的问题（如字段数量），聚焦到「添加」按钮
  if (m[2] === undefined)
    return { section: list.section, id: list.add, label: list.noun, order: -1 }
  const i = Number(m[2])
  const part = list.parts[m[3]]
  if (!part) return undefined
  return {
    section: list.section,
    id: `${part[0]}-${i}`,
    label: `第 ${i + 1} ${list.unit}${list.noun} · ${part[1]}`,
    order: i * 100 + indexOf(list.parts, m[3]),
  }
}
