import { useEffect, useState, useRef, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Input } from "../../components/ui/Input"
import { Badge } from "../../components/ui/Badge"
import { Send, Paperclip, Smartphone, ChevronDown } from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, Instance, WsEvent } from "../../lib/types"
import toast from "react-hot-toast"

interface ChatMessage {
  id: string
  from: string
  message: string
  fromMe: boolean
  timestamp: number
  pushName?: string
}

export function MessagesPage() {
  const [instances, setInstances] = useState<Instance[]>([])
  const [selectedInstance, setSelectedInstance] = useState<string>("")
  const [recipient, setRecipient] = useState("")
  const [message, setMessage] = useState("")
  const [sending, setSending] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [wsConnected, setWsConnected] = useState(false)
  const chatEndRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  // Fetch connected instances
  useEffect(() => {
    const fetch = async () => {
      try {
        const res = await api.get<ApiResponse<Instance[]>>("/api/instances?all=true")
        if (res.data.success && res.data.data) {
          setInstances(res.data.data.filter((i) => i.connected))
        }
      } catch {
        // ignore
      }
    }
    fetch()
  }, [])

  // Instance-specific WebSocket for incoming messages
  useEffect(() => {
    if (!selectedInstance) return

    const token = localStorage.getItem("access_token")
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
    const wsUrl = `${proto}//${window.location.host}/api/listen/${selectedInstance}?token=${token}`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => setWsConnected(true)
    ws.onclose = () => setWsConnected(false)
    ws.onerror = () => setWsConnected(false)

    ws.onmessage = (event) => {
      try {
        const wsEvent: WsEvent = JSON.parse(event.data)
        if (wsEvent.event === "incoming_message") {
          const data = wsEvent.data as Record<string, unknown>
          const msg: ChatMessage = {
            id: (data.message_id as string) || crypto.randomUUID(),
            from: data.from as string,
            message: data.message as string,
            fromMe: data.from_me as boolean,
            timestamp: data.timestamp as number,
            pushName: data.push_name as string,
          }
          setMessages((prev) => [...prev, msg])
        }
      } catch {
        // ignore
      }
    }

    return () => {
      ws.close()
      wsRef.current = null
      setWsConnected(false)
    }
  }, [selectedInstance])

  // Auto-scroll to bottom
  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  const handleSend = useCallback(async () => {
    if (!message.trim() || !selectedInstance || !recipient.trim()) return

    setSending(true)
    try {
      const res = await api.post<ApiResponse>(`/api/send/${selectedInstance}`, {
        to: recipient,
        message: message,
      })
      if (res.data.success) {
        setMessages((prev) => [
          ...prev,
          {
            id: crypto.randomUUID(),
            from: "me",
            message: message,
            fromMe: true,
            timestamp: Math.floor(Date.now() / 1000),
          },
        ])
        setMessage("")
      } else {
        toast.error(res.data.message)
      }
    } catch {
      toast.error("Failed to send message")
    } finally {
      setSending(false)
    }
  }, [message, selectedInstance, recipient])

  return (
    <div className="flex gap-4 h-[calc(100vh-7rem)]">
      {/* Left: Instance + Recipient selector */}
      <div className="w-72 shrink-0 flex flex-col gap-3">
        <h2 className="text-xl font-bold text-cyber-green">Messages</h2>

        {/* Instance selector */}
        <Card>
          <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-2">
            <Smartphone size={12} className="inline mr-1" />
            Instance
          </label>
          <div className="relative">
            <select
              value={selectedInstance}
              onChange={(e) => {
                setSelectedInstance(e.target.value)
                setMessages([])
              }}
              className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50 appearance-none cursor-pointer"
            >
              <option value="">Select instance</option>
              {instances.map((inst) => (
                <option key={inst.instanceId} value={inst.instanceId}>
                  {inst.instanceId} {inst.phoneNumber ? `(${inst.phoneNumber})` : ""}
                </option>
              ))}
            </select>
            <ChevronDown size={14} className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-green-muted pointer-events-none" />
          </div>
        </Card>

        {/* Recipient */}
        <Card>
          <Input
            label="Recipient"
            value={recipient}
            onChange={(e) => setRecipient(e.target.value)}
            placeholder="628xxxxxxxxxx"
          />
        </Card>

        {/* Status */}
        <Card>
          <div className="flex items-center gap-2">
            <span className={`w-2 h-2 rounded-full ${wsConnected ? "bg-cyber-green animate-pulse" : "bg-cyber-green-muted"}`} />
            <span className="text-xs text-cyber-green-muted">
              {wsConnected ? "Listening for messages" : selectedInstance ? "Connecting..." : "Select an instance"}
            </span>
          </div>
        </Card>

        {/* Message count */}
        {messages.length > 0 && (
          <Badge variant="info" className="self-start">
            {messages.length} messages
          </Badge>
        )}
      </div>

      {/* Right: Chat area */}
      <div className="flex-1 flex flex-col">
        {/* Chat messages */}
        <Card className="flex-1 overflow-y-auto mb-3 min-h-0">
          {!selectedInstance ? (
            <div className="flex items-center justify-center h-full">
              <p className="text-cyber-green-muted text-sm">Select an instance to start messaging</p>
            </div>
          ) : messages.length === 0 ? (
            <div className="flex items-center justify-center h-full">
              <p className="text-cyber-green-muted text-sm">No messages yet. Send or receive a message.</p>
            </div>
          ) : (
            <div className="space-y-3 p-2">
              {messages.map((msg) => (
                <div
                  key={msg.id}
                  className={`flex ${msg.fromMe ? "justify-end" : "justify-start"}`}
                >
                  <div
                    className={`max-w-[70%] px-3 py-2 text-sm ${
                      msg.fromMe
                        ? "bg-cyber-green/10 border border-cyber-green/20 text-cyber-green"
                        : "bg-bg-hover border border-border text-cyber-white"
                    }`}
                  >
                    {!msg.fromMe && msg.pushName && (
                      <p className="text-xs text-cyber-green-dim font-bold mb-1">{msg.pushName}</p>
                    )}
                    <p className="break-words whitespace-pre-wrap">{msg.message}</p>
                    <p className="text-[10px] text-cyber-green-muted mt-1 text-right">
                      {new Date(msg.timestamp * 1000).toLocaleTimeString()}
                    </p>
                  </div>
                </div>
              ))}
              <div ref={chatEndRef} />
            </div>
          )}
        </Card>

        {/* Input bar */}
        <div className="flex gap-2">
          <Button variant="ghost" size="md" onClick={() => toast("Media upload coming soon")}>
            <Paperclip size={16} />
          </Button>
          <input
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault()
                handleSend()
              }
            }}
            placeholder={selectedInstance ? "Type a message..." : "Select an instance first"}
            disabled={!selectedInstance || !recipient}
            className="flex-1 bg-bg-input border border-border text-cyber-green placeholder-cyber-green-muted/50 px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50 transition-all"
          />
          <Button onClick={handleSend} loading={sending} disabled={!selectedInstance || !recipient || !message.trim()}>
            <Send size={16} />
          </Button>
        </div>
      </div>
    </div>
  )
}
