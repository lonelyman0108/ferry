import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api } from '../api'
import type { Flow } from '../types'

// 流向图数据：总览的画布用它，聚焦详情里的窄栏也用它。同一时刻只发一个请求，失败时保留上一次的结果。
export const useFlow = defineStore('flow', () => {
  const data = ref<Flow | null>(null)
  let pending: Promise<void> | null = null

  function load(): Promise<void> {
    if (!pending) {
      pending = api
        .get<Flow>('/flow')
        .then((r) => {
          data.value = r.data
        })
        .finally(() => {
          pending = null
        })
    }
    return pending
  }

  return { data, load }
})
