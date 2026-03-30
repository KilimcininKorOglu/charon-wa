import axios from "axios"
import type { ApiResponse } from "./types"
import { getRefreshToken, setRefreshToken } from "../stores/authStore"

const api = axios.create({
  baseURL: "/",
  headers: { "Content-Type": "application/json" },
})

// Request interceptor: attach JWT token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem("access_token")
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: handle 401 + token refresh
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const original = error.config
    if (error.response?.status === 401 && !original._retry) {
      original._retry = true
      const refreshToken = getRefreshToken()
      if (refreshToken) {
        try {
          const res = await axios.post<ApiResponse<{ access_token: string; refresh_token: string }>>(
            "/refresh",
            { refresh_token: refreshToken }
          )
          if (res.data.success && res.data.data) {
            localStorage.setItem("access_token", res.data.data.access_token)
            if (res.data.data.refresh_token) {
              setRefreshToken(res.data.data.refresh_token)
            }
            original.headers.Authorization = `Bearer ${res.data.data.access_token}`
            return api(original)
          }
        } catch {
          localStorage.removeItem("access_token")
          setRefreshToken(null)
          window.location.href = "/login"
        }
      } else {
        window.location.href = "/login"
      }
    }
    return Promise.reject(error)
  }
)

export default api
