<script setup lang="ts">
import { ArrowLeftIcon } from '@lucide/vue'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import { navFor, pageFor } from '@/lib/nav'

// 每个页面顶部的标题区：标题 + 一句话说明，右侧放操作按钮。
// 不传 title / description 时取导航里这一页的名称和说明；传了 back 就显示返回按钮。
// 导航项下有几页时，标题上方用一排链接切换。窄屏时标题独占一行，操作区换到下一行。
const props = defineProps<{ title?: string; description?: string; back?: string }>()

const route = useRoute()
const router = useRouter()
const nav = computed(() => navFor(route.path))
const page = computed(() => pageFor(route.path))
</script>

<template>
  <nav
    v-if="nav.pages && !props.back"
    class="bg-muted/60 mb-4 inline-flex rounded-lg border p-0.5 text-sm"
    :aria-label="nav.label"
  >
    <router-link
      v-for="p in nav.pages"
      :key="p.path"
      :to="p.path"
      class="text-muted-foreground hover:text-foreground flex items-center rounded-md px-3 py-1.5 transition-colors max-md:min-h-10"
      :class="page?.path === p.path && 'bg-card text-foreground shadow-xs'"
      :aria-current="page?.path === p.path ? 'page' : undefined"
      >{{ p.label }}</router-link
    >
  </nav>
  <div class="mb-6 flex flex-wrap items-center gap-x-4 gap-y-3">
    <div class="flex min-w-0 basis-full items-center gap-3 sm:basis-0 sm:flex-1">
      <Button
        v-if="props.back"
        variant="ghost"
        size="icon"
        class="-ml-2 shrink-0"
        aria-label="返回"
        @click="router.push(props.back)"
        ><ArrowLeftIcon
      /></Button>
      <div class="min-w-0">
        <h1 class="flex flex-wrap items-center gap-2 text-xl leading-tight font-semibold">
          <span class="truncate">{{ props.title ?? page?.label ?? nav.label }}</span>
          <slot name="badges" />
        </h1>
        <p class="text-muted-foreground mt-0.5 text-sm">
          <slot name="description">{{
            props.description ?? page?.description ?? nav.description
          }}</slot>
        </p>
      </div>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
