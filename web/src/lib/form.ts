import { computed, nextTick, reactive, ref } from 'vue'

// 表单校验：保存按钮不置灰，点了保存再就地标出缺的项；标出来以后边改边更新，改好一项消掉一项。
// check 返回 { 字段: 提示 }，没问题的字段返回空值。把 errors.字段 传给 FormField 的 error。
export function useFormCheck(check: () => Record<string, string | false | null | undefined>) {
  const tried = ref(false)
  const problems = computed(() => {
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(check())) if (v) out[k] = v
    return out
  })
  return reactive({
    errors: computed(() => (tried.value ? problems.value : {})),
    // 点保存时调用：有缺的项就标出来，光标移到第一个，返回 false。
    ok() {
      tried.value = true
      if (!Object.keys(problems.value).length) return true
      // 弹窗开着时只在弹窗里找。
      void nextTick(() => {
        const q = '[aria-invalid=true]'
        const el =
          document.querySelector<HTMLElement>(`[role=dialog] ${q}`) ??
          document.querySelector<HTMLElement>(q)
        el?.focus()
      })
      return false
    },
    // 打开表单时调用，清掉上一次的标记。
    reset() {
      tried.value = false
    },
  })
}

// 地址要填，且以 http:// 或 https:// 开头，和后端的校验一致。
export function urlProblem(v: string | undefined, what = '地址') {
  const t = (v ?? '').trim()
  if (!t) return `请填写${what}`
  return /^https?:\/\//i.test(t) ? '' : `${what}需以 http:// 或 https:// 开头`
}
