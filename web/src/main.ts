import { createPinia } from 'pinia'
import { createApp } from 'vue'
import 'vue-sonner/style.css'

import App from './App.vue'
import router from './router'
import './style.css'

// 跟随系统的深色模式。
const dark = window.matchMedia('(prefers-color-scheme: dark)')
const applyTheme = () => document.documentElement.classList.toggle('dark', dark.matches)
applyTheme()
dark.addEventListener('change', applyTheme)

createApp(App).use(createPinia()).use(router).mount('#app')
