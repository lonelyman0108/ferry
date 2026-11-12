import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api } from '../api'
import type { SourceSummary } from '../types'

// 来源的自定义图标，按 key 存。总览、画布、详情这些接口不带图标（图标可能有几十 KB），
// 头像组件拿不到图标时从这里查；第一次用到时取一次来源列表，来源列表页和命令面板取到列表时顺手更新。
export const useSourceIcons = defineStore('sourceIcons', () => {
  const byKey = ref<Record<string, string>>({})
  let state: 'idle' | 'loading' | 'loaded' = 'idle'

  function set(list: Pick<SourceSummary, 'key' | 'icon'>[]) {
    byKey.value = Object.fromEntries(list.filter((s) => s.icon).map((s) => [s.key, s.icon!]))
    state = 'loaded'
  }

  function load() {
    state = 'loading'
    return api
      .get<SourceSummary[]>('/sources')
      .then((r) => set(r.data))
      .catch(() => {
        state = 'idle'
      })
  }

  function ensure() {
    if (state === 'idle') void load()
  }

  return { byKey, set, load, ensure }
})
