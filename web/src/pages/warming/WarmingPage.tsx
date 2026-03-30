import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import { Input } from "../../components/ui/Input"
import {
  Flame, Plus, Trash2, Play, Pause, Square, RotateCcw, ScrollText,
  FileText, RefreshCw, X, ChevronDown, ChevronRight, ArrowUp, ArrowDown,
  Sparkles, LayoutTemplate, Edit3, Check, Save,
} from "lucide-react"
import api from "../../lib/api"
import type {
  ApiResponse, WarmingRoom, WarmingScript, WarmingLog,
  WarmingScriptLine, WarmingTemplate, Instance, CreateWarmingRoomRequest,
} from "../../lib/types"
import toast from "react-hot-toast"

type Tab = "rooms" | "scripts" | "templates" | "logs"

const statusVariant = (s: string) => {
  if (s === "ACTIVE") return "success" as const
  if (s === "PAUSE") return "warning" as const
  if (s === "FINISHED") return "info" as const
  return "muted" as const
}

// ─── WIZARD STATE ────────────────────────────────────────
const defaultRoom: CreateWarmingRoomRequest = {
  name: "", senderInstanceId: "", receiverInstanceId: "", scriptId: 0,
  intervalMinSeconds: 60, intervalMaxSeconds: 120, sendRealMessage: false,
  roomType: "BOT_VS_BOT", whitelistedNumber: "", replyDelayMin: 10,
  replyDelayMax: 60, aiEnabled: false, aiProvider: "gemini",
  aiModel: "gemini-flash-latest", aiSystemPrompt: "", aiTemperature: 0.7,
  aiMaxTokens: 150, fallbackToScript: true,
}

export function WarmingPage() {
  const [tab, setTab] = useState<Tab>("rooms")

  // Rooms
  const [rooms, setRooms] = useState<WarmingRoom[]>([])
  const [roomsLoading, setRoomsLoading] = useState(true)
  const [showWizard, setShowWizard] = useState(false)
  const [wizardStep, setWizardStep] = useState(1)
  const [roomForm, setRoomForm] = useState<CreateWarmingRoomRequest>({ ...defaultRoom })
  const [creatingRoom, setCreatingRoom] = useState(false)
  const [instances, setInstances] = useState<Instance[]>([])
  const [selectedRoom, setSelectedRoom] = useState<WarmingRoom | null>(null)
  const [editRoom, setEditRoom] = useState<CreateWarmingRoomRequest>({ ...defaultRoom })
  const [savingRoom, setSavingRoom] = useState(false)

  // Scripts
  const [scripts, setScripts] = useState<WarmingScript[]>([])
  const [scriptsLoading, setScriptsLoading] = useState(false)
  const [showCreateScript, setShowCreateScript] = useState(false)
  const [newScript, setNewScript] = useState({ title: "", description: "", category: "casual" })
  const [expandedScript, setExpandedScript] = useState<number | null>(null)
  const [scriptLines, setScriptLines] = useState<WarmingScriptLine[]>([])
  const [linesLoading, setLinesLoading] = useState(false)
  const [newLine, setNewLine] = useState({ actorRole: "ACTOR_A" as "ACTOR_A" | "ACTOR_B", messageContent: "", typingDurationSec: 2 })
  const [editingLine, setEditingLine] = useState<number | null>(null)
  const [editContent, setEditContent] = useState("")
  const [genCount, setGenCount] = useState(5)
  const [selectedScript, setSelectedScript] = useState<WarmingScript | null>(null)
  const [editScript, setEditScript] = useState({ title: "", description: "", category: "" })
  const [savingScript, setSavingScript] = useState(false)

  // Templates
  const [templates, setTemplates] = useState<WarmingTemplate[]>([])
  const [templatesLoading, setTemplatesLoading] = useState(false)
  const [showCreateTemplate, setShowCreateTemplate] = useState(false)
  const [newTemplate, setNewTemplate] = useState({ name: "", category: "casual", structure: "{}" })

  // Logs
  const [logs, setLogs] = useState<WarmingLog[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [logRoomId, setLogRoomId] = useState("")
  const [logStatus, setLogStatus] = useState("")

  // ─── FETCHERS ──────────────────────────────────────────
  const fetchRooms = useCallback(async () => {
    setRoomsLoading(true)
    try {
      const res = await api.get<ApiResponse<{ rooms: WarmingRoom[]; total: number }>>("/api/warming/rooms")
      if (res.data.success && res.data.data) setRooms(res.data.data.rooms || [])
    } catch { /* */ } finally { setRoomsLoading(false) }
  }, [])

  const fetchScripts = useCallback(async () => {
    setScriptsLoading(true)
    try {
      const res = await api.get<ApiResponse<{ scripts: WarmingScript[]; total: number }>>("/api/warming/scripts?q=&category=")
      if (res.data.success && res.data.data) setScripts(res.data.data.scripts || [])
    } catch { /* */ } finally { setScriptsLoading(false) }
  }, [])

  const fetchTemplates = useCallback(async () => {
    setTemplatesLoading(true)
    try {
      const res = await api.get<ApiResponse<{ templates: WarmingTemplate[]; total: number }>>("/api/warming/templates")
      if (res.data.success && res.data.data) setTemplates(res.data.data.templates || [])
    } catch { /* */ } finally { setTemplatesLoading(false) }
  }, [])

  const fetchLogs = useCallback(async () => {
    if (!logRoomId) return
    setLogsLoading(true)
    try {
      const params = `roomId=${logRoomId}&status=${logStatus}&limit=50`
      const res = await api.get<ApiResponse<{ logs: WarmingLog[]; total: number } | WarmingLog[]>>(`/api/warming/logs?${params}`)
      if (res.data.success && res.data.data) {
        const d = res.data.data
        setLogs(Array.isArray(d) ? d : d.logs || [])
      }
    } catch { /* */ } finally { setLogsLoading(false) }
  }, [logRoomId, logStatus])

  const fetchInstances = useCallback(async () => {
    try {
      const res = await api.get<ApiResponse<{ instances: Instance[]; total: number }>>("/api/instances?all=true")
      if (res.data.success && res.data.data) setInstances(res.data.data.instances || [])
    } catch { /* */ }
  }, [])

  const fetchScriptLines = useCallback(async (scriptId: number) => {
    setLinesLoading(true)
    try {
      const res = await api.get<ApiResponse<{ lines: WarmingScriptLine[]; total: number }>>(`/api/warming/scripts/${scriptId}/lines`)
      if (res.data.success && res.data.data) setScriptLines(res.data.data.lines || [])
    } catch { setScriptLines([]) } finally { setLinesLoading(false) }
  }, [])

  useEffect(() => {
    if (tab === "rooms") { fetchRooms(); fetchInstances(); fetchScripts() }
    if (tab === "scripts") { fetchScripts(); fetchInstances() }
    if (tab === "templates") fetchTemplates()
    if (tab === "logs") { fetchLogs(); fetchRooms() }
  }, [tab, fetchRooms, fetchScripts, fetchTemplates, fetchLogs, fetchInstances])

  // ─── ROOM ACTIONS ──────────────────────────────────────
  const createRoom = async () => {
    setCreatingRoom(true)
    try {
      const res = await api.post<ApiResponse>("/api/warming/rooms", roomForm)
      if (res.data.success) {
        toast.success("Room created")
        setShowWizard(false)
        setRoomForm({ ...defaultRoom })
        setWizardStep(1)
        fetchRooms()
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to create room") } finally { setCreatingRoom(false) }
  }

  const updateRoomStatus = async (id: string, status: string) => {
    try { await api.patch(`/api/warming/rooms/${id}/status`, { status }); toast.success(`Room ${status.toLowerCase()}`); fetchRooms() }
    catch { toast.error("Failed") }
  }
  const restartRoom = async (id: string) => {
    try { await api.post(`/api/warming/rooms/${id}/restart`); toast.success("Restarted"); fetchRooms() }
    catch { toast.error("Failed") }
  }
  const deleteRoom = async (id: string) => {
    if (!confirm("Delete room?")) return
    try { await api.delete(`/api/warming/rooms/${id}`); toast.success("Deleted"); if (selectedRoom?.id === id) setSelectedRoom(null); fetchRooms() }
    catch { toast.error("Failed") }
  }

  const selectRoom = (room: WarmingRoom) => {
    setSelectedRoom(room)
    setEditRoom({
      name: room.name, senderInstanceId: room.senderInstanceId,
      receiverInstanceId: room.receiverInstanceId || "",
      scriptId: room.scriptId, intervalMinSeconds: room.intervalMinSeconds,
      intervalMaxSeconds: room.intervalMaxSeconds,
      sendRealMessage: room.sendRealMessage || false,
      roomType: room.roomType,
      whitelistedNumber: room.whitelistedNumber || "",
      replyDelayMin: room.replyDelayMin || 10,
      replyDelayMax: room.replyDelayMax || 60,
      aiEnabled: room.aiEnabled || false,
      aiProvider: room.aiProvider || "gemini",
      aiModel: room.aiModel || "gemini-flash-latest",
      aiSystemPrompt: room.aiSystemPrompt || "",
      aiTemperature: room.aiTemperature ?? 0.7,
      aiMaxTokens: room.aiMaxTokens ?? 150,
      fallbackToScript: room.fallbackToScript ?? true,
    })
  }
  const saveRoom = async () => {
    if (!selectedRoom) return
    setSavingRoom(true)
    try {
      await api.put(`/api/warming/rooms/${selectedRoom.id}`, editRoom)
      toast.success("Room updated"); fetchRooms()
    } catch { toast.error("Failed to update") } finally { setSavingRoom(false) }
  }

  // ─── SCRIPT ACTIONS ────────────────────────────────────
  const createScript = async () => {
    try { await api.post("/api/warming/scripts", newScript); toast.success("Created"); setShowCreateScript(false); setNewScript({ title: "", description: "", category: "casual" }); fetchScripts() }
    catch { toast.error("Failed") }
  }
  const deleteScript = async (id: number) => {
    if (!confirm("Delete script?")) return
    try { await api.delete(`/api/warming/scripts/${id}`); toast.success("Deleted"); if (expandedScript === id) setExpandedScript(null); if (selectedScript?.id === id) setSelectedScript(null); fetchScripts() }
    catch { toast.error("Failed") }
  }
  const toggleScript = (id: number) => {
    if (expandedScript === id) { setExpandedScript(null) } else { setExpandedScript(id); fetchScriptLines(id) }
  }

  // ─── LINE ACTIONS ──────────────────────────────────────
  const addLine = async () => {
    if (!expandedScript || !newLine.messageContent) return
    try {
      await api.post(`/api/warming/scripts/${expandedScript}/lines`, { ...newLine, sequenceOrder: scriptLines.length + 1 })
      toast.success("Line added"); setNewLine({ actorRole: "ACTOR_A", messageContent: "", typingDurationSec: 2 }); fetchScriptLines(expandedScript)
    } catch { toast.error("Failed") }
  }
  const deleteLine = async (lineId: number) => {
    if (!expandedScript) return
    try { await api.delete(`/api/warming/scripts/${expandedScript}/lines/${lineId}`); toast.success("Deleted"); fetchScriptLines(expandedScript) }
    catch { toast.error("Failed") }
  }
  const saveLine = async (lineId: number) => {
    if (!expandedScript) return
    try { await api.put(`/api/warming/scripts/${expandedScript}/lines/${lineId}`, { messageContent: editContent }); toast.success("Updated"); setEditingLine(null); fetchScriptLines(expandedScript) }
    catch { toast.error("Failed") }
  }
  const generateLines = async () => {
    if (!expandedScript) return
    const script = scripts.find(s => s.id === expandedScript)
    try {
      const res = await api.post<ApiResponse>(`/api/warming/scripts/${expandedScript}/lines/generate`, { lineCount: genCount, category: script?.category || "casual" })
      if (res.data.success) { toast.success("Lines generated"); fetchScriptLines(expandedScript) }
      else toast.error(res.data.message)
    } catch { toast.error("Generation failed") }
  }
  const reorderLine = async (lineId: number, direction: "up" | "down") => {
    if (!expandedScript) return
    const idx = scriptLines.findIndex(l => l.id === lineId)
    if ((direction === "up" && idx <= 0) || (direction === "down" && idx >= scriptLines.length - 1)) return
    const swapIdx = direction === "up" ? idx - 1 : idx + 1
    const reordered = scriptLines.map((l, i) => {
      if (i === idx) return { id: l.id, sequenceOrder: swapIdx + 1 }
      if (i === swapIdx) return { id: l.id, sequenceOrder: idx + 1 }
      return { id: l.id, sequenceOrder: i + 1 }
    })
    try { await api.put(`/api/warming/scripts/${expandedScript}/lines/reorder`, { lines: reordered }); fetchScriptLines(expandedScript) }
    catch { toast.error("Reorder failed") }
  }

  const selectScript = (script: WarmingScript) => {
    setSelectedScript(script)
    setEditScript({ title: script.title, description: script.description, category: script.category })
  }
  const saveScript = async () => {
    if (!selectedScript) return
    setSavingScript(true)
    try {
      await api.put(`/api/warming/scripts/${selectedScript.id}`, editScript)
      toast.success("Script updated"); fetchScripts()
    } catch { toast.error("Failed to update") } finally { setSavingScript(false) }
  }

  // ─── TEMPLATE ACTIONS ──────────────────────────────────
  const createTemplate = async () => {
    try {
      let structure = {}
      try { structure = JSON.parse(newTemplate.structure) } catch { toast.error("Invalid JSON"); return }
      await api.post("/api/warming/templates", { ...newTemplate, structure })
      toast.success("Created"); setShowCreateTemplate(false); setNewTemplate({ name: "", category: "casual", structure: "{}" }); fetchTemplates()
    } catch { toast.error("Failed") }
  }
  const deleteTemplate = async (id: number) => {
    if (!confirm("Delete template?")) return
    try { await api.delete(`/api/warming/templates/${id}`); toast.success("Deleted"); fetchTemplates() }
    catch { toast.error("Failed") }
  }

  const tabClass = (t: Tab) => `px-4 py-2 text-sm font-mono cursor-pointer border-b-2 transition-colors ${tab === t ? "border-cyber-green text-cyber-green" : "border-transparent text-cyber-green-muted hover:text-cyber-green"}`
  const selCls = "w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50 appearance-none cursor-pointer"

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-cyber-green flex items-center gap-2"><Flame size={20} /> Warming System</h2>
      </div>

      <div className="flex gap-1 border-b border-border mb-6">
        <button onClick={() => setTab("rooms")} className={tabClass("rooms")}><Flame size={14} className="inline mr-1.5" />Rooms</button>
        <button onClick={() => setTab("scripts")} className={tabClass("scripts")}><ScrollText size={14} className="inline mr-1.5" />Scripts</button>
        <button onClick={() => setTab("templates")} className={tabClass("templates")}><LayoutTemplate size={14} className="inline mr-1.5" />Templates</button>
        <button onClick={() => setTab("logs")} className={tabClass("logs")}><FileText size={14} className="inline mr-1.5" />Logs</button>
      </div>

      {/* ═══ ROOMS TAB ═══ */}
      {tab === "rooms" && (
        <div className="flex gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex justify-end gap-2 mb-4">
            <Button variant="ghost" size="sm" onClick={fetchRooms}><RefreshCw size={14} className="mr-1.5" /> Refresh</Button>
            <Button size="sm" onClick={() => { setShowWizard(true); setWizardStep(1); setRoomForm({ ...defaultRoom }) }}><Plus size={14} className="mr-1.5" /> New Room</Button>
          </div>

          {/* ── WIZARD ── */}
          {showWizard && (
            <Card className="mb-6 border-cyber-green/20">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Room — Step {wizardStep}/3</h3>
                <button onClick={() => setShowWizard(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={16} /></button>
              </div>

              {/* Step 1: Type */}
              {wizardStep === 1 && (
                <div className="space-y-3">
                  <Input label="Room Name" value={roomForm.name} onChange={(e) => setRoomForm({ ...roomForm, name: e.target.value })} />
                  <div>
                    <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Room Type</label>
                    <div className="flex gap-3">
                      {(["BOT_VS_BOT", "HUMAN_VS_BOT"] as const).map(t => (
                        <button key={t} onClick={() => setRoomForm({ ...roomForm, roomType: t })}
                          className={`flex-1 py-2 text-xs border font-mono cursor-pointer transition-all ${roomForm.roomType === t ? "border-cyber-green bg-cyber-green/10 text-cyber-green" : "border-border text-cyber-green-muted hover:border-cyber-green/30"}`}>{t}</button>
                      ))}
                    </div>
                  </div>
                  <div>
                    <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Sender Instance</label>
                    <select value={roomForm.senderInstanceId} onChange={(e) => setRoomForm({ ...roomForm, senderInstanceId: e.target.value })} className={selCls}>
                      <option value="">Select</option>
                      {instances.map(i => <option key={i.instanceId} value={i.instanceId}>{i.instanceId} {i.phoneNumber ? `(${i.phoneNumber})` : ""}</option>)}
                    </select>
                  </div>
                  <Button onClick={() => setWizardStep(2)} disabled={!roomForm.name || !roomForm.senderInstanceId}>Next</Button>
                </div>
              )}

              {/* Step 2: Config */}
              {wizardStep === 2 && (
                <div className="space-y-3">
                  {roomForm.roomType === "BOT_VS_BOT" && (
                    <div>
                      <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Receiver Instance</label>
                      <select value={roomForm.receiverInstanceId} onChange={(e) => setRoomForm({ ...roomForm, receiverInstanceId: e.target.value })} className={selCls}>
                        <option value="">Select</option>
                        {instances.filter(i => i.instanceId !== roomForm.senderInstanceId).map(i => <option key={i.instanceId} value={i.instanceId}>{i.instanceId}</option>)}
                      </select>
                    </div>
                  )}
                  {roomForm.roomType === "HUMAN_VS_BOT" && (
                    <>
                      <Input label="Whitelisted Number" value={roomForm.whitelistedNumber || ""} onChange={(e) => setRoomForm({ ...roomForm, whitelistedNumber: e.target.value })} placeholder="628xxxxxxxxxx" />
                      <div className="grid grid-cols-2 gap-3">
                        <Input label="Reply Delay Min (s)" type="number" value={roomForm.replyDelayMin || 10} onChange={(e) => setRoomForm({ ...roomForm, replyDelayMin: parseInt(e.target.value) || 0 })} />
                        <Input label="Reply Delay Max (s)" type="number" value={roomForm.replyDelayMax || 60} onChange={(e) => setRoomForm({ ...roomForm, replyDelayMax: parseInt(e.target.value) || 0 })} />
                      </div>
                      <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                        <input type="checkbox" checked={roomForm.aiEnabled || false} onChange={(e) => setRoomForm({ ...roomForm, aiEnabled: e.target.checked })} className="accent-cyber-green" />
                        Enable AI Replies
                      </label>
                      {roomForm.aiEnabled && (
                        <div className="space-y-3 pl-4 border-l-2 border-cyber-green/20">
                          <div className="grid grid-cols-2 gap-3">
                            <Input label="AI Provider" value={roomForm.aiProvider || "gemini"} onChange={(e) => setRoomForm({ ...roomForm, aiProvider: e.target.value })} />
                            <Input label="AI Model" value={roomForm.aiModel || ""} onChange={(e) => setRoomForm({ ...roomForm, aiModel: e.target.value })} />
                          </div>
                          <div>
                            <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">System Prompt</label>
                            <textarea value={roomForm.aiSystemPrompt || ""} onChange={(e) => setRoomForm({ ...roomForm, aiSystemPrompt: e.target.value })}
                              className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50 h-20 resize-y" placeholder="You are a friendly assistant..." />
                          </div>
                          <div className="grid grid-cols-2 gap-3">
                            <Input label="Temperature" type="number" value={roomForm.aiTemperature || 0.7} onChange={(e) => setRoomForm({ ...roomForm, aiTemperature: parseFloat(e.target.value) || 0.7 })} />
                            <Input label="Max Tokens" type="number" value={roomForm.aiMaxTokens || 150} onChange={(e) => setRoomForm({ ...roomForm, aiMaxTokens: parseInt(e.target.value) || 150 })} />
                          </div>
                          <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                            <input type="checkbox" checked={roomForm.fallbackToScript || false} onChange={(e) => setRoomForm({ ...roomForm, fallbackToScript: e.target.checked })} className="accent-cyber-green" />
                            Fallback to Script on AI Error
                          </label>
                        </div>
                      )}
                    </>
                  )}
                  <div>
                    <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Script</label>
                    <select value={roomForm.scriptId} onChange={(e) => setRoomForm({ ...roomForm, scriptId: parseInt(e.target.value) || 0 })} className={selCls}>
                      <option value={0}>Select script</option>
                      {scripts.map(s => <option key={s.id} value={s.id}>{s.title} ({s.category})</option>)}
                    </select>
                  </div>
                  <div className="flex gap-2">
                    <Button variant="ghost" onClick={() => setWizardStep(1)}>Back</Button>
                    <Button onClick={() => setWizardStep(3)} disabled={roomForm.roomType === "BOT_VS_BOT" && !roomForm.receiverInstanceId}>Next</Button>
                  </div>
                </div>
              )}

              {/* Step 3: Timing + Confirm */}
              {wizardStep === 3 && (
                <div className="space-y-3">
                  <div className="grid grid-cols-2 gap-3">
                    <Input label="Interval Min (s)" type="number" value={roomForm.intervalMinSeconds} onChange={(e) => setRoomForm({ ...roomForm, intervalMinSeconds: parseInt(e.target.value) || 0 })} />
                    <Input label="Interval Max (s)" type="number" value={roomForm.intervalMaxSeconds} onChange={(e) => setRoomForm({ ...roomForm, intervalMaxSeconds: parseInt(e.target.value) || 0 })} />
                  </div>
                  <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                    <input type="checkbox" checked={roomForm.sendRealMessage} onChange={(e) => setRoomForm({ ...roomForm, sendRealMessage: e.target.checked })} className="accent-cyber-green" />
                    Send Real Messages (uncheck for simulation/dry-run)
                  </label>
                  {/* Summary */}
                  <Card className="bg-bg-primary text-xs space-y-1">
                    <p className="text-cyber-green-dim font-bold uppercase mb-2">Summary</p>
                    <p><span className="text-cyber-green-muted">Name:</span> <span className="text-cyber-green">{roomForm.name}</span></p>
                    <p><span className="text-cyber-green-muted">Type:</span> <Badge variant="info">{roomForm.roomType}</Badge></p>
                    <p><span className="text-cyber-green-muted">Sender:</span> <span className="text-cyber-green font-mono">{roomForm.senderInstanceId}</span></p>
                    {roomForm.roomType === "BOT_VS_BOT" && <p><span className="text-cyber-green-muted">Receiver:</span> <span className="text-cyber-green font-mono">{roomForm.receiverInstanceId}</span></p>}
                    {roomForm.roomType === "HUMAN_VS_BOT" && <p><span className="text-cyber-green-muted">Whitelisted:</span> <span className="text-cyber-green">{roomForm.whitelistedNumber}</span></p>}
                    <p><span className="text-cyber-green-muted">Interval:</span> <span className="text-cyber-green">{roomForm.intervalMinSeconds}-{roomForm.intervalMaxSeconds}s</span></p>
                    {roomForm.aiEnabled && <p><span className="text-cyber-green-muted">AI:</span> <Badge variant="success">Enabled</Badge> {roomForm.aiProvider}/{roomForm.aiModel}</p>}
                  </Card>
                  <div className="flex gap-2">
                    <Button variant="ghost" onClick={() => setWizardStep(2)}>Back</Button>
                    <Button onClick={createRoom} loading={creatingRoom}>Create Room</Button>
                  </div>
                </div>
              )}
            </Card>
          )}

          {/* Room List */}
          {roomsLoading ? <Card className="animate-pulse"><div className="h-32 bg-bg-hover rounded" /></Card> :
            rooms.length === 0 ? <Card><p className="text-cyber-green-muted text-sm text-center py-8">No warming rooms yet.</p></Card> :
              <div className="space-y-2">{rooms.map(room => (
                <div key={room.id} onClick={() => selectRoom(room)} className="cursor-pointer">
                <Card className={`flex items-center justify-between transition-colors ${selectedRoom?.id === room.id ? "border-cyber-green/30 bg-cyber-green/5" : "hover:bg-bg-hover"}`}>
                  <div>
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-bold text-cyber-green">{room.name}</p>
                      <Badge variant={statusVariant(room.status)}>{room.status}</Badge>
                      <Badge variant="muted">{room.roomType}</Badge>
                    </div>
                    <p className="text-xs text-cyber-green-muted mt-1">
                      Sender: {room.senderInstanceId}{room.receiverInstanceId && ` → ${room.receiverInstanceId}`} | {room.intervalMinSeconds}-{room.intervalMaxSeconds}s | Seq: {room.currentSequence}
                    </p>
                  </div>
                  <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
                    {room.status !== "ACTIVE" && <Button variant="outline" size="sm" onClick={() => updateRoomStatus(room.id, "ACTIVE")}><Play size={13} /></Button>}
                    {room.status === "ACTIVE" && <Button variant="outline" size="sm" onClick={() => updateRoomStatus(room.id, "PAUSE")}><Pause size={13} /></Button>}
                    <Button variant="ghost" size="sm" onClick={() => updateRoomStatus(room.id, "STOPPED")}><Square size={13} /></Button>
                    <Button variant="ghost" size="sm" onClick={() => restartRoom(room.id)}><RotateCcw size={13} /></Button>
                    <Button variant="ghost" size="sm" onClick={() => { setLogRoomId(room.id); setTab("logs") }}><FileText size={13} /></Button>
                    <Button variant="danger" size="sm" onClick={() => deleteRoom(room.id)}><Trash2 size={13} /></Button>
                  </div>
                </Card>
                </div>
              ))}</div>}
        </div>

        {/* Room Edit Panel */}
        {selectedRoom && (
          <div className="w-80 shrink-0">
            <Card>
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Edit Room</h3>
                <button onClick={() => setSelectedRoom(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
              </div>
              <div className="space-y-3 max-h-[70vh] overflow-y-auto pr-1">
                <Input label="Name" value={editRoom.name} onChange={(e) => setEditRoom({ ...editRoom, name: e.target.value })} />
                <div>
                  <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Script</label>
                  <select value={editRoom.scriptId} onChange={(e) => setEditRoom({ ...editRoom, scriptId: parseInt(e.target.value) || 0 })} className={selCls}>
                    <option value={0}>Select script</option>
                    {scripts.map(s => <option key={s.id} value={s.id}>{s.title} ({s.category})</option>)}
                  </select>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <Input label="Min (s)" type="number" value={editRoom.intervalMinSeconds} onChange={(e) => setEditRoom({ ...editRoom, intervalMinSeconds: parseInt(e.target.value) || 0 })} />
                  <Input label="Max (s)" type="number" value={editRoom.intervalMaxSeconds} onChange={(e) => setEditRoom({ ...editRoom, intervalMaxSeconds: parseInt(e.target.value) || 0 })} />
                </div>
                <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                  <input type="checkbox" checked={editRoom.sendRealMessage} onChange={(e) => setEditRoom({ ...editRoom, sendRealMessage: e.target.checked })} className="accent-cyber-green" />
                  Send Real Messages
                </label>

                {/* AI Config */}
                <div className="border-t border-border pt-3 mt-3">
                  <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                    <input type="checkbox" checked={editRoom.aiEnabled || false} onChange={(e) => setEditRoom({ ...editRoom, aiEnabled: e.target.checked })} className="accent-cyber-green" />
                    Enable AI Replies
                  </label>
                  {editRoom.aiEnabled && (
                    <div className="space-y-3 mt-3 pl-3 border-l-2 border-cyber-green/20">
                      <div className="grid grid-cols-2 gap-2">
                        <Input label="Provider" value={editRoom.aiProvider || ""} onChange={(e) => setEditRoom({ ...editRoom, aiProvider: e.target.value })} />
                        <Input label="Model" value={editRoom.aiModel || ""} onChange={(e) => setEditRoom({ ...editRoom, aiModel: e.target.value })} />
                      </div>
                      <div>
                        <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">System Prompt</label>
                        <textarea value={editRoom.aiSystemPrompt || ""} onChange={(e) => setEditRoom({ ...editRoom, aiSystemPrompt: e.target.value })}
                          className="w-full bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50 h-16 resize-y" />
                      </div>
                      <div className="grid grid-cols-2 gap-2">
                        <Input label="Temp" type="number" value={editRoom.aiTemperature ?? 0.7} onChange={(e) => setEditRoom({ ...editRoom, aiTemperature: parseFloat(e.target.value) || 0.7 })} />
                        <Input label="Tokens" type="number" value={editRoom.aiMaxTokens ?? 150} onChange={(e) => setEditRoom({ ...editRoom, aiMaxTokens: parseInt(e.target.value) || 150 })} />
                      </div>
                      <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                        <input type="checkbox" checked={editRoom.fallbackToScript ?? true} onChange={(e) => setEditRoom({ ...editRoom, fallbackToScript: e.target.checked })} className="accent-cyber-green" />
                        Fallback to Script
                      </label>
                    </div>
                  )}
                </div>

                <Button onClick={saveRoom} loading={savingRoom} disabled={!editRoom.name}>
                  <Save size={12} className="mr-1" /> Save Changes
                </Button>
              </div>
            </Card>
          </div>
        )}
        </div>
      )}

      {/* ═══ SCRIPTS TAB ═══ */}
      {tab === "scripts" && (
        <div className="flex gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex justify-end gap-2 mb-4">
            <Button variant="ghost" size="sm" onClick={fetchScripts}><RefreshCw size={14} className="mr-1.5" /> Refresh</Button>
            <Button size="sm" onClick={() => setShowCreateScript(true)}><Plus size={14} className="mr-1.5" /> New Script</Button>
          </div>

          {showCreateScript && (
            <Card className="mb-4 border-cyber-green/20">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Script</h3>
                <button onClick={() => setShowCreateScript(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={16} /></button>
              </div>
              <div className="space-y-3">
                <Input label="Title" value={newScript.title} onChange={(e) => setNewScript({ ...newScript, title: e.target.value })} />
                <Input label="Description" value={newScript.description} onChange={(e) => setNewScript({ ...newScript, description: e.target.value })} />
                <Input label="Category" value={newScript.category} onChange={(e) => setNewScript({ ...newScript, category: e.target.value })} placeholder="casual, business, sales" />
                <Button onClick={createScript} disabled={!newScript.title}>Create</Button>
              </div>
            </Card>
          )}

          {scriptsLoading ? <Card className="animate-pulse"><div className="h-32 bg-bg-hover rounded" /></Card> :
            scripts.length === 0 ? <Card><p className="text-cyber-green-muted text-sm text-center py-8">No scripts yet.</p></Card> :
              <div className="space-y-2">{scripts.map(script => (
                <div key={script.id}>
                  <Card className={`cursor-pointer transition-colors ${expandedScript === script.id ? "border-cyber-green/30" : ""} ${selectedScript?.id === script.id ? "bg-cyber-green/5" : ""}`}>
                    <div className="flex items-center justify-between" onClick={() => toggleScript(script.id)}>
                      <div className="flex items-center gap-2">
                        {expandedScript === script.id ? <ChevronDown size={14} className="text-cyber-green" /> : <ChevronRight size={14} className="text-cyber-green-muted" />}
                        <p className="text-sm font-bold text-cyber-green">{script.title}</p>
                        <Badge variant="muted">{script.category}</Badge>
                      </div>
                      <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                        <Button variant="outline" size="sm" onClick={() => selectScript(script)}><Edit3 size={13} /></Button>
                        <Button variant="danger" size="sm" onClick={() => deleteScript(script.id)}><Trash2 size={13} /></Button>
                      </div>
                    </div>

                    {/* ── EXPANDED LINES ── */}
                    {expandedScript === script.id && (
                      <div className="mt-3 pt-3 border-t border-border">
                        {linesLoading ? <p className="text-cyber-green-muted text-xs">Loading lines...</p> :
                          scriptLines.length === 0 ? <p className="text-cyber-green-muted text-xs mb-3">No dialog lines yet.</p> :
                            <div className="space-y-1 mb-3">{scriptLines.map((line, idx) => (
                              <div key={line.id} className="flex items-center gap-2 text-xs bg-bg-hover px-2 py-1.5 group">
                                <span className="text-cyber-green-muted w-6 shrink-0">#{line.sequenceOrder}</span>
                                <Badge variant={line.actorRole === "ACTOR_A" ? "success" : "info"} className="shrink-0">{line.actorRole === "ACTOR_A" ? "A" : "B"}</Badge>
                                {editingLine === line.id ? (
                                  <>
                                    <input value={editContent} onChange={(e) => setEditContent(e.target.value)}
                                      className="flex-1 bg-bg-input border border-cyber-green/30 text-cyber-green px-2 py-0.5 text-xs font-mono" autoFocus />
                                    <button onClick={() => saveLine(line.id)} className="text-cyber-green cursor-pointer"><Check size={12} /></button>
                                    <button onClick={() => setEditingLine(null)} className="text-cyber-green-muted cursor-pointer"><X size={12} /></button>
                                  </>
                                ) : (
                                  <>
                                    <span className="flex-1 text-cyber-green truncate">{line.messageContent}</span>
                                    <div className="hidden group-hover:flex items-center gap-0.5">
                                      <button onClick={() => reorderLine(line.id, "up")} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer" disabled={idx === 0}><ArrowUp size={11} /></button>
                                      <button onClick={() => reorderLine(line.id, "down")} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer" disabled={idx === scriptLines.length - 1}><ArrowDown size={11} /></button>
                                      <button onClick={() => { setEditingLine(line.id); setEditContent(line.messageContent) }} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><Edit3 size={11} /></button>
                                      <button onClick={() => deleteLine(line.id)} className="text-cyber-danger/50 hover:text-cyber-danger cursor-pointer"><Trash2 size={11} /></button>
                                    </div>
                                  </>
                                )}
                              </div>
                            ))}</div>}

                        {/* Add Line */}
                        <div className="flex gap-2 items-end">
                          <select value={newLine.actorRole} onChange={(e) => setNewLine({ ...newLine, actorRole: e.target.value as "ACTOR_A" | "ACTOR_B" })}
                            className="bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono w-24">
                            <option value="ACTOR_A">A</option><option value="ACTOR_B">B</option>
                          </select>
                          <input value={newLine.messageContent} onChange={(e) => setNewLine({ ...newLine, messageContent: e.target.value })}
                            placeholder="Message content..." className="flex-1 bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50"
                            onKeyDown={(e) => { if (e.key === "Enter") addLine() }} />
                          <Button size="sm" onClick={addLine} disabled={!newLine.messageContent}><Plus size={12} /></Button>
                        </div>

                        {/* AI Generate */}
                        <div className="flex gap-2 items-center mt-2">
                          <Button size="sm" variant="outline" onClick={generateLines}><Sparkles size={12} className="mr-1" /> AI Generate</Button>
                          <input type="number" value={genCount} onChange={(e) => setGenCount(parseInt(e.target.value) || 5)} min={1} max={20}
                            className="w-14 bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono text-center" />
                          <span className="text-[10px] text-cyber-green-muted">lines</span>
                        </div>
                      </div>
                    )}
                  </Card>
                </div>
              ))}</div>}
        </div>

        {/* Script Edit Panel */}
        {selectedScript && (
          <div className="w-80 shrink-0">
            <Card>
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Edit Script</h3>
                <button onClick={() => setSelectedScript(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
              </div>
              <div className="space-y-3">
                <Input label="Title" value={editScript.title} onChange={(e) => setEditScript({ ...editScript, title: e.target.value })} />
                <Input label="Description" value={editScript.description} onChange={(e) => setEditScript({ ...editScript, description: e.target.value })} />
                <Input label="Category" value={editScript.category} onChange={(e) => setEditScript({ ...editScript, category: e.target.value })} />
                <Button onClick={saveScript} loading={savingScript} disabled={!editScript.title}>
                  <Save size={12} className="mr-1" /> Save Changes
                </Button>
              </div>
            </Card>
          </div>
        )}
        </div>
      )}

      {/* ═══ TEMPLATES TAB ═══ */}
      {tab === "templates" && (
        <div>
          <div className="flex justify-end gap-2 mb-4">
            <Button variant="ghost" size="sm" onClick={fetchTemplates}><RefreshCw size={14} className="mr-1.5" /> Refresh</Button>
            <Button size="sm" onClick={() => setShowCreateTemplate(true)}><Plus size={14} className="mr-1.5" /> New Template</Button>
          </div>

          {showCreateTemplate && (
            <Card className="mb-4 border-cyber-green/20">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Template</h3>
                <button onClick={() => setShowCreateTemplate(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={16} /></button>
              </div>
              <div className="space-y-3">
                <Input label="Name" value={newTemplate.name} onChange={(e) => setNewTemplate({ ...newTemplate, name: e.target.value })} />
                <Input label="Category" value={newTemplate.category} onChange={(e) => setNewTemplate({ ...newTemplate, category: e.target.value })} />
                <div>
                  <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Structure (JSON)</label>
                  <textarea value={newTemplate.structure} onChange={(e) => setNewTemplate({ ...newTemplate, structure: e.target.value })}
                    className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50 h-24 resize-y" />
                </div>
                <Button onClick={createTemplate} disabled={!newTemplate.name}>Create</Button>
              </div>
            </Card>
          )}

          {templatesLoading ? <Card className="animate-pulse"><div className="h-32 bg-bg-hover rounded" /></Card> :
            templates.length === 0 ? <Card><p className="text-cyber-green-muted text-sm text-center py-8">No templates yet.</p></Card> :
              <div className="space-y-2">{templates.map(t => (
                <Card key={t.id} className="flex items-center justify-between">
                  <div>
                    <p className="text-sm font-bold text-cyber-green">{t.name}</p>
                    <p className="text-xs text-cyber-green-muted mt-0.5"><Badge variant="muted">{t.category}</Badge></p>
                  </div>
                  <Button variant="danger" size="sm" onClick={() => deleteTemplate(t.id)}><Trash2 size={13} /></Button>
                </Card>
              ))}</div>}
        </div>
      )}

      {/* ═══ LOGS TAB ═══ */}
      {tab === "logs" && (
        <div>
          <div className="flex gap-3 mb-4 items-end">
            <div className="flex-1">
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Room</label>
              <select value={logRoomId} onChange={(e) => setLogRoomId(e.target.value)} className={selCls}>
                <option value="">Select room</option>
                {rooms.map(r => <option key={r.id} value={r.id}>{r.name} ({r.status})</option>)}
              </select>
            </div>
            <div>
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Status</label>
              <div className="flex gap-1">
                {["", "SUCCESS", "FAILED"].map(s => (
                  <button key={s} onClick={() => setLogStatus(s)}
                    className={`px-2 py-1.5 text-xs font-mono border cursor-pointer transition-all ${logStatus === s ? "border-cyber-green bg-cyber-green/10 text-cyber-green" : "border-border text-cyber-green-muted hover:border-cyber-green/30"}`}>
                    {s || "ALL"}
                  </button>
                ))}
              </div>
            </div>
            <Button onClick={fetchLogs} disabled={!logRoomId}>Load</Button>
          </div>

          {logsLoading ? <Card className="animate-pulse"><div className="h-32 bg-bg-hover rounded" /></Card> :
            logs.length === 0 ? <Card><p className="text-cyber-green-muted text-sm text-center py-8">{logRoomId ? "No logs found." : "Select a room."}</p></Card> :
              <Card className="p-0 overflow-hidden">
                <table className="w-full text-xs">
                  <thead><tr className="border-b border-border text-cyber-green-muted uppercase">
                    <th className="text-left px-3 py-2">Status</th><th className="text-left px-3 py-2">Message</th><th className="text-right px-3 py-2">Time</th>
                  </tr></thead>
                  <tbody>{logs.map(log => (
                    <tr key={log.id} className="border-b border-border/50 hover:bg-bg-hover">
                      <td className="px-3 py-2"><Badge variant={log.status === "SUCCESS" ? "success" : "danger"}>{log.status}</Badge></td>
                      <td className="px-3 py-2 text-cyber-green-muted max-w-md truncate">{log.messageContent}</td>
                      <td className="px-3 py-2 text-right text-cyber-green-muted">{new Date(log.executedAt).toLocaleString()}</td>
                    </tr>
                  ))}</tbody>
                </table>
              </Card>}
        </div>
      )}
    </div>
  )
}
