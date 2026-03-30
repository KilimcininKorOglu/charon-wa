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
  Info,
} from "lucide-react"
import api from "../../lib/api"
import { globalWs } from "../../lib/ws"
import type { ApiResponse, Instance, WsEvent } from "../../lib/types"
import toast from "react-hot-toast"

export function InstancesPage() {
  const [instances, setInstances] = useState<Instance[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [circle, setCircle] = useState("")
  const [showCreate, setShowCreate] = useState(false)
  const [qrData, setQrData] = useState<{ instanceId: string; qr: string } | null>(null)
  const [scanningId, setScanningId] = useState<string | null>(null)

  const fetchInstances = useCallback(async () => {
    try {
      const res = await api.get<ApiResponse<Instance[]>>("/api/instances?all=true")
      if (res.data.success && res.data.data) {
        setInstances(res.data.data)
      }
    } catch {
      toast.error("Failed to fetch instances")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchInstances()
  }, [fetchInstances])

  // WebSocket: QR events + status changes
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
        if (event.event === "QR_EXPIRED") {
          setQrData(null)
          setScanningId(null)
        }
      }
    }

    globalWs.on("*", handler)
    return () => globalWs.off("*", handler)
  }, [scanningId, fetchInstances])

  const handleCreate = async () => {
    setCreating(true)
    try {
      const res = await api.post<ApiResponse<{ instanceId: string }>>("/api/login", {
        circle: circle || "default",
      })
      if (res.data.success) {
        toast.success("Instance created")
        setShowCreate(false)
        setCircle("")
        fetchInstances()
      } else {
        toast.error(res.data.message)
      }
    } catch {
      toast.error("Failed to create instance")
    } finally {
      setCreating(false)
    }
  }

  const handleScanQR = async (instanceId: string) => {
    setScanningId(instanceId)
    setQrData(null)
    try {
      await api.get(`/api/qr/${instanceId}`)
    } catch {
      toast.error("Failed to start QR scan")
      setScanningId(null)
    }
  }

  const handleDelete = async (instanceId: string) => {
    if (!confirm(`Delete instance ${instanceId}?`)) return
    try {
      await api.delete(`/api/instances/${instanceId}`)
      toast.success("Instance deleted")
      fetchInstances()
    } catch {
      toast.error("Failed to delete instance")
    }
  }

  const handleLogout = async (instanceId: string) => {
    try {
      await api.post(`/api/logout/${instanceId}`)
      toast.success("Instance disconnected")
      fetchInstances()
    } catch {
      toast.error("Failed to disconnect")
    }
  }

  const statusBadge = (inst: Instance) => {
    if (inst.connected) return <Badge variant="success">Connected</Badge>
    if (inst.status === "logged_out") return <Badge variant="danger">Logged Out</Badge>
    return <Badge variant="warning">Disconnected</Badge>
  }

  return (
    <div>
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

      {/* Create Modal */}
      {showCreate && (
        <Card className="mb-6 border-cyber-green/20">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Create Instance</h3>
            <button onClick={() => setShowCreate(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer">
              <X size={16} />
            </button>
          </div>
          <div className="flex gap-3">
            <Input
              placeholder="Circle (e.g. default)"
              value={circle}
              onChange={(e) => setCircle(e.target.value)}
              className="flex-1"
            />
            <Button onClick={handleCreate} loading={creating}>
              Create
            </Button>
          </div>
        </Card>
      )}

      {/* QR Scanner */}
      {qrData && (
        <Card className="mb-6 border-cyber-green/30 shadow-[0_0_20px_rgba(0,255,65,0.1)]">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-bold text-cyber-green uppercase">
              Scan QR — {qrData.instanceId}
            </h3>
            <button
              onClick={() => { setQrData(null); setScanningId(null) }}
              className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"
            >
              <X size={16} />
            </button>
          </div>
          <div className="flex justify-center p-4">
            <div className="bg-white p-3 rounded">
              <img
                src={`https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(qrData.qr)}`}
                alt="QR Code"
                width={200}
                height={200}
              />
            </div>
          </div>
          <p className="text-xs text-cyber-green-muted text-center mt-2">
            Open WhatsApp → Linked Devices → Scan this QR code
          </p>
        </Card>
      )}

      {/* Instance List */}
      {loading ? (
        <div className="space-y-3">
          {[...Array(3)].map((_, i) => (
            <Card key={i} className="animate-pulse"><div className="h-12 bg-bg-hover rounded" /></Card>
          ))}
        </div>
      ) : instances.length === 0 ? (
        <Card>
          <p className="text-cyber-green-muted text-sm text-center py-8">
            No instances yet. Create one to get started.
          </p>
        </Card>
      ) : (
        <div className="space-y-2">
          {instances.map((inst) => (
            <Card key={inst.instanceId} className="flex items-center justify-between">
              <div className="flex items-center gap-4">
                <div className={`w-2 h-2 rounded-full ${inst.connected ? "bg-cyber-green shadow-[0_0_8px_rgba(0,255,65,0.5)]" : "bg-cyber-green-muted"}`} />
                <div>
                  <p className="text-sm font-mono text-cyber-green">{inst.instanceId}</p>
                  <div className="flex items-center gap-2 mt-0.5">
                    {statusBadge(inst)}
                    {inst.circle && <Badge variant="muted">{inst.circle}</Badge>}
                    {inst.phoneNumber && (
                      <span className="text-xs text-cyber-green-muted">{inst.phoneNumber}</span>
                    )}
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-1.5">
                {!inst.connected && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleScanQR(inst.instanceId)}
                    disabled={scanningId === inst.instanceId}
                  >
                    <QrCode size={13} className="mr-1" /> Scan QR
                  </Button>
                )}
                {inst.connected && (
                  <>
                    <Button variant="ghost" size="sm" onClick={() => toast("Device info coming soon")}>
                      <Info size={13} />
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => handleLogout(inst.instanceId)}>
                      <WifiOff size={13} />
                    </Button>
                  </>
                )}
                <Button variant="danger" size="sm" onClick={() => handleDelete(inst.instanceId)}>
                  <Trash2 size={13} />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
