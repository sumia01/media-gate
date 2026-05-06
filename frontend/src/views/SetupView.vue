<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import { useAuth } from '@/composables/useAuth'
import SetupAccount from './setup/SetupAccount.vue'
import SetupBasePath from './setup/SetupBasePath.vue'
import SetupIndexer from './setup/SetupIndexer.vue'
import SetupTmdb from './setup/SetupTmdb.vue'
import SetupTorrentClient from './setup/SetupTorrentClient.vue'
import SetupTvdb from './setup/SetupTvdb.vue'

const router = useRouter()
const { getSetupStatus, clearSetupStatusCache } = useAuth()

const currentStep = ref(0)
const loading = ref(true)

const steps = [
  { label: 'Account', component: SetupAccount },
  { label: 'Base Path', component: SetupBasePath },
  { label: 'Torrent', component: SetupTorrentClient },
  { label: 'Indexer', component: SetupIndexer },
  { label: 'TMDB', component: SetupTmdb },
  { label: 'TVDB', component: SetupTvdb },
]

const currentComponent = computed(() => steps[currentStep.value]?.component)

onMounted(async () => {
  try {
    const status = await getSetupStatus()
    if (status.onboardingCompleted) {
      router.replace('/')
      return
    }
    if (status.onboardingStep > 0) {
      currentStep.value = Math.min(status.onboardingStep, steps.length - 1)
    }
  } catch {
    // Start from beginning
  } finally {
    loading.value = false
  }
})

async function persistStep(step: number) {
  try {
    await client.PUT('/settings', {
      body: {
        onboardingStep: step,
      },
    })
  } catch {
    // Non-critical
  }
}

async function handleNext() {
  const nextStep = currentStep.value + 1
  if (nextStep >= steps.length) {
    // Final step — mark onboarding as completed
    try {
      await client.PUT('/settings', {
        body: {
          onboardingCompleted: true,
          onboardingStep: steps.length,
        },
      })
      clearSetupStatusCache()
      router.replace('/libraries')
    } catch {
      // Try redirect anyway
      router.replace('/libraries')
    }
    return
  }
  currentStep.value = nextStep
  persistStep(nextStep)
}

function handleBack() {
  if (currentStep.value > 0) {
    currentStep.value--
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-[#0f172a] px-4 py-12">
    <div class="w-full max-w-2xl">
      <!-- Brand -->
      <div class="text-center mb-6">
        <img src="/small_logo.png" alt="MediaGate" class="w-12 h-12 mb-3 mx-auto" />
        <h1 class="text-xl font-semibold text-[#c4b5fd]" style="text-shadow: 0 0 12px rgba(255, 255, 255, 0.3)">MediaGate Setup</h1>
      </div>

      <!-- Stepper -->
      <div v-if="!loading" class="flex items-center justify-center gap-1 mb-8">
        <template v-for="(step, i) in steps" :key="i">
          <div class="flex items-center gap-1">
            <!-- Step circle -->
            <div
              class="flex items-center justify-center w-7 h-7 rounded-full text-xs font-semibold transition-colors"
              :class="
                i < currentStep
                  ? 'bg-violet-600 text-white'
                  : i === currentStep
                    ? 'ring-2 ring-violet-500 text-violet-400 bg-transparent'
                    : 'bg-[#161b2e] text-gray-600'
              "
            >
              <svg
                v-if="i < currentStep"
                class="w-3.5 h-3.5"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                viewBox="0 0 24 24"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
              </svg>
              <span v-else>{{ i + 1 }}</span>
            </div>
            <!-- Label -->
            <span
              class="text-xs font-medium hidden sm:inline"
              :class="
                i <= currentStep ? 'text-gray-300' : 'text-gray-600'
              "
            >
              {{ step.label }}
            </span>
          </div>
          <!-- Connector line -->
          <div
            v-if="i < steps.length - 1"
            class="w-6 h-px mx-0.5"
            :class="i < currentStep ? 'bg-violet-600' : 'bg-[#161b2e]'"
          />
        </template>
      </div>

      <!-- Card -->
      <div
        v-if="!loading"
        class="bg-[#0c0f1a] border border-violet-900/20 rounded-xl p-6"
      >
        <component
          :is="currentComponent"
          @next="handleNext"
          @back="handleBack"
        />
      </div>

      <div v-else class="text-center text-gray-500 text-sm">Loading...</div>
    </div>
  </div>
</template>
