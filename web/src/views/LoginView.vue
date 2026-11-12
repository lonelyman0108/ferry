<script setup lang="ts">
import { CircleAlertIcon, LoaderCircleIcon } from '@lucide/vue'
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { errorText } from '@/api'
import BrandMark from '@/components/BrandMark.vue'
import FormField from '@/components/FormField.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useFormCheck } from '@/lib/form'
import { useAuth } from '@/stores/auth'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const form = reactive({ username: '', password: '' })
const loading = ref(false)
// 登录接口返回的错误显示在登录按钮上方，两个输入框同时标为无效，并用 aria-describedby 指向这条报错；
// 改动任一输入框后清除。
const failed = ref('')
const failedId = 'login-failed'
const check = useFormCheck(() => ({
  username: !form.username.trim() && '请填写用户名',
  password: !form.password && '请填写密码',
}))
watch(
  () => [form.username, form.password],
  () => (failed.value = ''),
)

onMounted(() => document.getElementById('username')?.focus())

async function submit() {
  failed.value = ''
  if (!check.ok()) return
  loading.value = true
  try {
    await auth.login(form.username, form.password)
    const next =
      typeof route.query.next === 'string' && route.query.next.startsWith('/')
        ? route.query.next
        : '/'
    await router.replace(next)
  } catch (e) {
    failed.value = errorText(e)
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
        <form class="grid gap-4" novalidate @submit.prevent="submit">
          <FormField label="用户名" for="username" :error="check.errors.username">
            <Input
              id="username"
              v-model="form.username"
              autocomplete="username"
              :aria-invalid="failed ? true : undefined"
              :aria-describedby="failed ? failedId : undefined"
            />
          </FormField>
          <FormField label="密码" for="password" :error="check.errors.password">
            <Input
              id="password"
              v-model="form.password"
              type="password"
              autocomplete="current-password"
              :aria-invalid="failed ? true : undefined"
              :aria-describedby="failed ? failedId : undefined"
            />
          </FormField>
          <p
            v-if="failed"
            :id="failedId"
            role="alert"
            class="border-destructive/25 bg-destructive/10 text-destructive flex items-start gap-2 rounded-md border px-3 py-2 text-sm"
          >
            <CircleAlertIcon class="mt-0.5 size-4 shrink-0" />
            <span>登录失败：{{ failed }}</span>
          </p>
          <Button type="submit" :disabled="loading" class="w-full">
            <LoaderCircleIcon v-if="loading" class="animate-spin" />
            登录
          </Button>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
