<script setup lang="ts">
import { ArrowLeftIcon } from '@lucide/vue'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import { navFor, pageFor } from '@/lib/nav'

// 每个页面顶部的标题区：标题 + 一句话说明，右侧放操作按钮。
// 不传 title / description 时取导航里这一页的名称和说明；传了 back 就显示返回按钮。
// 导航项下有几页时（推送、设置），标题写导航项的名称，说明写当前这一页的，标题下面一排下划线链接切换各页；
// 页面里自己的标签页是带底色的分段样式，两层一眼能分开，标题也不会和选中的那一页、第一个标签页重复。
// 下划线链接所在的横条可以横向滚动，底线用内阴影画（滚动容器会裁掉负外边距压上去的边框）。
// 窄屏时标题独占一行，操作区换到下一行。
const props = defineProps<{ title?: string; description?: string; back?: string }>()

const route = useRoute()
const router = useRouter()
const nav = computed(() => navFor(route.path))
const page = computed(() => pageFor(route.path))
const subnav = computed(() => (nav.value.pages && !props.back ? nav.value.pages : null))
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-4 gap-y-3" :class="subnav ? 'mb-4' : 'mb-6'">
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
          <span class="truncate">{{
            props.title ?? (subnav ? nav.label : (page?.label ?? nav.label))
          }}</span>
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
  <nav
    v-if="subnav"
    class="no-scrollbar mb-6 flex overflow-x-auto text-sm shadow-[inset_0_-1px_0_var(--color-border)]"
    :aria-label="nav.label"
  >
    <router-link
      v-for="p in subnav"
      :key="p.path"
      :to="p.path"
      class="flex shrink-0 items-center border-b-2 px-3 py-2 transition-colors max-md:min-h-11"
      :class="
        page?.path === p.path
          ? 'border-primary text-foreground font-medium'
          : 'text-muted-foreground hover:text-foreground hover:border-border border-transparent'
      "
      :aria-current="page?.path === p.path ? 'page' : undefined"
      >{{ p.label }}</router-link
    >
  </nav>
</template>
