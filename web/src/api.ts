import axios from 'axios'

export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('dbops_access_token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

let refreshing = false
let waiters: Array<(token: string) => void> = []

api.interceptors.response.use(
  response => response,
  async error => {
    const original = error.config
    if (error.response?.status !== 401 || original?._retry) throw error
    const refreshToken = localStorage.getItem('dbops_refresh_token')
    if (!refreshToken) throw error

    original._retry = true
    if (refreshing) {
      return new Promise(resolve => {
        waiters.push((token) => {
          original.headers.Authorization = `Bearer ${token}`
          resolve(api(original))
        })
      })
    }

    refreshing = true
    try {
      const response = await axios.post('/api/v1/auth/refresh', { refresh_token: refreshToken })
      const pair = response.data.data
      localStorage.setItem('dbops_access_token', pair.access_token)
      localStorage.setItem('dbops_refresh_token', pair.refresh_token)
      for (const waiter of waiters) waiter(pair.access_token)
      waiters = []
      original.headers.Authorization = `Bearer ${pair.access_token}`
      return api(original)
    } finally {
      refreshing = false
    }
  },
)

export async function getData<T>(url: string): Promise<T> {
  const response = await api.get(url)
  return response.data.data as T
}
