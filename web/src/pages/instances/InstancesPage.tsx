import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import { Input } from "../../components/ui/Input"
import {
  Plus,
  Trash2,
  QrCode,
  WifiOff,
  RefreshCw,
  X,
  Save,
  Webhook,
  Smartphone,
} from "lucide-react"
import api from "../../lib/api"
import { globalWs } from "../../lib/ws"
import type { ApiResponse, Instance, DeviceInfo, WsEvent } from "../../lib/types"
import toast from "react-hot-toast"

export function InstancesPage() {
  const [instances, setInstances] = useState<Instance[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [circle, setCircle] = useState("")
  const [showCreate, setShowCreate] = useState(false)
  const [qrData, setQrData] = useState<{ instanceId: string; qr: string } | null>(null)
  const [scanningId, setScanningId] = useState<string | null>(null)

  // Detail panel
  const [selected, setSelected] = useState<Instance | null>(null)
  const [deviceInfo, setDeviceInfo] = useState<DeviceInfo | null>(null)
  const [editForm, setEditForm] = useState({ circle: "", description: "", used: false })
  const [saving, setSaving] = useState(false)
  const [webhookUrl, setWebhookUrl] = useState("")
  const [webhookSecret, setWebhookSecret] = useState("")
  const [savingWebhook, setSavingWebhook] = useState(false)

  const fetchInstances = useCallback(async () => {
    try {
      const res = await api.get<ApiResponse<{ instances: Instance[]; total: number }>>("/api/instances?all=true")
      if (res.data.success && res.data.data) {
        setInstances(res.data.data.instances || [])
      }
    } catch {
      toast.error("Failed to fetch instances")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchInstances() }, [fetchInstances])

  // WebSocket
  useEffect(() => {
    globalWs.connect()
    const handler = (event: WsEvent) => {
      const data = event.data as Record<string, unknown>
      if (event.event === "QR_GENERATED" && data.instanceId === scanningId) {
        setQrData({ instanceId: data.instanceId as string, qr: data.qr_data as string })
      }
      if (event.event === "INSTANCE_STATUS_CHANGED" || event.event === "QR_EXPIRED") {
        fetchInstances()
        if (event.event === "INSTANCE_STATUS_CHANGED" && data.status === "connected") {
          setQrData(null)
          setScanningId(null)
          toast.success(`Instance ${data.instanceId} connected`)
        }
        if (event.event === "QR_EXPIRED") { setQrData(null); setScanningId(null) }
      }
    }
    globalWs.on("*", handler)
    return () => globalWs.off("*", handler)
  }, [scanningId, fetchInstances])

  const handleSelectInstance = async (inst: Instance) => {
    setSelected(inst)
    setEditForm({ circle: inst.circle || "", description: inst.description || "", used: inst.used })
    setDeviceInfo(null)
    setWebhookUrl("")
    setWebhookSecret("")
    if (inst.connected) {
      try {
        const res = await api.get<ApiResponse<DeviceInfo>>(`/api/info-device/${inst.instanceId}`)
        if (res.data.success && res.data.data) setDeviceInfo(res.data.data)
      } catch { /* ignore */ }
    }
  }

  const handleCreate = async () => {
    setCreating(true)
    try {
      const res = await api.post<ApiResponse<{ instanceId: string }>>("/api/login", { circle: circle || "default" })
      if (res.data.success) {
        toast.success("Instance created")
        setShowCreate(false)
        setCircle("")
        fetchInstances()
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to create instance") } finally { setCreating(false) }
  }

  const handleScanQR = async (instanceId: string) => {
    setScanningId(instanceId)
    setQrData(null)
    try { await api.get(`/api/qr/${instanceId}`) } catch { toast.error("Failed to start QR scan"); setScanningId(null) }
  }

  const handleDelete = async (instanceId: string) => {
    if (!confirm(`Delete instance ${instanceId}?`)) return
    try {
      await api.delete(`/api/instances/${instanceId}`)
      toast.success("Instance deleted")
      if (selected?.instanceId === instanceId) setSelected(null)
      fetchInstances()
    } catch { toast.error("Failed to delete instance") }
  }

  const handleLogout = async (instanceId: string) => {
    try {
      await api.post(`/api/logout/${instanceId}`)
      toast.success("Instance disconnected")
      fetchInstances()
    } catch { toast.error("Failed to disconnect") }
  }

  const handleSaveEdit = async () => {
    if (!selected) return
    setSaving(true)
    try {
      await api.patch(`/api/instances/${selected.instanceId}`, editForm)
      toast.success("Instance updated")
      fetchInstances()
    } catch { toast.error("Failed to update") } finally { setSaving(false) }
  }

  const handleSaveWebhook = async () => {
    if (!selected) return
    setSavingWebhook(true)
    try {
      const res = await api.post<ApiResponse<{ secret: string }>>(`/api/instances/${selected.instanceId}/webhook-setconfig`, {
        url: webhookUrl,
        secret: webhookSecret || undefined,
      })
      if (res.data.success && res.data.data) {
        setWebhookSecret(res.data.data.secret)
        toast.success("Webhook configured")
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to set webhook") } finally { setSavingWebhook(false) }
  }

  const statusBadge = (inst: Instance) => {
    if (inst.connected) return <Badge variant="success">Connected</Badge>
    if (inst.status === "logged_out") return <Badge variant="danger">Logged Out</Badge>
    return <Badge variant="warning">Disconnected</Badge>
  }

  return (
    <div className="flex gap-4">
      {/* Left: Instance List */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-cyber-green">Instances</h2>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={fetchInstances} size="sm">
              <RefreshCw size={14} className="mr-1.5" /> Refresh
            </Button>
            <Button onClick={() => setShowCreate(true)} size="sm">
              <Plus size={14} className="mr-1.5" /> New Instance
            </Button>
          </div>
        </div>

        {showCreate && (
          <Card className="mb-6 border-cyber-green/20">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Instance</h3>
              <button onClick={() => setShowCreate(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={16} /></button>
            </div>
            <div className="flex gap-3">
              <Input placeholder="Circle (e.g. default)" value={circle} onChange={(e) => setCircle(e.target.value)} className="flex-1" />
              <Button onClick={handleCreate} loading={creating}>Create</Button>
            </div>
          </Card>
        )}

        {qrData && (
          <Card className="mb-6 border-cyber-green/30 shadow-[0_0_20px_rgba(0,255,65,0.1)]">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-bold text-cyber-green uppercase">Scan QR — {qrData.instanceId}</h3>
              <button onClick={() => { api.delete(`/api/qr-cancel/${qrData.instanceId}`).catch(() => {}); setQrData(null); setScanningId(null) }} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={16} /></button>
            </div>
            <div className="flex justify-center p-4">
              <div className="bg-white p-3 rounded">
                <img src={`https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(qrData.qr)}`} alt="QR Code" width={200} height={200} />
              </div>
            </div>
            <p className="text-xs text-cyber-green-muted text-center mt-2">Open WhatsApp → Linked Devices → Scan this QR code</p>
          </Card>
        )}

        {loading ? (
          <div className="space-y-3">
            {[...Array(3)].map((_, i) => <Card key={i} className="animate-pulse"><div className="h-12 bg-bg-hover rounded" /></Card>)}
          </div>
        ) : instances.length === 0 ? (
          <Card><p className="text-cyber-green-muted text-sm text-center py-8">No instances yet. Create one to get started.</p></Card>
        ) : (
          <div className="space-y-2">
            {instances.map((inst) => (
              <Card
                key={inst.instanceId}
                className={`flex items-center justify-between cursor-pointer transition-colors ${selected?.instanceId === inst.instanceId ? "border-cyber-green/30 bg-cyber-green/5" : "hover:bg-bg-hover"}`}
              >
                <div className="flex items-center gap-4 flex-1 min-w-0" onClick={() => handleSelectInstance(inst)}>
                  <div className={`w-2 h-2 rounded-full shrink-0 ${inst.connected ? "bg-cyber-green shadow-[0_0_8px_rgba(0,255,65,0.5)]" : "bg-cyber-green-muted"}`} />
                  <div className="min-w-0">
                    <p className="text-sm font-mono text-cyber-green truncate">{inst.instanceId}</p>
                    <div className="flex items-center gap-2 mt-0.5">
                      {statusBadge(inst)}
                      {inst.circle && <Badge variant="muted">{inst.circle}</Badge>}
                      {inst.phoneNumber && <span className="text-xs text-cyber-green-muted">{inst.phoneNumber}</span>}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  {!inst.connected && (
                    <Button variant="outline" size="sm" onClick={(e) => { e.stopPropagation(); handleScanQR(inst.instanceId) }} disabled={scanningId === inst.instanceId}>
                      <QrCode size={13} className="mr-1" /> QR
                    </Button>
                  )}
                  {inst.connected && (
                    <Button variant="ghost" size="sm" onClick={(e) => { e.stopPropagation(); handleLogout(inst.instanceId) }}>
                      <WifiOff size={13} />
                    </Button>
                  )}
                  <Button variant="danger" size="sm" onClick={(e) => { e.stopPropagation(); handleDelete(inst.instanceId) }}>
                    <Trash2 size={13} />
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>

      {/* Right: Detail Panel */}
      {selected && (
        <div className="w-80 shrink-0">
          <Card>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Instance Detail</h3>
              <button onClick={() => setSelected(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>

            {/* ID & Status */}
            <div className="space-y-2 text-xs mb-4 pb-4 border-b border-border">
              <div>
                <span className="text-cyber-green-muted">ID: </span>
                <span className="text-cyber-green font-mono">{selected.instanceId}</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-cyber-green-muted">Status: </span>
                {statusBadge(selected)}
              </div>
              {selected.phoneNumber && (
                <div>
                  <span className="text-cyber-green-muted">Phone: </span>
                  <span className="text-cyber-green">{selected.phoneNumber}</span>
                </div>
              )}
            </div>

            {/* Device Info */}
            {deviceInfo && (
              <div className="space-y-2 text-xs mb-4 pb-4 border-b border-border">
                <div className="flex items-center gap-1.5 mb-2">
                  <Smartphone size={12} className="text-cyber-green-dim" />
                  <span className="text-cyber-green-dim font-bold uppercase tracking-wider">Device Info</span>
                </div>
                <div>
                  <span className="text-cyber-green-muted">JID: </span>
                  <span className="text-cyber-green font-mono text-[10px]">{deviceInfo.jid}</span>
                </div>
                <div>
                  <span className="text-cyber-green-muted">Phone: </span>
                  <span className="text-cyber-green">{deviceInfo.phoneNumber}</span>
                </div>
              </div>
            )}

            {/* Edit Form */}
            <div className="space-y-3 mb-4 pb-4 border-b border-border">
              <h4 className="text-xs font-bold text-cyber-green-dim uppercase tracking-wider">Edit</h4>
              <Input label="Circle" value={editForm.circle} onChange={(e) => setEditForm({ ...editForm, circle: e.target.value })} />
              <Input label="Description" value={editForm.description} onChange={(e) => setEditForm({ ...editForm, description: e.target.value })} />
              <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                <input type="checkbox" checked={editForm.used} onChange={(e) => setEditForm({ ...editForm, used: e.target.checked })} className="accent-cyber-green" />
                Mark as used
              </label>
              <Button size="sm" onClick={handleSaveEdit} loading={saving}>
                <Save size={12} className="mr-1" /> Save
              </Button>
            </div>

            {/* Webhook Config */}
            <div className="space-y-3">
              <div className="flex items-center gap-1.5">
                <Webhook size={12} className="text-cyber-green-dim" />
                <h4 className="text-xs font-bold text-cyber-green-dim uppercase tracking-wider">Webhook</h4>
              </div>
              <Input label="Webhook URL" value={webhookUrl} onChange={(e) => setWebhookUrl(e.target.value)} placeholder="https://your-app.com/webhook" />
              <Input label="Secret (optional)" value={webhookSecret} onChange={(e) => setWebhookSecret(e.target.value)} placeholder="Auto-generated if empty" />
              <Button size="sm" variant="outline" onClick={handleSaveWebhook} loading={savingWebhook} disabled={!webhookUrl}>
                <Webhook size={12} className="mr-1" /> Configure
              </Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  )
}
