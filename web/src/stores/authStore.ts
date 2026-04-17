import { create } from "zustand"
import type { User, AuthResponse, ApiResponse } from "../lib/types"
import api from "../lib/api"
import { globalWs } from "../lib/ws"

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  isLoading: boolean

  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  fetchProfile: () => Promise<void>
  setUser: (user: User) => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isAuthenticated: false,
  isLoading: false,

  login: async (username, password) => {
    const res = await api.post<ApiResponse<AuthResponse>>("/login", { username, password })
    if (res.data.success && res.data.data) {
      const { user } = res.data.data
      set({ user, isAuthenticated: true })
    } else {
      throw new Error(res.data.message)
    }
  },

  logout: async () => {
    try {
      await api.post("/logout")
    } catch {
      // Ignore logout errors
    }
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
      set({ user: null, isAuthenticated: false })
    } finally {
      set({ isLoading: false })
    }
  },

  setUser: (user) => set({ user }),
}))
