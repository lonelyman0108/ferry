<script setup lang="ts">
import { MonitorIcon, MoonIcon, SunIcon } from '@lucide/vue'

import { setThemeMode, themeMode, themeModes } from '@/lib/theme'

// 选中项在深色下用 accent 底色加描边，和 PageHeader 的子页切换一致。
const icons = { system: MonitorIcon, light: SunIcon, dark: MoonIcon }
</script>

<template>
  <div
    class="bg-muted/70 inline-flex rounded-md border p-0.5"
    role="radiogroup"
    aria-label="深浅色"
  >
    <button
      v-for="m in themeModes"
      :key="m.value"
      type="button"
      role="radio"
      :aria-checked="themeMode === m.value"
      :title="m.label"
      class="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-7 items-center justify-center rounded-sm border border-transparent transition-colors outline-none focus-visible:ring-2 [&>svg]:size-3.5"
      :class="
        themeMode === m.value &&
        'bg-card text-foreground dark:bg-accent dark:border-input shadow-xs'
      "
      @click="setThemeMode(m.value)"
    >
      <component :is="icons[m.value]" />
      <span class="sr-only">{{ m.label }}</span>
    </button>
  </div>
</template>
