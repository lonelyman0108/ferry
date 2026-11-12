<script setup lang="ts">
import { onMounted, ref, useId, watch } from 'vue'

import { Label } from '@/components/ui/label'

// 一个表单项：标签、可选标记、控件、说明。full 时在两列布局里占满一行。
// error 不为空时，说明换成红色的报错，里面的输入框标上 aria-invalid（样式变红，读屏会读出报错）。
const props = withDefaults(
  defineProps<{
    label?: string
    for?: string
    hint?: string
    optional?: boolean
    full?: boolean
    error?: string
  }>(),
  { label: '', for: undefined, hint: '', optional: false, full: false, error: '' },
)

const root = ref<HTMLElement>()
const errorId = useId()
// aria-describedby 里可能还有页面自己加的说明（如登录失败的提示），这里只增删本项报错的 id。
function mark() {
  for (const el of root.value?.querySelectorAll('input, textarea, select') ?? []) {
    const ids = (el.getAttribute('aria-describedby') ?? '')
      .split(/\s+/)
      .filter((id) => id && id !== errorId)
    if (props.error) {
      el.setAttribute('aria-invalid', 'true')
      ids.unshift(errorId)
    } else {
      el.removeAttribute('aria-invalid')
    }
    if (ids.length) el.setAttribute('aria-describedby', ids.join(' '))
    else el.removeAttribute('aria-describedby')
  }
}
watch(() => props.error, mark, { flush: 'post' })
onMounted(mark)
</script>

<template>
  <div ref="root" class="grid content-start gap-1.5" :class="full ? 'sm:col-span-2' : ''">
    <Label v-if="label" :for="$props.for" class="gap-1" :class="error ? 'text-destructive' : ''">
      {{ label }}
      <span v-if="optional" class="text-muted-foreground text-xs font-normal">可选</span>
    </Label>
    <slot />
    <p v-if="error" :id="errorId" class="text-destructive text-xs leading-relaxed">{{ error }}</p>
    <p v-else-if="hint || $slots.hint" class="text-muted-foreground text-xs leading-relaxed">
      <slot name="hint">{{ hint }}</slot>
    </p>
  </div>
</template>
