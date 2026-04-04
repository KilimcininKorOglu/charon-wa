import { useEffect, useState, useRef, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Input } from "../../components/ui/Input"
import { Badge } from "../../components/ui/Badge"
import {
  Send,
  Paperclip,
  Smartphone,
  Phone,
  ChevronDown,
  Users as UsersIcon,
  User as UserIcon,
  Search,
  CheckCircle,
  Link2,
  X,
  Image,
} from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, Instance, Contact, Group, WsEvent } from "../../lib/types"
import toast from "react-hot-toast"

interface ChatMessage {
  id: string
  from: string
  message: string
  fromMe: boolean
  timestamp: number
  pushName?: string
}

type LeftTab = "contacts" | "groups" | "check" | "manual"
type SendMode = "instance" | "phone"

export function MessagesPage() {
  const [sendMode, setSendMode] = useState<SendMode>("instance")
  const [senderPhone, setSenderPhone] = useState("")
  const [instances, setInstances] = useState<Instance[]>([])
  const [selectedInstance, setSelectedInstance] = useState("")
  const [recipient, setRecipient] = useState("")
  const [recipientName, setRecipientName] = useState("")
  const [isGroup, setIsGroup] = useState(false)
  const [message, setMessage] = useState("")
  const [sending, setSending] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [wsConnected, setWsConnected] = useState(false)
  const chatEndRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  // Left panel
  const [leftTab, setLeftTab] = useState<LeftTab>("contacts")
  const [contacts, setContacts] = useState<Contact[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [contactSearch, setContactSearch] = useState("")
  const [contactsLoading, setContactsLoading] = useState(false)

  // Number check
  const [checkPhone, setCheckPhone] = useState("")
  const [checkResult, setCheckResult] = useState<{ isRegistered: boolean; jid: string } | null>(null)
  const [checking, setChecking] = useState(false)

  // Media
  const [showMediaForm, setShowMediaForm] = useState(false)
  const [mediaUrl, setMediaUrl] = useState("")
  const [mediaCaption, setMediaCaption] = useState("")
  const [sendingMedia, setSendingMedia] = useState(false)

  // Fetch connected instances
  useEffect(() => {
    const fetch = async () => {
      try {
        const res = await api.get<ApiResponse<{ instances: Instance[]; total: number }>>("/api/instances?all=true")
        if (res.data.success && res.data.data) {
          setInstances((res.data.data.instances || []).filter((i) => i.isConnected))
        }
      } catch { /* ignore */ }
    }
    fetch()
  }, [])

  // Fetch contacts when instance changes
  const fetchContacts = useCallback(async () => {
    if (!selectedInstance) return
    setContactsLoading(true)
    try {
      const res = await api.get<ApiResponse<{ contacts: Contact[]; total: number }>>(`/api/contacts/${selectedInstance}?limit=50&search=${contactSearch}`)
      if (res.data.success && res.data.data) {
        setContacts(res.data.data.contacts || [])
      }
    } catch { setContacts([]) } finally { setContactsLoading(false) }
  }, [selectedInstance, contactSearch])

  const fetchGroups = useCallback(async () => {
    if (!selectedInstance) return
    try {
      const res = await api.get<ApiResponse<{ groups: Group[]; total: number }>>(`/api/groups/${selectedInstance}`)
      if (res.data.success && res.data.data) {
        setGroups(res.data.data.groups || [])
      }
    } catch { setGroups([]) }
  }, [selectedInstance])

  const fetchGroupsByPhone = useCallback(async () => {
    if (!senderPhone) return
    try {
      const res = await api.get<ApiResponse<{ groups: Group[]; total: number }>>(`/api/groups/by-number/${senderPhone}`)
      if (res.data.success && res.data.data) {
        setGroups(res.data.data.groups || [])
      }
    } catch { setGroups([]) }
  }, [senderPhone])

  useEffect(() => {
    if (selectedInstance) {
      fetchContacts()
      fetchGroups()
    }
  }, [selectedInstance, fetchContacts, fetchGroups])

  useEffect(() => {
    if (sendMode === "phone" && senderPhone) {
      fetchGroupsByPhone()
    }
  }, [sendMode, senderPhone, fetchGroupsByPhone])

  // WebSocket for incoming messages (instance mode only)
  useEffect(() => {
    if (!selectedInstance || sendMode !== "instance") return
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
          setMessages((prev) => [...prev, {
            id: (data.message_id as string) || crypto.randomUUID(),
            from: data.from as string,
            message: data.message as string,
            fromMe: data.from_me as boolean,
            timestamp: data.timestamp as number,
            pushName: data.push_name as string,
          }])
        }
      } catch { /* ignore */ }
    }
    return () => { ws.close(); wsRef.current = null; setWsConnected(false) }
  }, [selectedInstance, sendMode])

  useEffect(() => { chatEndRef.current?.scrollIntoView({ behavior: "smooth" }) }, [messages])

  const handleModeSwitch = (mode: SendMode) => {
    setSendMode(mode)
    setRecipient("")
    setRecipientName("")
    setIsGroup(false)
    setMessages([])
    setGroups([])
    if (mode === "phone") {
      setSelectedInstance("")
      setLeftTab("groups")
    } else {
      setSenderPhone("")
      setLeftTab("contacts")
    }
  }

  const senderReady = sendMode === "instance" ? !!selectedInstance : !!senderPhone

  const selectContact = (phone: string, name: string, group: boolean) => {
    setRecipient(phone)
    setRecipientName(name)
    setIsGroup(group)
  }

  const handleSend = useCallback(async () => {
    if (!message.trim() || !senderReady || !recipient.trim()) return
    setSending(true)
    try {
      const endpoint = sendMode === "phone"
        ? (isGroup ? `/api/send-group/by-number/${senderPhone}` : `/api/by-number/${senderPhone}`)
        : (isGroup ? `/api/send-group/${selectedInstance}` : `/api/send/${selectedInstance}`)
      const body = isGroup ? { groupJid: recipient, message } : { to: recipient, message }
      const res = await api.post<ApiResponse>(endpoint, body)
      if (res.data.success) {
        setMessages((prev) => [...prev, {
          id: crypto.randomUUID(), from: "me", message, fromMe: true, timestamp: Math.floor(Date.now() / 1000),
        }])
        setMessage("")
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to send message") } finally { setSending(false) }
  }, [message, senderReady, recipient, isGroup, sendMode, senderPhone, selectedInstance])

  const handleSendMedia = async () => {
    if (!mediaUrl || !senderReady || !recipient) return
    setSendingMedia(true)
    try {
      const endpoint = sendMode === "phone"
        ? (isGroup ? `/api/send-group/by-number/${senderPhone}/media-url` : `/api/by-number/${senderPhone}/media-url`)
        : (isGroup ? `/api/send-group/${selectedInstance}/media-url` : `/api/send/${selectedInstance}/media-url`)
      const body = isGroup
        ? { groupJid: recipient, mediaUrl, caption: mediaCaption || undefined }
        : { to: recipient, mediaUrl, caption: mediaCaption || undefined }
      const res = await api.post<ApiResponse>(endpoint, body)
      if (res.data.success) {
        toast.success("Media sent")
        setMessages((prev) => [...prev, {
          id: crypto.randomUUID(), from: "me", message: `[Media] ${mediaCaption || mediaUrl}`, fromMe: true, timestamp: Math.floor(Date.now() / 1000),
        }])
        setShowMediaForm(false)
        setMediaUrl("")
        setMediaCaption("")
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to send media") } finally { setSendingMedia(false) }
  }

  const handleSendMediaFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file || !senderReady || !recipient) return
    setSendingMedia(true)
    const formData = new FormData()
    formData.append("file", file)
    formData.append(isGroup ? "groupJid" : "to", recipient)
    try {
      const endpoint = sendMode === "phone"
        ? (isGroup ? `/api/send-group/by-number/${senderPhone}/media` : `/api/by-number/${senderPhone}/media-file`)
        : (isGroup ? `/api/send-group/${selectedInstance}/media` : `/api/send/${selectedInstance}/media`)
      const res = await api.post<ApiResponse>(endpoint, formData, { headers: { "Content-Type": "multipart/form-data" } })
      if (res.data.success) {
        toast.success("Media sent")
        setMessages((prev) => [...prev, {
          id: crypto.randomUUID(), from: "me", message: `[File] ${file.name}`, fromMe: true, timestamp: Math.floor(Date.now() / 1000),
        }])
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to send media") } finally { setSendingMedia(false); e.target.value = "" }
  }

  const handleCheckNumber = async () => {
    if (!checkPhone || !selectedInstance) return
    setChecking(true)
    setCheckResult(null)
    try {
      const res = await api.post<ApiResponse<{ isRegistered: boolean; jid: string }>>(`/api/check/${selectedInstance}`, { phone: checkPhone })
      if (res.data.success && res.data.data) { setCheckResult(res.data.data) }
      else { toast.error(res.data.message) }
    } catch { toast.error("Check failed") } finally { setChecking(false) }
  }

  const tabClass = (t: LeftTab) =>
    `flex-1 py-1.5 text-[10px] text-center cursor-pointer transition-colors ${leftTab === t ? "text-cyber-green border-b border-cyber-green" : "text-cyber-green-muted border-b border-transparent hover:text-cyber-green"}`

  return (
    <div className="flex gap-4 h-[calc(100vh-7rem)]">
      {/* Left Panel */}
      <div className="w-72 shrink-0 flex flex-col gap-3">
        <h2 className="text-xl font-bold text-cyber-green">Messages</h2>

        {/* Send Mode Toggle */}
        <div className="flex border border-border">
          <button onClick={() => handleModeSwitch("instance")}
            className={`flex-1 py-1.5 text-[10px] text-center cursor-pointer transition-colors flex items-center justify-center gap-1 ${sendMode === "instance" ? "text-cyber-green bg-cyber-green/10 border-b-2 border-cyber-green" : "text-cyber-green-muted hover:text-cyber-green"}`}>
            <Smartphone size={10} /> By Instance
          </button>
          <button onClick={() => handleModeSwitch("phone")}
            className={`flex-1 py-1.5 text-[10px] text-center cursor-pointer transition-colors flex items-center justify-center gap-1 ${sendMode === "phone" ? "text-cyber-green bg-cyber-green/10 border-b-2 border-cyber-green" : "text-cyber-green-muted hover:text-cyber-green"}`}>
            <Phone size={10} /> By Phone
          </button>
        </div>

        {/* Instance Selector (instance mode) */}
        {sendMode === "instance" && (
          <Card className="p-3">
            <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5">
              <Smartphone size={10} className="inline mr-1" /> Instance
            </label>
            <div className="relative">
              <select value={selectedInstance} onChange={(e) => { setSelectedInstance(e.target.value); setMessages([]); setRecipient(""); setRecipientName("") }}
                className="w-full bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50 appearance-none cursor-pointer">
                <option value="">Select instance</option>
                {instances.map((inst) => <option key={inst.instanceId} value={inst.instanceId}>{inst.instanceId} {inst.phoneNumber ? `(${inst.phoneNumber})` : ""}</option>)}
              </select>
              <ChevronDown size={12} className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-green-muted pointer-events-none" />
            </div>
          </Card>
        )}

        {/* Phone Number Input (phone mode) */}
        {sendMode === "phone" && (
          <Card className="p-3">
            <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5">
              <Phone size={10} className="inline mr-1" /> Sender Phone Number
            </label>
            <input value={senderPhone} onChange={(e) => { setSenderPhone(e.target.value); setMessages([]); setRecipient(""); setRecipientName("") }}
              placeholder="905xxxxxxxxx"
              className="w-full bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
          </Card>
        )}

        {/* Current Recipient */}
        {recipient && (
          <Card className="p-3 border-cyber-green/20">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs text-cyber-green font-bold">{recipientName || recipient}</p>
                <p className="text-[10px] text-cyber-green-muted">{isGroup ? "Group" : "Contact"}</p>
              </div>
              <button onClick={() => { setRecipient(""); setRecipientName(""); setIsGroup(false) }} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={12} /></button>
            </div>
          </Card>
        )}

        {/* Tabs (instance mode only) */}
        {sendMode === "instance" && selectedInstance && (
          <>
            <div className="flex border-b border-border">
              <button onClick={() => setLeftTab("contacts")} className={tabClass("contacts")}><UserIcon size={10} className="inline mr-0.5" /> Contacts</button>
              <button onClick={() => setLeftTab("groups")} className={tabClass("groups")}><UsersIcon size={10} className="inline mr-0.5" /> Groups</button>
              <button onClick={() => setLeftTab("check")} className={tabClass("check")}><CheckCircle size={10} className="inline mr-0.5" /> Check</button>
            </div>

            {/* Contacts Tab */}
            {leftTab === "contacts" && (
              <div className="flex-1 overflow-y-auto">
                <div className="relative mb-2">
                  <Search size={12} className="absolute left-2 top-1/2 -translate-y-1/2 text-cyber-green-muted" />
                  <input value={contactSearch} onChange={(e) => setContactSearch(e.target.value)}
                    placeholder="Search contacts..." className="w-full bg-bg-input border border-border text-cyber-green pl-7 pr-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
                </div>
                {contactsLoading ? <p className="text-cyber-green-muted text-xs p-2">Loading...</p> :
                  contacts.length === 0 ? <p className="text-cyber-green-muted text-xs p-2">No contacts found</p> :
                    contacts.filter(c => !c.isGroup).map((c) => (
                      <button key={c.jid} onClick={() => selectContact(c.phoneNumber || c.jid, c.name || c.phoneNumber, false)}
                        className={`w-full text-left px-2 py-1.5 text-xs hover:bg-bg-hover transition-colors cursor-pointer flex items-center gap-2 ${recipient === (c.phoneNumber || c.jid) ? "bg-cyber-green/5 border-l-2 border-cyber-green" : "border-l-2 border-transparent"}`}>
                        <UserIcon size={12} className="text-cyber-green-muted shrink-0" />
                        <div className="min-w-0">
                          <p className="text-cyber-green truncate">{c.name || c.phoneNumber}</p>
                          <p className="text-[10px] text-cyber-green-muted">{c.phoneNumber}</p>
                        </div>
                      </button>
                    ))}
              </div>
            )}

            {/* Groups Tab */}
            {leftTab === "groups" && (
              <div className="flex-1 overflow-y-auto">
                {groups.length === 0 ? <p className="text-cyber-green-muted text-xs p-2">No groups found</p> :
                  groups.map((g) => (
                    <button key={g.jid} onClick={() => selectContact(g.jid, g.name, true)}
                      className={`w-full text-left px-2 py-1.5 text-xs hover:bg-bg-hover transition-colors cursor-pointer flex items-center gap-2 ${recipient === g.jid ? "bg-cyber-green/5 border-l-2 border-cyber-green" : "border-l-2 border-transparent"}`}>
                      <UsersIcon size={12} className="text-cyber-green-muted shrink-0" />
                      <div className="min-w-0">
                        <p className="text-cyber-green truncate">{g.name}</p>
                        <p className="text-[10px] text-cyber-green-muted">{g.participants} members</p>
                      </div>
                    </button>
                  ))}
              </div>
            )}

            {/* Check Tab */}
            {leftTab === "check" && (
              <div className="space-y-2 p-1">
                <div className="flex gap-1.5">
                  <input value={checkPhone} onChange={(e) => setCheckPhone(e.target.value)} placeholder="905xxxxxxxxxx"
                    className="flex-1 bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
                  <Button size="sm" onClick={handleCheckNumber} loading={checking} disabled={!checkPhone}><CheckCircle size={12} /></Button>
                </div>
                {checkResult && (
                  <Card className={`p-2 ${checkResult.isRegistered ? "border-cyber-green/30" : "border-cyber-danger/30"}`}>
                    <Badge variant={checkResult.isRegistered ? "success" : "danger"}>{checkResult.isRegistered ? "Registered" : "Not Found"}</Badge>
                    {checkResult.isRegistered && (
                      <div className="mt-1.5">
                        <p className="text-[10px] text-cyber-green-muted">{checkResult.jid}</p>
                        <Button size="sm" variant="ghost" className="mt-1" onClick={() => selectContact(checkPhone, checkPhone, false)}>
                          <Send size={10} className="mr-1" /> Message
                        </Button>
                      </div>
                    )}
                  </Card>
                )}
              </div>
            )}
          </>
        )}

        {/* Phone mode: groups + manual tabs */}
        {sendMode === "phone" && senderPhone && (
          <>
            <div className="flex border-b border-border">
              <button onClick={() => setLeftTab("groups")} className={tabClass("groups")}><UsersIcon size={10} className="inline mr-0.5" /> Groups</button>
              <button onClick={() => setLeftTab("manual")} className={tabClass("manual")}><UserIcon size={10} className="inline mr-0.5" /> Manual</button>
            </div>

            {leftTab === "groups" && (
              <div className="flex-1 overflow-y-auto">
                {groups.length === 0 ? <p className="text-cyber-green-muted text-xs p-2">No groups found</p> :
                  groups.map((g) => (
                    <button key={g.jid} onClick={() => selectContact(g.jid, g.name, true)}
                      className={`w-full text-left px-2 py-1.5 text-xs hover:bg-bg-hover transition-colors cursor-pointer flex items-center gap-2 ${recipient === g.jid ? "bg-cyber-green/5 border-l-2 border-cyber-green" : "border-l-2 border-transparent"}`}>
                      <UsersIcon size={12} className="text-cyber-green-muted shrink-0" />
                      <div className="min-w-0">
                        <p className="text-cyber-green truncate">{g.name}</p>
                        <p className="text-[10px] text-cyber-green-muted">{g.participants} members</p>
                      </div>
                    </button>
                  ))}
              </div>
            )}

            {leftTab === "manual" && (
              <Card className="p-3 space-y-2">
                <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block">Recipient</label>
                <input value={recipient} onChange={(e) => { setRecipient(e.target.value); setRecipientName("") }}
                  placeholder="905xxxxxxxxx or group JID"
                  className="w-full bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
                <label className="flex items-center gap-2 text-[10px] text-cyber-green cursor-pointer">
                  <input type="checkbox" checked={isGroup} onChange={(e) => setIsGroup(e.target.checked)} className="accent-cyber-green" />
                  Group Message
                </label>
              </Card>
            )}
          </>
        )}

        {/* WS Status (instance mode only) */}
        <div className="mt-auto">
          {sendMode === "instance" && (
            <div className="flex items-center gap-2 px-1">
              <span className={`w-1.5 h-1.5 rounded-full ${wsConnected ? "bg-cyber-green animate-pulse" : "bg-cyber-green-muted"}`} />
              <span className="text-[10px] text-cyber-green-muted">{wsConnected ? "Live" : selectedInstance ? "Connecting..." : "Idle"}</span>
              {messages.length > 0 && <Badge variant="info" className="ml-auto">{messages.length}</Badge>}
            </div>
          )}
          {sendMode === "phone" && senderPhone && (
            <p className="text-[10px] text-cyber-green-muted px-1">Sending via phone: {senderPhone}</p>
          )}
        </div>
      </div>

      {/* Right: Chat */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Media form */}
        {showMediaForm && (
          <Card className="mb-2 border-cyber-green/20">
            <div className="flex items-center justify-between mb-2">
              <h4 className="text-xs text-cyber-green-dim uppercase font-bold flex items-center gap-1"><Image size={12} /> Send Media</h4>
              <button onClick={() => setShowMediaForm(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>
            <div className="space-y-2">
              <div className="flex gap-2">
                <Input label="Media URL" value={mediaUrl} onChange={(e) => setMediaUrl(e.target.value)} placeholder="https://..." className="flex-1" />
              </div>
              <Input label="Caption (optional)" value={mediaCaption} onChange={(e) => setMediaCaption(e.target.value)} />
              <div className="flex gap-2">
                <Button size="sm" onClick={handleSendMedia} loading={sendingMedia} disabled={!mediaUrl}><Link2 size={12} className="mr-1" /> Send URL</Button>
                <label className="cursor-pointer inline-flex items-center px-3 py-1.5 text-xs font-mono border bg-transparent text-cyber-green border-border hover:border-cyber-green/30 hover:bg-cyber-green/5 transition-all">
                  <Paperclip size={12} className="mr-1" /> Upload File
                  <input type="file" onChange={handleSendMediaFile} className="hidden" />
                </label>
              </div>
            </div>
          </Card>
        )}

        {/* Chat messages */}
        <Card className="flex-1 overflow-y-auto mb-2 min-h-0">
          {!senderReady ? (
            <div className="flex items-center justify-center h-full"><p className="text-cyber-green-muted text-sm">{sendMode === "phone" ? "Enter a sender phone number" : "Select an instance to start messaging"}</p></div>
          ) : !recipient ? (
            <div className="flex items-center justify-center h-full"><p className="text-cyber-green-muted text-sm">{sendMode === "phone" ? "Enter a recipient number" : "Select a contact or group from the left panel"}</p></div>
          ) : messages.length === 0 ? (
            <div className="flex items-center justify-center h-full"><p className="text-cyber-green-muted text-sm">No messages yet</p></div>
          ) : (
            <div className="space-y-3 p-2">
              {messages.map((msg) => (
                <div key={msg.id} className={`flex ${msg.fromMe ? "justify-end" : "justify-start"}`}>
                  <div className={`max-w-[70%] px-3 py-2 text-sm ${msg.fromMe ? "bg-cyber-green/10 border border-cyber-green/20 text-cyber-green" : "bg-bg-hover border border-border text-cyber-white"}`}>
                    {!msg.fromMe && msg.pushName && <p className="text-xs text-cyber-green-dim font-bold mb-1">{msg.pushName}</p>}
                    <p className="break-words whitespace-pre-wrap">{msg.message}</p>
                    <p className="text-[10px] text-cyber-green-muted mt-1 text-right">{new Date(msg.timestamp * 1000).toLocaleTimeString()}</p>
                  </div>
                </div>
              ))}
              <div ref={chatEndRef} />
            </div>
          )}
        </Card>

        {/* Input bar */}
        <div className="flex gap-2">
          <Button variant="ghost" size="md" onClick={() => setShowMediaForm(!showMediaForm)} disabled={!senderReady || !recipient}>
            <Paperclip size={16} />
          </Button>
          <input value={message} onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); handleSend() } }}
            placeholder={!senderReady ? (sendMode === "phone" ? "Enter sender phone number" : "Select an instance") : !recipient ? "Select a contact" : "Type a message..."}
            disabled={!senderReady || !recipient}
            className="flex-1 bg-bg-input border border-border text-cyber-green placeholder-cyber-green-muted/50 px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50 transition-all" />
          <Button onClick={handleSend} loading={sending} disabled={!senderReady || !recipient || !message.trim()}>
            <Send size={16} />
          </Button>
        </div>
      </div>
    </div>
  )
}
