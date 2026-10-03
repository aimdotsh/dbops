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

let refreshFlight: Promise<string> | null = null
api.interceptors.response.use(response => response, async error => {
 const original = error.config
 if (error.response?.status !== 401 || !original || original._retry || ['/auth/login', '/auth/refresh'].includes(original.url)) throw error
 const token = localStorage.getItem('dbops_refresh_token')
 if (!token) throw error
 original._retry = true
 if (!refreshFlight) {
  refreshFlight = axios.post('/api/v1/auth/refresh', { refresh_token: token }, { timeout: 15000 })
   .then(response => {
    const pair = response.data.data
    localStorage.setItem('dbops_access_token', pair.access_token)
    localStorage.setItem('dbops_refresh_token', pair.refresh_token)
    return pair.access_token as string
   }).catch(err => {
    localStorage.removeItem('dbops_access_token')
    localStorage.removeItem('dbops_refresh_token')
    window.location.assign('/login')
    throw err
   }).finally(() => { refreshFlight = null })
 }
 const access = await refreshFlight
 original.headers.Authorization = `Bearer ${access}`
 return api(original)
})

export async function getData<T>(url: string): Promise<T> {
  const response = await api.get(url)
  return (response.data.data ?? []) as T
}
