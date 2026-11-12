<script setup lang="ts">
import { MonitorIcon, MoonIcon, SunIcon } from '@lucide/vue'
import { RadioGroupItem, RadioGroupRoot } from 'reka-ui'

import { type ThemeMode, setThemeMode, themeMode, themeModes } from '@/lib/theme'

// 三档深浅色的分段单选：整组只占一个 Tab 位置，方向键在三档之间移动并直接切换。
// 选中项在深色下用 accent 底色加描边，和 PageHeader 的子页切换一致。
const icons = { system: MonitorIcon, light: SunIcon, dark: MoonIcon }
</script>

<template>
  <RadioGroupRoot
    :model-value="themeMode"
    orientation="horizontal"
    aria-label="深浅色"
    class="bg-muted/70 inline-flex rounded-md border p-0.5"
    @update:model-value="(v) => setThemeMode(v as ThemeMode)"
  >
    <RadioGroupItem
      v-for="m in themeModes"
      :key="m.value"
      :value="m.value"
      :title="m.label"
      class="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 data-[state=checked]:bg-card data-[state=checked]:text-foreground dark:data-[state=checked]:bg-accent dark:data-[state=checked]:border-input inline-flex size-7 items-center justify-center rounded-sm border border-transparent transition-colors outline-none focus-visible:ring-2 data-[state=checked]:shadow-xs [&>svg]:size-3.5"
    >
      <component :is="icons[m.value]" />
      <span class="sr-only">{{ m.label }}</span>
    </RadioGroupItem>
  </RadioGroupRoot>
</template>
