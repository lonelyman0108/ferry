<script setup lang="ts">
import {
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  SettingsIcon,
  SunIcon,
  UserRoundIcon,
} from '@lucide/vue'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { useSidebar } from '@/components/ui/sidebar'
import { type ThemeMode, setThemeMode, themeMode, themeModes } from '@/lib/theme'
import { useAuth } from '@/stores/auth'

// 用户菜单的内容，侧栏底部和手机顶栏共用：账户、深浅色、退出登录。
// 手机上没有侧栏，菜单顶部补上用户名，并放入「设置」（底部导航不再单列）。
// 侧栏展开时底部已有深浅色切换，菜单里不再重复；侧栏收起为图标或在手机上时才显示。
// 菜单项响应 reka 的 select 事件：鼠标、触屏、键盘选中都会触发，触发后菜单自行关闭。
const props = defineProps<{
  side?: 'top' | 'bottom'
  align?: 'start' | 'end'
  mobile?: boolean
}>()

const auth = useAuth()
const router = useRouter()
const sidebar = useSidebar()
const showTheme = computed(() => props.mobile || sidebar.state.value === 'collapsed')
const icons = { system: MonitorIcon, light: SunIcon, dark: MoonIcon }

async function logout() {
  try {
    await auth.logout()
    await router.push('/login')
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <DropdownMenuContent :side="props.side" :align="props.align" class="w-52">
    <template v-if="props.mobile">
      <DropdownMenuLabel class="text-foreground truncate text-sm">{{
        auth.user?.username
      }}</DropdownMenuLabel>
      <DropdownMenuSeparator />
    </template>
    <DropdownMenuItem @select="router.push('/account')"><UserRoundIcon /> 账户</DropdownMenuItem>
    <DropdownMenuItem v-if="props.mobile" @select="router.push('/settings')"
      ><SettingsIcon /> 设置</DropdownMenuItem
    >
    <template v-if="showTheme">
      <DropdownMenuSeparator />
      <DropdownMenuLabel>深浅色</DropdownMenuLabel>
      <DropdownMenuRadioGroup
        aria-label="深浅色"
        :model-value="themeMode"
        @update:model-value="(v) => setThemeMode(v as ThemeMode)"
      >
        <DropdownMenuRadioItem v-for="m in themeModes" :key="m.value" :value="m.value"
          ><component :is="icons[m.value]" /> {{ m.label }}</DropdownMenuRadioItem
        >
      </DropdownMenuRadioGroup>
    </template>
    <DropdownMenuSeparator />
    <DropdownMenuItem variant="destructive" @select="logout"
      ><LogOutIcon /> 退出登录</DropdownMenuItem
    >
  </DropdownMenuContent>
</template>
