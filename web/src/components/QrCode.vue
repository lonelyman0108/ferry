<script setup lang="ts">
import { renderSVG } from 'uqr'
import { computed } from 'vue'

// 二维码始终白底黑码，深色模式下手机也能扫。
const props = defineProps<{ value: string }>()
const svg = computed(() => renderSVG(props.value, { border: 1 }))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- uqr 生成的 SVG，内容只有方块 -->
  <div class="rounded-md bg-white p-1 [&>svg]:h-full [&>svg]:w-full" v-html="svg" />
</template>
