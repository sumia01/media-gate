<script setup lang="ts">
import { ref } from 'vue'
import { useAuth } from '@/composables/useAuth'

const emit = defineEmits<{ next: [] }>()

const { setup } = useAuth()

const email = ref('')
const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const loading = ref(false)

const passwordMismatch = ref(false)

function checkMatch() {
  passwordMismatch.value = confirmPassword.value !== '' && password.value !== confirmPassword.value
}

async function handleSubmit() {
  if (password.value !== confirmPassword.value) {
    error.value = 'Passwords do not match'
    return
  }
  if (password.value.length < 6) {
    error.value = 'Password must be at least 6 characters'
    return
  }

  error.value = ''
  loading.value = true
  try {
    await setup(email.value, password.value)
    emit('next')
  } catch (e: any) {
    error.value = e.message || 'Failed to create account'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-gray-200">Create Admin Account</h2>
      <p class="text-sm text-gray-500 mt-1">
        Set up the first user account. This will be the administrator of your Media Gate instance.
      </p>
    </div>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ error }}
      </div>

      <div>
        <label for="setup-email" class="block text-sm font-medium text-gray-400 mb-1.5">Email</label>
        <input
          id="setup-email"
          v-model="email"
          type="email"
          required
          autocomplete="email"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          placeholder="admin@example.com"
        />
      </div>

      <div>
        <label for="setup-password" class="block text-sm font-medium text-gray-400 mb-1.5">Password</label>
        <input
          id="setup-password"
          v-model="password"
          type="password"
          required
          autocomplete="new-password"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          placeholder="At least 6 characters"
          @input="checkMatch"
        />
      </div>

      <div>
        <label for="setup-confirm" class="block text-sm font-medium text-gray-400 mb-1.5">Confirm Password</label>
        <input
          id="setup-confirm"
          v-model="confirmPassword"
          type="password"
          required
          autocomplete="new-password"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          :class="{ 'border-red-500/50': passwordMismatch }"
          placeholder="Repeat password"
          @input="checkMatch"
        />
        <p v-if="passwordMismatch" class="text-xs text-red-400 mt-1">Passwords do not match</p>
      </div>

      <button
        type="submit"
        :disabled="loading || !email || !password || !confirmPassword || passwordMismatch"
        class="w-full py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
      >
        {{ loading ? 'Creating account...' : 'Create Account & Continue' }}
      </button>
    </form>
  </div>
</template>
