import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api } from '../api'
import type { Overview } from '../types'

// 总览数据：总览页展示它，侧栏角标也用它。同一时刻只发一个请求，失败时保留上一次的结果。
export const useOverview = defineStore('overview', () => {
  const data = ref<Overview | null>(null)
  let pending: Promise<void> | null = null

  // quiet 为 false 时把错误交给调用方提示（总览页）；角标的定时刷新失败不打扰。
  function load(quiet = true): Promise<void> {
    if (!pending) {
      pending = api
        .get<Overview>('/overview')
        .then((r) => {
          data.value = r.data
        })
        .finally(() => {
          pending = null
        })
    }
    return quiet ? pending.catch(() => {}) : pending
  }

  return { data, load }
})
