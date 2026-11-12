import { defineStore } from 'pinia'
import { ref } from 'vue'

import { api } from '../api'

export interface User {
  id: number
  username: string
}

export const useAuth = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const checked = ref(false)

  async function load() {
    try {
      user.value = (await api.get<User>('/auth/me')).data
    } catch {
      user.value = null
    } finally {
      checked.value = true
    }
  }

  async function login(username: string, password: string) {
    user.value = (await api.post<User>('/auth/login', { username, password })).data
  }

  async function logout() {
    await api.post('/auth/logout', {})
    user.value = null
  }

  return { user, checked, load, login, logout }
})
