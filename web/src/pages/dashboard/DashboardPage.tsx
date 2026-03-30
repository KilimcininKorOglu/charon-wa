import { useEffect, useState } from "react"
import { Card } from "../../components/ui/Card"
import { Badge } from "../../components/ui/Badge"
import {
  Smartphone,
  Wifi,
  Users,
  Flame,
  Rocket,
  Activity,
} from "lucide-react"
import api from "../../lib/api"
import { globalWs } from "../../lib/ws"
import type { AdminStats, ApiResponse, WsEvent } from "../../lib/types"

interface StatCardProps {
  icon: React.ElementType
  label: string
  value: number | string
  glow?: boolean
}

function StatCard({ icon: Icon, label, value, glow }: StatCardProps) {
  return (
    <Card glow={glow} className="flex items-center gap-4">
      <div className="p-3 bg-cyber-green/5 border border-cyber-green/10 rounded">
        <Icon size={20} className="text-cyber-green" />
      </div>
      <div>
        <p className="text-xs text-cyber-green-muted uppercase tracking-wider">{label}</p>
        <p className="text-2xl font-bold text-cyber-green mt-0.5">{value}</p>
      </div>
    </Card>
  )
}

export function DashboardPage() {
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [events, setEvents] = useState<WsEvent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchStats = async () => {
      try {
        const res = await api.get<ApiResponse<AdminStats>>("/api/admin/stats")
        if (res.data.success && res.data.data) {
          setStats(res.data.data)
        }
      } catch {
        // User may not be admin — stats unavailable
      } finally {
        setLoading(false)
      }
    }
    fetchStats()
  }, [])

  // WebSocket events
  useEffect(() => {
    globalWs.connect()

    const handler = (event: WsEvent) => {
      setEvents((prev) => [event, ...prev].slice(0, 50))
    }

    globalWs.on("*", handler)
    return () => {
      globalWs.off("*", handler)
    }
  }, [])

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-cyber-green">Dashboard</h2>
        <Badge variant="success">
          <span className="inline-block w-1.5 h-1.5 bg-cyber-green rounded-full mr-1.5 animate-pulse" />
          Live
        </Badge>
      </div>

      {/* Stats Grid */}
      {loading ? (
        <div className="grid grid-cols-3 gap-4">
          {[...Array(6)].map((_, i) => (
            <Card key={i} className="animate-pulse">
              <div className="h-16 bg-bg-hover rounded" />
            </Card>
          ))}
        </div>
      ) : stats ? (
        <div className="grid grid-cols-3 gap-4">
          <StatCard icon={Users} label="Total Users" value={stats.totalUsers} />
          <StatCard icon={Activity} label="Active Users" value={stats.activeUsers} />
          <StatCard icon={Smartphone} label="Total Instances" value={stats.totalInstances} glow />
          <StatCard icon={Wifi} label="Connected" value={stats.connectedInstances} glow />
          <StatCard icon={Flame} label="Active Warming" value={stats.activeWarmingRooms} />
          <StatCard icon={Rocket} label="Active Workers" value={stats.activeWorkers} />
        </div>
      ) : (
        <Card>
          <p className="text-cyber-green-muted text-sm">
            Stats are available for admin users only.
          </p>
        </Card>
      )}

      {/* Live Events */}
      <div className="mt-8">
        <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider mb-3">
          Live Events
        </h3>
        <Card className="max-h-80 overflow-y-auto">
          {events.length === 0 ? (
            <p className="text-cyber-green-muted text-xs">
              Waiting for WebSocket events...
            </p>
          ) : (
            <div className="space-y-1">
              {events.map((event, i) => (
                <div
                  key={i}
                  className="flex items-center gap-3 text-xs py-1.5 border-b border-border/50 last:border-0"
                >
                  <span className="text-cyber-green-muted w-20 shrink-0">
                    {new Date(event.timestamp).toLocaleTimeString()}
                  </span>
                  <Badge
                    variant={
                      event.event.includes("ERROR") || event.event.includes("FAILED")
                        ? "danger"
                        : event.event.includes("CONNECTED") || event.event.includes("SUCCESS")
                          ? "success"
                          : "info"
                    }
                  >
                    {event.event}
                  </Badge>
                  <span className="text-cyber-green-muted truncate">
                    {typeof event.data === "object"
                      ? JSON.stringify(event.data).slice(0, 80)
                      : String(event.data)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  )
}
