import { create } from 'zustand'
import type { WsConnection } from '@/lib/ws'

interface ConnectionStore {
  connection: WsConnection | null
  isConnected: boolean
  connectionError: string | null
  setConnection: (conn: WsConnection | null) => void
  setConnected: (connected: boolean) => void
  setConnectionError: (error: string | null) => void
  reconnect: () => void
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  connection: null,
  isConnected: false,
  connectionError: null,
  setConnection: (conn) => set({ connection: conn }),
  setConnected: (connected) =>
    set({ isConnected: connected, connectionError: connected ? null : get().connectionError }),
  setConnectionError: (error) => set({ connectionError: error }),

  reconnect: () => {
    const { connection } = get()
    if (!connection) {
      set({ connectionError: 'Cannot reconnect — please refresh the page.' })
      return
    }
    set({ connectionError: null })
    connection.connect()
  },
}))
