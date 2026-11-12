<script setup lang="ts">
import { ChevronDownIcon, CircleCheckIcon, InboxIcon } from '@lucide/vue'
import { computed, ref } from 'vue'

import type { AttentionItem } from '@/types'

import { itemKey, readDismissed, writeDismissed } from './inbox'
import InboxItem from './InboxItem.vue'

// 总览的收件箱：分“要处理”“提醒”两组，每条就地处理。可以折叠成一行。
// 窄于 1280 时收件箱排在流向图上面，“提醒”组默认收起，免得把图挤出首屏；宽屏上收件箱限高、内部滚动，默认展开。
const props = defineProps<{ items: AttentionItem[] }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: true })
const noticeOpen = ref(window.matchMedia('(min-width: 1280px)').matches)

const dismissed = ref(readDismissed())
const visible = computed(() => props.items.filter((it) => !dismissed.value.has(itemKey(it))))
const action = computed(() => visible.value.filter((it) => it.severity === 'action'))
const notice = computed(() => visible.value.filter((it) => it.severity === 'notice'))

function dismiss(it: AttentionItem) {
  const next = new Set(dismissed.value)
  next.add(itemKey(it))
  dismissed.value = next
  writeDismissed(next)
}
</script>

<template>
  <section
    class="bg-card flex max-h-full min-h-0 flex-col overflow-hidden rounded-xl border shadow-lg shadow-black/5 dark:shadow-black/40"
    aria-label="待处理"
  >
    <h2 class="shrink-0">
      <button
        type="button"
        class="hover:bg-accent/50 flex h-12 w-full items-center gap-2.5 px-4 text-left"
        :aria-expanded="open"
        @click="open = !open"
      >
        <InboxIcon class="text-muted-foreground size-4" />
        <span class="font-semibold">待处理</span>
        <span class="text-muted-foreground text-sm">
          {{ action.length ? `${action.length} 项待处理` : '运行正常' }}
          <template v-if="notice.length"> · {{ notice.length }} 条提醒</template>
        </span>
        <ChevronDownIcon
          class="text-muted-foreground ml-auto size-4 transition-transform"
          :class="!open && '-rotate-90'"
        />
      </button>
    </h2>
    <div v-if="open" class="min-h-0 overflow-y-auto border-t">
      <div
        v-if="!visible.length"
        class="text-muted-foreground flex items-start gap-2.5 p-4 text-sm"
      >
        <CircleCheckIcon class="text-success mt-0.5 size-4 shrink-0" />
        暂无待处理事项。
      </div>
      <template
        v-for="group in [
          { label: '待处理', list: action },
          { label: '提醒', list: notice },
        ]"
        :key="group.label"
      >
        <template v-if="group.list.length">
          <h3 v-if="group.label === '提醒'" class="text-muted-foreground text-xs">
            <button
              type="button"
              class="hover:bg-accent/50 flex min-h-11 w-full items-center gap-2 px-4 text-left"
              :class="action.length > 0 && 'border-t'"
              :aria-expanded="noticeOpen"
              @click="noticeOpen = !noticeOpen"
            >
              提醒<span class="tabular-nums">{{ group.list.length }} 条</span>
              <ChevronDownIcon
                class="ml-auto size-4 transition-transform"
                :class="!noticeOpen && '-rotate-90'"
              />
            </button>
          </h3>
          <h3 v-else class="text-muted-foreground px-4 pt-3 pb-1 text-xs">{{ group.label }}</h3>
          <TransitionGroup
            v-if="group.label !== '提醒' || noticeOpen"
            tag="ul"
            leave-active-class="transition duration-200"
            leave-to-class="opacity-0 -translate-x-3"
          >
            <li
              v-for="it in group.list"
              :key="itemKey(it)"
              class="border-b px-4 py-3 last:border-b-0"
            >
              <InboxItem :item="it" @done="emit('changed')" @dismiss="dismiss(it)" />
            </li>
          </TransitionGroup>
        </template>
      </template>
    </div>
  </section>
</template>
