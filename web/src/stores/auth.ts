import { defineStore } from 'pinia'
import { api } from '../api'

export interface User {
  id: number
  username: string
  display_name?: string
  roles?: string[]
  role?: string
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as User | null,
    loading: false,
  }),
  getters: {
    authenticated: () => Boolean(localStorage.getItem('dbops_access_token')),
  },
  actions: {
    async login(username: string, password: string) {
      this.loading = true
      try {
        const response = await api.post('/auth/login', { username, password })
        const pair = response.data.data
        localStorage.setItem('dbops_access_token', pair.access_token)
        localStorage.setItem('dbops_refresh_token', pair.refresh_token)
        this.user = pair.user
      } finally {
        this.loading = false
      }
    },
    async loadMe() {
      if (!this.authenticated) return
      const response = await api.get('/auth/me')
      this.user = response.data.data
    },
    logout() {
      localStorage.removeItem('dbops_access_token')
      localStorage.removeItem('dbops_refresh_token')
      this.user = null
    },
  },
})
