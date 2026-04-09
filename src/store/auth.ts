import { create } from 'zustand'
import { UserRole } from '@/lib/api'

interface AuthStore {
  token: string | null
  role: UserRole | null
  username: string | null
  setToken: (token: string, role: UserRole, username: string) => void
  clearAuth: () => void
}

// Retrieves auth state from storage.
// Token prefers sessionStorage (XSS protection); falls back to localStorage.
// role and username remain in localStorage (less sensitive).
function getStoredAuth() {
  return {
    token: sessionStorage.getItem('omnipus_auth_token') ?? localStorage.getItem('omnipus_auth_token'),
    role: localStorage.getItem('omnipus_auth_role') as UserRole | null,
    username: localStorage.getItem('omnipus_auth_username'),
  }
}

export const useAuthStore = create<AuthStore>((set) => ({
  ...getStoredAuth(),
  setToken: (token, role, username) => {
    sessionStorage.setItem('omnipus_auth_token', token) // sessionStorage for token (XSS protection)
    localStorage.setItem('omnipus_auth_role', role)
    localStorage.setItem('omnipus_auth_username', username)
    set({ token, role, username })
  },
  clearAuth: () => {
    sessionStorage.removeItem('omnipus_auth_token')
    localStorage.removeItem('omnipus_auth_role')
    localStorage.removeItem('omnipus_auth_username')
    set({ token: null, role: null, username: null })
  },
}))
