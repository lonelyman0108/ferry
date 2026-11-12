import { ref } from 'vue'

// 深浅色有三档：跟随系统、浅色、深色，选择存在这台浏览器里。
// index.html 里有一段同样逻辑的内联脚本，在页面渲染前先加上 dark 类，避免首屏闪一下浅色。
export type ThemeMode = 'system' | 'light' | 'dark'

export const themeModes: { value: ThemeMode; label: string }[] = [
  { value: 'system', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
]

const storageKey = 'ferry.theme'
const media = window.matchMedia('(prefers-color-scheme: dark)')

function readMode(): ThemeMode {
  try {
    const v = localStorage.getItem(storageKey)
    if (v === 'light' || v === 'dark') return v
  } catch {
    // 隐私模式下读不到存储，按跟随系统处理
  }
  return 'system'
}

export const themeMode = ref<ThemeMode>(readMode())

function apply() {
  const dark = themeMode.value === 'dark' || (themeMode.value === 'system' && media.matches)
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
}

export function setThemeMode(mode: ThemeMode) {
  themeMode.value = mode
  try {
    if (mode === 'system') localStorage.removeItem(storageKey)
    else localStorage.setItem(storageKey, mode)
  } catch {
    // 存不下时只在本次打开期间生效
  }
  apply()
}

// 在浅色、深色之间切换（命令面板用）；跟随系统时切到和当前相反的一档。
export function toggleTheme() {
  setThemeMode(document.documentElement.classList.contains('dark') ? 'light' : 'dark')
}

export function initTheme() {
  apply()
  media.addEventListener('change', apply)
}
