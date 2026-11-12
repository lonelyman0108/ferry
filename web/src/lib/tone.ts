// 界面里的配色分两类：语义色（成功、警告、失败等状态）和各功能区的主题色（侧边栏、页面标题、图标块共用）。
// 类名必须完整写出来，Tailwind 只认源码里出现过的字面量。

export type Tone =
  | 'primary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'
  | 'neutral'
  | 'violet'
  | 'sky'
  | 'indigo'
  | 'emerald'
  | 'amber'
  | 'slate'

// 浅色模式下 Tailwind 色板的文字和图标用 700：600 在同色浅底上不到 4.5:1（sky、emerald、amber 连 3:1 都不到）。

// 图标块：浅底色 + 同色图标。
export const toneTile: Record<Tone, string> = {
  primary: 'bg-primary/10 text-primary',
  success: 'bg-success/12 text-success',
  warning: 'bg-warning/15 text-warning',
  danger: 'bg-destructive/10 text-destructive',
  info: 'bg-info/12 text-info',
  neutral: 'bg-muted text-muted-foreground',
  violet: 'bg-violet-500/12 text-violet-700 dark:text-violet-400',
  sky: 'bg-sky-500/12 text-sky-700 dark:text-sky-400',
  indigo: 'bg-indigo-500/12 text-indigo-700 dark:text-indigo-400',
  emerald: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-400',
  amber: 'bg-amber-500/15 text-amber-700 dark:text-amber-400',
  slate: 'bg-slate-500/12 text-slate-700 dark:text-slate-300',
}

// 徽章：浅底色 + 细边 + 同色文字，状态点用 toneDot。
export const toneBadge: Record<Tone, string> = {
  primary: 'border-primary/25 bg-primary/10 text-primary',
  success: 'border-success/25 bg-success/10 text-success',
  warning: 'border-warning/30 bg-warning/12 text-warning',
  danger: 'border-destructive/25 bg-destructive/10 text-destructive',
  info: 'border-info/25 bg-info/10 text-info',
  neutral: 'border-border bg-muted text-muted-foreground',
  violet: 'border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-400',
  sky: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-400',
  indigo: 'border-indigo-500/25 bg-indigo-500/10 text-indigo-700 dark:text-indigo-400',
  emerald: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  amber: 'border-amber-500/30 bg-amber-500/12 text-amber-700 dark:text-amber-400',
  slate: 'border-slate-500/25 bg-slate-500/10 text-slate-700 dark:text-slate-300',
}

export const toneDot: Record<Tone, string> = {
  primary: 'bg-primary',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-destructive',
  info: 'bg-info',
  neutral: 'bg-muted-foreground/50',
  violet: 'bg-violet-500',
  sky: 'bg-sky-500',
  indigo: 'bg-indigo-500',
  emerald: 'bg-emerald-500',
  amber: 'bg-amber-500',
  slate: 'bg-slate-500',
}

// 数字强调色，用在统计卡片和表格里的计数。
export const toneText: Record<Tone, string> = {
  primary: 'text-primary',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-destructive',
  info: 'text-info',
  neutral: 'text-muted-foreground',
  violet: 'text-violet-700 dark:text-violet-400',
  sky: 'text-sky-700 dark:text-sky-400',
  indigo: 'text-indigo-700 dark:text-indigo-400',
  emerald: 'text-emerald-700 dark:text-emerald-400',
  amber: 'text-amber-700 dark:text-amber-400',
  slate: 'text-slate-700 dark:text-slate-300',
}
