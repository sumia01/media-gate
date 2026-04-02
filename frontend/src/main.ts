import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './style.css'
import { configureAuth } from './api/client'
import { useAuth } from './composables/useAuth'

const { getAccessToken, refresh, logout } = useAuth()

configureAuth(
  getAccessToken,
  refresh,
  () => {
    logout()
    router.push('/login')
  },
)

const app = createApp(App)

app.use(router)

app.mount('#app')
