import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 开发时前端单独跑，后端路径转发给本机的 ferry serve；FERRY_API 可改后端地址。
// /sub 和 /script 也转发，手机可以直接装开发服务器上的订阅。
// 规则按前缀匹配，所以要带结尾斜杠，否则前端的 /subscriptions 页面也会被转发走。
const api = process.env.FERRY_API ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    proxy: {
      '/api': api,
      '/healthz': api,
      '/sub/': api,
      '/script/': api,
    },
  },
})
