import type { WsEvent } from "./types"
import api from "./api"

type WsHandler = (event: WsEvent) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, WsHandler[]> = new Map()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private url: string

  constructor(path: string) {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
    this.url = `${proto}//${window.location.host}${path}`
  }

  async connect() {
    if (this.ws?.readyState === WebSocket.OPEN) return

    // Get one-time ticket for WS auth
    try {
      const res = await api.post("/api/ws/ticket")
      const ticket = res.data.data.ticket
      const separator = this.url.includes("?") ? "&" : "?"
      this.ws = new WebSocket(`${this.url}${separator}ticket=${ticket}`)
    } catch (err) {
      console.error("Failed to get WS ticket:", err)
      this.scheduleReconnect()
      return
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
    }

    this.ws.onmessage = (event) => {
      try {
        const wsEvent: WsEvent = JSON.parse(event.data)
        const handlers = this.handlers.get(wsEvent.event) || []
        handlers.forEach((h) => h(wsEvent))

        // Also fire wildcard handlers
        const wildcardHandlers = this.handlers.get("*") || []
        wildcardHandlers.forEach((h) => h(wsEvent))
      } catch {
        // Ignore non-JSON messages
      }
    }

    this.ws.onclose = () => {
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close()
    this.ws = null
  }

  on(event: string, handler: WsHandler) {
    const existing = this.handlers.get(event) || []
    existing.push(handler)
    this.handlers.set(event, existing)
  }

  off(event: string, handler: WsHandler) {
    const existing = this.handlers.get(event) || []
    this.handlers.set(
      event,
      existing.filter((h) => h !== handler)
    )
  }

  private reconnectAttempts = 0

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    // Don't reconnect if user is not authenticated
    if (!localStorage.getItem("access_token")) return
    // Exponential backoff: 3s, 6s, 12s, 24s, max 30s
    const delay = Math.min(3000 * Math.pow(2, this.reconnectAttempts), 30000)
    this.reconnectAttempts++
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }
}

export const globalWs = new WebSocketClient("/ws")
