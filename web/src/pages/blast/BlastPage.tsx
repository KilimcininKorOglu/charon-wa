import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import { Input } from "../../components/ui/Input"
import {
  Rocket,
  Plus,
  Trash2,
  ToggleLeft,
  ToggleRight,
  RefreshCw,
  X,
  Save,
} from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, WorkerConfig } from "../../lib/types"
import toast from "react-hot-toast"

export function BlastPage() {
  const [configs, setConfigs] = useState<WorkerConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [circles, setCircles] = useState<string[]>([])
  const [applications, setApplications] = useState<string[]>([])

  // Edit panel
  const [selectedConfig, setSelectedConfig] = useState<WorkerConfig | null>(null)
  const [editForm, setEditForm] = useState({ worker_name: "", circle: "", application: "", message_type: "direct", interval_seconds: 10, interval_max_seconds: 30, enabled: true, allow_media: false, webhook_url: "", webhook_secret: "" })
  const [savingEdit, setSavingEdit] = useState(false)

  const [form, setForm] = useState({
    worker_name: "",
    circle: "",
    application: "",
    message_type: "direct",
    interval_seconds: 10,
    interval_max_seconds: 30,
    enabled: true,
    allow_media: false,
    webhook_url: "",
  })

  const fetchConfigs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get<ApiResponse<WorkerConfig[]>>("/api/blast-outbox/configs")
      if (res.data.success && res.data.data) {
        setConfigs(res.data.data)
      }
    } catch { /* ignore */ } finally { setLoading(false) }
  }, [])

  const fetchMeta = useCallback(async () => {
    try {
      const [circlesRes, appsRes] = await Promise.all([
        api.get<ApiResponse<string[]>>("/api/blast-outbox/available-circles"),
        api.get<ApiResponse<string[]>>("/api/blast-outbox/available-applications"),
      ])
      if (circlesRes.data.success && circlesRes.data.data) setCircles(circlesRes.data.data)
      if (appsRes.data.success && appsRes.data.data) setApplications(appsRes.data.data)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    fetchConfigs()
    fetchMeta()
  }, [fetchConfigs, fetchMeta])

  const handleCreate = async () => {
    if (!form.worker_name || !form.circle) {
      toast.error("Worker name and circle are required")
      return
    }
    setCreating(true)
    try {
      const res = await api.post<ApiResponse>("/api/blast-outbox/configs", form)
      if (res.data.success) {
        toast.success("Worker config created")
        setShowCreate(false)
        setForm({ worker_name: "", circle: "", application: "", message_type: "direct", interval_seconds: 10, interval_max_seconds: 30, enabled: true, allow_media: false, webhook_url: "" })
        fetchConfigs()
      } else {
        toast.error(res.data.message)
      }
    } catch { toast.error("Failed to create config") } finally { setCreating(false) }
  }

  const handleToggle = async (configId: number) => {
    try {
      await api.post(`/api/blast-outbox/configs/${configId}/toggle`)
      toast.success("Config toggled")
      fetchConfigs()
    } catch { toast.error("Failed to toggle") }
  }

  const handleDelete = async (configId: number) => {
    if (!confirm("Delete this worker config?")) return
    try {
      await api.delete(`/api/blast-outbox/configs/${configId}`)
      toast.success("Config deleted")
      if (selectedConfig?.id === configId) setSelectedConfig(null)
      fetchConfigs()
    } catch { toast.error("Failed to delete") }
  }

  const handleSelectConfig = (config: WorkerConfig) => {
    setSelectedConfig(config)
    setEditForm({
      worker_name: config.worker_name,
      circle: config.circle,
      application: config.application,
      message_type: config.message_type,
      interval_seconds: config.interval_seconds,
      interval_max_seconds: config.interval_max_seconds,
      enabled: config.enabled,
      allow_media: config.allow_media,
      webhook_url: config.webhook_url || "",
      webhook_secret: config.webhook_secret || "",
    })
  }

  const handleSaveEdit = async () => {
    if (!selectedConfig) return
    setSavingEdit(true)
    try {
      await api.put(`/api/blast-outbox/configs/${selectedConfig.id}`, editForm)
      toast.success("Config updated")
      fetchConfigs()
    } catch { toast.error("Failed to update") } finally { setSavingEdit(false) }
  }

  return (
    <div className="flex gap-4">
      <div className="flex-1 min-w-0">
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-cyber-green flex items-center gap-2">
          <Rocket size={20} /> Blast Outbox
        </h2>
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" onClick={fetchConfigs}>
            <RefreshCw size={14} className="mr-1.5" /> Refresh
          </Button>
          <Button size="sm" onClick={() => setShowCreate(true)}>
            <Plus size={14} className="mr-1.5" /> New Config
          </Button>
        </div>
      </div>

      {/* Create Form */}
      {showCreate && (
        <Card className="mb-6 border-cyber-green/20">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Worker Config</h3>
            <button onClick={() => setShowCreate(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer">
              <X size={16} />
            </button>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Input label="Worker Name" value={form.worker_name} onChange={(e) => setForm({ ...form, worker_name: e.target.value })} />
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider">Circle</label>
              <select
                value={form.circle}
                onChange={(e) => setForm({ ...form, circle: e.target.value })}
                className="bg-bg-input border border-border text-cyber-green px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50"
              >
                <option value="">Select circle</option>
                {circles.map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider">Application</label>
              <select
                value={form.application}
                onChange={(e) => setForm({ ...form, application: e.target.value })}
                className="bg-bg-input border border-border text-cyber-green px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50"
              >
                <option value="">All (*)</option>
                {applications.map((a) => <option key={a} value={a}>{a}</option>)}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider">Message Type</label>
              <select
                value={form.message_type}
                onChange={(e) => setForm({ ...form, message_type: e.target.value })}
                className="bg-bg-input border border-border text-cyber-green px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50"
              >
                <option value="direct">Direct</option>
                <option value="group">Group</option>
              </select>
            </div>
            <Input
              label="Interval Min (sec)"
              type="number"
              value={form.interval_seconds}
              onChange={(e) => setForm({ ...form, interval_seconds: parseInt(e.target.value) || 0 })}
            />
            <Input
              label="Interval Max (sec)"
              type="number"
              value={form.interval_max_seconds}
              onChange={(e) => setForm({ ...form, interval_max_seconds: parseInt(e.target.value) || 0 })}
            />
            <Input label="Webhook URL (optional)" value={form.webhook_url} onChange={(e) => setForm({ ...form, webhook_url: e.target.value })} />
            <div className="flex items-center gap-4 pt-6">
              <label className="flex items-center gap-2 text-sm text-cyber-green cursor-pointer">
                <input type="checkbox" checked={form.allow_media} onChange={(e) => setForm({ ...form, allow_media: e.target.checked })} className="accent-cyber-green" />
                Allow Media
              </label>
            </div>
          </div>
          <div className="mt-4">
            <Button onClick={handleCreate} loading={creating}>Create Config</Button>
          </div>
        </Card>
      )}

      {/* Config List */}
      {loading ? (
        <Card className="animate-pulse"><div className="h-32 bg-bg-hover rounded" /></Card>
      ) : configs.length === 0 ? (
        <Card><p className="text-cyber-green-muted text-sm text-center py-8">No worker configs. Create one to start blast messaging.</p></Card>
      ) : (
        <div className="space-y-2">
          {configs.map((config) => (
            <Card key={config.id} className={`flex items-center justify-between cursor-pointer transition-colors ${selectedConfig?.id === config.id ? "border-cyber-green/30 bg-cyber-green/5" : "hover:bg-bg-hover"}`}>
              <div className="flex-1 min-w-0" onClick={() => handleSelectConfig(config)}>
                <div className="flex items-center gap-2">
                  <p className="text-sm font-bold text-cyber-green">{config.worker_name}</p>
                  <Badge variant={config.enabled ? "success" : "muted"}>
                    {config.enabled ? "Enabled" : "Disabled"}
                  </Badge>
                  <Badge variant="info">{config.message_type}</Badge>
                </div>
                <p className="text-xs text-cyber-green-muted mt-1">
                  Circle: {config.circle} | App: {config.application || "*"} | Interval: {config.interval_seconds}s
                  {config.interval_max_seconds > 0 && `-${config.interval_max_seconds}s`}
                  {config.allow_media && " | Media: ON"}
                </p>
              </div>
              <div className="flex items-center gap-1.5 shrink-0" onClick={(e) => e.stopPropagation()}>
                <Button variant="outline" size="sm" onClick={() => handleToggle(config.id)}>
                  {config.enabled ? <><ToggleRight size={13} className="mr-1" /> Disable</> : <><ToggleLeft size={13} className="mr-1" /> Enable</>}
                </Button>
                <Button variant="danger" size="sm" onClick={() => handleDelete(config.id)}>
                  <Trash2 size={13} />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}
      </div>

      {/* Right: Edit Panel */}
      {selectedConfig && (
        <div className="w-80 shrink-0">
          <Card>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Edit Config</h3>
              <button onClick={() => setSelectedConfig(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>
            <div className="space-y-3">
              <Input label="Worker Name" value={editForm.worker_name} onChange={(e) => setEditForm({ ...editForm, worker_name: e.target.value })} />
              <Input label="Circle" value={editForm.circle} onChange={(e) => setEditForm({ ...editForm, circle: e.target.value })} />
              <Input label="Application" value={editForm.application} onChange={(e) => setEditForm({ ...editForm, application: e.target.value })} placeholder="* for all" />
              <div>
                <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-1.5">Message Type</label>
                <select value={editForm.message_type} onChange={(e) => setEditForm({ ...editForm, message_type: e.target.value })}
                  className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50">
                  <option value="direct">Direct</option><option value="group">Group</option>
                </select>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <Input label="Interval (s)" type="number" value={editForm.interval_seconds} onChange={(e) => setEditForm({ ...editForm, interval_seconds: parseInt(e.target.value) || 0 })} />
                <Input label="Max (s)" type="number" value={editForm.interval_max_seconds} onChange={(e) => setEditForm({ ...editForm, interval_max_seconds: parseInt(e.target.value) || 0 })} />
              </div>
              <label className="flex items-center gap-2 text-xs text-cyber-green cursor-pointer">
                <input type="checkbox" checked={editForm.allow_media} onChange={(e) => setEditForm({ ...editForm, allow_media: e.target.checked })} className="accent-cyber-green" />
                Allow Media
              </label>
              <Input label="Webhook URL" value={editForm.webhook_url} onChange={(e) => setEditForm({ ...editForm, webhook_url: e.target.value })} placeholder="https://..." />
              <Input label="Webhook Secret" value={editForm.webhook_secret} onChange={(e) => setEditForm({ ...editForm, webhook_secret: e.target.value })} placeholder="Auto-generated if empty" />
              <Button onClick={handleSaveEdit} loading={savingEdit}>
                <Save size={12} className="mr-1" /> Save Changes
              </Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  )
}
