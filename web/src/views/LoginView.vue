<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import BrandMark from '@/components/BrandMark.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useAuth } from '@/stores/auth'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const form = reactive({ username: '', password: '' })
const loading = ref(false)

async function submit() {
  loading.value = true
  try {
    await auth.login(form.username, form.password)
    const next =
      typeof route.query.next === 'string' && route.query.next.startsWith('/')
        ? route.query.next
        : '/'
    await router.replace(next)
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div
    class="from-primary/8 flex min-h-svh flex-col items-center bg-linear-to-b to-transparent to-50% px-4 pt-[12vh]"
  >
    <div class="mb-6 flex flex-col items-center gap-2 text-center">
      <BrandMark class="size-11 rounded-xl [&>svg]:size-6" />
      <div class="text-xl font-semibold tracking-wide">Ferry</div>
    </div>
    <Card class="h-fit w-full max-w-sm">
      <CardHeader><CardTitle>登录</CardTitle></CardHeader>
      <CardContent>
        <form class="grid gap-4" @submit.prevent="submit">
          <div class="grid gap-2">
            <Label for="username">用户名</Label>
            <Input id="username" v-model="form.username" autocomplete="username" />
          </div>
          <div class="grid gap-2">
            <Label for="password">密码</Label>
            <Input
              id="password"
              v-model="form.password"
              type="password"
              autocomplete="current-password"
            />
          </div>
          <Button type="submit" :disabled="loading" class="w-full">登录</Button>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
