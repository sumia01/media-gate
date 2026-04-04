<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'

const router = useRouter()
const { login } = useAuth()

const email = ref('')
const password = ref('')
const rememberMe = ref(false)
const error = ref('')
const loading = ref(false)

async function handleSubmit() {
  error.value = ''
  loading.value = true
  try {
    await login(email.value, password.value, rememberMe.value)
    router.push('/')
  } catch (e: any) {
    error.value = e.message || 'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-[#0f172a] px-4">
    <div class="w-full max-w-sm">
      <!-- Brand -->
      <div class="text-center mb-8">
        <img src="/small_logo.png" alt="MediaGate" class="w-14 h-14 mb-3 mx-auto" />
        <h1 class="text-xl font-semibold text-[#c4b5fd]" style="text-shadow: 0 0 12px rgba(255, 255, 255, 0.3)">MediaGate</h1>
      </div>

      <!-- Card -->
      <form
        class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl p-6 space-y-5"
        @submit.prevent="handleSubmit"
      >
        <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
          {{ error }}
        </div>

        <div>
          <label for="email" class="block text-sm font-medium text-gray-400 mb-1.5">Email</label>
          <input
            id="email"
            v-model="email"
            type="email"
            required
            autocomplete="email"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="you@example.com"
          />
        </div>

        <div>
          <label for="password" class="block text-sm font-medium text-gray-400 mb-1.5">Password</label>
          <input
            id="password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="Password"
          />
        </div>

        <div class="flex items-center gap-2">
          <input
            id="remember"
            v-model="rememberMe"
            type="checkbox"
            class="w-4 h-4 rounded bg-[#161b2e] border-violet-800/30 text-violet-600 focus:ring-violet-500/50"
          />
          <label for="remember" class="text-sm text-gray-400">Remember me</label>
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
        >
          {{ loading ? 'Signing in...' : 'Sign In' }}
        </button>
      </form>
    </div>
  </div>
</template>
