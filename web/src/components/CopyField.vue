<script setup lang="ts">
import { CopyIcon } from '@lucide/vue'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { copyText } from '@/format'

// 只读的一行文字加复制按钮，用于链接、令牌。
const props = defineProps<{ value: string; label?: string }>()

async function copy() {
  try {
    await copyText(props.value)
    toast.success(props.label ? `已复制${props.label}` : '已复制')
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <div class="flex gap-2">
    <Input
      :model-value="value"
      readonly
      :name="label"
      :aria-label="label"
      class="font-mono text-xs"
      @focus="($event.target as HTMLInputElement).select()"
    />
    <Button variant="outline" size="icon" :aria-label="`复制${label ?? ''}`" @click="copy">
      <CopyIcon />
    </Button>
  </div>
</template>
