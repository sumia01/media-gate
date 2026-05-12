<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'
import { useAuth } from '@/composables/useAuth'
import type { UserProfile } from '@/types/api'

const { isAdmin } = useAuth()

const users = ref<UserProfile[]>([])
const loading = ref(true)
const error = ref('')

// Registration form
const showForm = ref(false)
const regEmail = ref('')
const regPassword = ref('')
const regFirstName = ref('')
const regLastName = ref('')
const regBirthYear = ref<number | undefined>()
const regErr = ref('')
const regLoading = ref(false)

// Delete confirmation
const deleteTarget = ref<UserProfile | null>(null)

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const { data, error: err } = await client.GET('/users')
    if (err) throw new Error((err as any).message || 'Failed to load users')
    users.value = (data as any).users ?? []
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function registerUser() {
  regErr.value = ''
  regLoading.value = true
  try {
    const { error: err } = await client.POST('/users', {
      body: {
        email: regEmail.value,
        password: regPassword.value,
        firstName: regFirstName.value || undefined,
        lastName: regLastName.value || undefined,
        birthYear: regBirthYear.value ?? undefined,
      },
    })
    if (err) throw new Error((err as any).message || 'Failed to register user')
    regEmail.value = ''
    regPassword.value = ''
    regFirstName.value = ''
    regLastName.value = ''
    regBirthYear.value = undefined
    showForm.value = false
    await loadUsers()
  } catch (e: any) {
    regErr.value = e.message
  } finally {
    regLoading.value = false
  }
}

async function deleteUser(user: UserProfile) {
  try {
    await client.DELETE('/users/{id}', { params: { path: { id: user.id } } })
    deleteTarget.value = null
    await loadUsers()
  } catch {
    // Silently fail — user stays in list.
  }
}

function formatDate(date: string) {
  return new Date(date).toLocaleDateString()
}

onMounted(loadUsers)
</script>

<template>
  <div class="max-w-4xl space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-200">Users</h1>
      <button
        v-if="isAdmin"
        class="py-2 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors"
        @click="showForm = !showForm"
      >
        {{ showForm ? 'Cancel' : 'Add User' }}
      </button>
    </div>

    <!-- Register form -->
    <form
      v-if="showForm"
      class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl p-6 space-y-4"
      @submit.prevent="registerUser"
    >
      <h2 class="text-lg font-semibold text-gray-300">New User</h2>

      <div v-if="regErr" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ regErr }}
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Email</label>
          <input
            v-model="regEmail"
            type="email"
            required
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="user@example.com"
          />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Password</label>
          <input
            v-model="regPassword"
            type="password"
            required
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="Password"
          />
        </div>
      </div>

      <div class="grid grid-cols-3 gap-4">
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">First Name</label>
          <input
            v-model="regFirstName"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Last Name</label>
          <input
            v-model="regLastName"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Birth Year</label>
          <input
            v-model.number="regBirthYear"
            type="number"
            min="1900"
            max="2020"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
      </div>

      <button
        type="submit"
        :disabled="regLoading"
        class="py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white text-sm font-medium transition-colors"
      >
        {{ regLoading ? 'Creating...' : 'Create User' }}
      </button>
    </form>

    <!-- Users table -->
    <div class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl overflow-hidden">
      <div v-if="loading" class="px-6 py-8 text-center text-gray-500 text-sm">Loading...</div>
      <div v-else-if="error" class="px-6 py-8 text-center text-red-400 text-sm">{{ error }}</div>
      <table v-else class="w-full">
        <thead>
          <tr class="border-b border-violet-900/20">
            <th class="px-6 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">Email</th>
            <th class="px-6 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">Name</th>
            <th class="px-6 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">Created</th>
            <th class="px-6 py-3 text-right text-xs font-semibold uppercase tracking-wider text-gray-500">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="user in users"
            :key="user.id"
            class="border-b border-violet-900/10 last:border-0"
          >
            <td class="px-6 py-4 text-sm text-gray-300">{{ user.email }}</td>
            <td class="px-6 py-4 text-sm text-gray-400">
              {{ [user.firstName, user.lastName].filter(Boolean).join(' ') || '—' }}
            </td>
            <td class="px-6 py-4 text-sm text-gray-500">{{ formatDate(user.createdAt) }}</td>
            <td class="px-6 py-4 text-right">
              <template v-if="isAdmin && !user.isAdmin">
                <button
                  v-if="deleteTarget?.id !== user.id"
                  class="text-sm text-red-400/60 hover:text-red-400 transition-colors"
                  @click="deleteTarget = user"
                >
                  Delete
                </button>
                <span v-else class="inline-flex items-center gap-2">
                  <button
                    class="text-sm text-red-400 hover:text-red-300 transition-colors"
                    @click="deleteUser(user)"
                  >
                    Confirm
                  </button>
                  <button
                    class="text-sm text-gray-500 hover:text-gray-300 transition-colors"
                    @click="deleteTarget = null"
                  >
                    Cancel
                  </button>
                </span>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
