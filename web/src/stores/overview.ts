import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

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

  // 顶栏状态胶囊的口径：要处理的是得动手的事（待审核、推送失败），其余算提醒。
  const summary = computed(() => {
    const a = data.value?.attention
    if (!a) return null
    return {
      action: a.pending_review + a.push_failed,
      notice: a.expiring + a.expired + a.outdated + a.never_connected + a.push_retrying,
    }
  })

  return { data, load, summary }
})
