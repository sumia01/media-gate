<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'
import { useAuth } from '@/composables/useAuth'

const { currentUser, fetchProfile } = useAuth()

const firstName = ref('')
const lastName = ref('')
const birthYear = ref<number | undefined>()
const profileMsg = ref('')
const profileErr = ref('')
const profileLoading = ref(false)

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const pwMsg = ref('')
const pwErr = ref('')
const pwLoading = ref(false)

onMounted(() => {
  if (currentUser.value) {
    firstName.value = currentUser.value.firstName ?? ''
    lastName.value = currentUser.value.lastName ?? ''
    birthYear.value = currentUser.value.birthYear ?? undefined
  }
})

async function saveProfile() {
  profileMsg.value = ''
  profileErr.value = ''
  profileLoading.value = true
  try {
    const { error } = await client.PUT('/auth/me', {
      body: {
        firstName: firstName.value || undefined,
        lastName: lastName.value || undefined,
        birthYear: birthYear.value ?? undefined,
      },
    })
    if (error) throw new Error((error as any).message || 'Failed to update profile')
    await fetchProfile()
    profileMsg.value = 'Profile updated'
  } catch (e: any) {
    profileErr.value = e.message
  } finally {
    profileLoading.value = false
  }
}

async function changePassword() {
  pwMsg.value = ''
  pwErr.value = ''

  if (newPassword.value !== confirmPassword.value) {
    pwErr.value = 'New passwords do not match'
    return
  }
  if (!newPassword.value) {
    pwErr.value = 'New password is required'
    return
  }

  pwLoading.value = true
  try {
    const { error } = await client.POST('/auth/change-password', {
      body: {
        oldPassword: oldPassword.value,
        newPassword: newPassword.value,
      },
    })
    if (error) throw new Error((error as any).message || 'Failed to change password')
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    pwMsg.value = 'Password changed'
  } catch (e: any) {
    pwErr.value = e.message
  } finally {
    pwLoading.value = false
  }
}
</script>

<template>
  <div class="max-w-2xl space-y-8">
    <h1 class="text-2xl font-bold text-gray-200">Profile</h1>

    <!-- Profile form -->
    <form class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl p-6 space-y-5" @submit.prevent="saveProfile">
      <h2 class="text-lg font-semibold text-gray-300">Personal Information</h2>

      <div v-if="profileMsg" class="px-3 py-2 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-sm">
        {{ profileMsg }}
      </div>
      <div v-if="profileErr" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ profileErr }}
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Email</label>
        <input
          type="email"
          :value="currentUser?.email"
          disabled
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-500 text-sm cursor-not-allowed"
        />
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">First Name</label>
          <input
            v-model="firstName"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Last Name</label>
          <input
            v-model="lastName"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Birth Year</label>
        <input
          v-model.number="birthYear"
          type="number"
          min="1900"
          max="2020"
          placeholder="e.g. 1990"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
        />
      </div>

      <button
        type="submit"
        :disabled="profileLoading"
        class="py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white text-sm font-medium transition-colors"
      >
        {{ profileLoading ? 'Saving...' : 'Save Changes' }}
      </button>
    </form>

    <!-- Change password -->
    <form class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl p-6 space-y-5" @submit.prevent="changePassword">
      <h2 class="text-lg font-semibold text-gray-300">Change Password</h2>

      <div v-if="pwMsg" class="px-3 py-2 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-sm">
        {{ pwMsg }}
      </div>
      <div v-if="pwErr" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ pwErr }}
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Current Password</label>
        <input
          v-model="oldPassword"
          type="password"
          required
          autocomplete="current-password"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
        />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">New Password</label>
        <input
          v-model="newPassword"
          type="password"
          required
          autocomplete="new-password"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
        />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Confirm New Password</label>
        <input
          v-model="confirmPassword"
          type="password"
          required
          autocomplete="new-password"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
        />
      </div>

      <button
        type="submit"
        :disabled="pwLoading"
        class="py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white text-sm font-medium transition-colors"
      >
        {{ pwLoading ? 'Changing...' : 'Change Password' }}
      </button>
    </form>
  </div>
</template>
