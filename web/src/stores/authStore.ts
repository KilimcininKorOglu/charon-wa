import { create } from "zustand"
import type { User, AuthResponse, ApiResponse } from "../lib/types"
import api from "../lib/api"
import { globalWs } from "../lib/ws"

// In-memory only — not stored in localStorage to prevent XSS exfiltration
let refreshTokenMemory: string | null = null

export function getRefreshToken() { return refreshTokenMemory }
export function setRefreshToken(token: string | null) { refreshTokenMemory = token }

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  isLoading: boolean

  login: (username: string, password: string) => Promise<void>
  register: (username: string, email: string, password: string, fullName?: string) => Promise<void>
  logout: () => Promise<void>
  fetchProfile: () => Promise<void>
  setUser: (user: User) => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isAuthenticated: !!localStorage.getItem("access_token"),
  isLoading: false,

  login: async (username, password) => {
    const res = await api.post<ApiResponse<AuthResponse>>("/login", { username, password })
    if (res.data.success && res.data.data) {
      const { access_token, refresh_token, user } = res.data.data
      localStorage.setItem("access_token", access_token)
      setRefreshToken(refresh_token)
      set({ user, isAuthenticated: true })
    } else {
      throw new Error(res.data.message)
    }
  },

  register: async (username, email, password, fullName) => {
    const res = await api.post<ApiResponse<AuthResponse>>("/register", {
      username,
      email,
      password,
      full_name: fullName,
    })
    if (res.data.success && res.data.data) {
      const { access_token, refresh_token, user } = res.data.data
      localStorage.setItem("access_token", access_token)
      setRefreshToken(refresh_token)
      set({ user, isAuthenticated: true })
    } else {
      throw new Error(res.data.message)
    }
  },

  logout: async () => {
    const refreshToken = getRefreshToken()
    try {
      await api.post("/api/logout", { refresh_token: refreshToken })
    } catch {
      // Ignore logout errors
    }
    localStorage.removeItem("access_token")
    setRefreshToken(null)
    globalWs.disconnect()
    set({ user: null, isAuthenticated: false })
  },

  fetchProfile: async () => {
    set({ isLoading: true })
    try {
      const res = await api.get<ApiResponse<User>>("/api/me")
      if (res.data.success && res.data.data) {
        set({ user: res.data.data, isAuthenticated: true })
      }
    } catch {
      localStorage.removeItem("access_token")
      setRefreshToken(null)
      set({ user: null, isAuthenticated: false })
    } finally {
      set({ isLoading: false })
    }
  },

  setUser: (user) => set({ user }),
}))
