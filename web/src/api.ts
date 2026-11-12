import axios, { AxiosError } from 'axios'

import router from './router'

// 校验问题的位置与说明。path 写法同 JSON 路径，如 matchers[0].host；涉及整体时为空。
export interface Problem {
  path?: string
  message: string
}

// 后端统一返回 { error, details, problems }，这里把它变成可直接展示的文字。
// problems 与 details 一一对应，另带出错位置，表单据此把错误显示在对应的输入框下方。
export class ApiError extends Error {
  status: number
  details: string[]
  problems: Problem[]
  constructor(status: number, message: string, details: string[] = [], problems: Problem[] = []) {
    super(message)
    this.status = status
    this.details = details
    this.problems = problems
  }
}

export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.response.use(
  (resp) => resp,
  (err: AxiosError<{ error?: string; details?: string[]; problems?: Problem[] }>) => {
    const status = err.response?.status ?? 0
    const data = err.response?.data
    const msg = data?.error ?? (status ? `请求失败（HTTP ${status}）` : '连接服务失败')
    const path = router.currentRoute.value.path
    // 登录过期就回登录页；尚未初始化就去向导。登录、向导页自己处理错误。
    if (status === 401 && path !== '/login' && !err.config?.url?.startsWith('/auth/login')) {
      void router.push({ path: '/login', query: { next: path } })
    } else if (status === 503 && path !== '/setup') {
      void router.push('/setup')
    }
    return Promise.reject(new ApiError(status, msg, data?.details ?? [], data?.problems ?? []))
  },
)

// errorText 把任意错误变成一行提示，带上校验的明细。
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    return e.details.length ? `${e.message}：${e.details.join('；')}` : e.message
  }
  return e instanceof Error ? e.message : String(e)
}
