import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import {
  Mail,
  RefreshCw,
  X,
  ChevronLeft,
  ChevronRight,
} from "lucide-react"
import api from "../../lib/api"
import { formatPhoneDisplay } from "../../lib/phone"
import type { ApiResponse, OutboxMessage } from "../../lib/types"
import toast from "react-hot-toast"

const statusVariant = (s: number) => {
  if (s === 0) return "warning" as const
  if (s === 1) return "success" as const
  if (s === 2) return "danger" as const
  if (s === 3) return "info" as const
  return "muted" as const
}

const statusLabel = (s: number) => {
  if (s === 0) return "Pending"
  if (s === 1) return "Sent"
  if (s === 2) return "Failed"
  if (s === 3) return "Processing"
  return "Unknown"
}

export function OutboxPage() {
  const [messages, setMessages] = useState<OutboxMessage[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [limit] = useState(25)
  const [statusFilter, setStatusFilter] = useState<string>("")
  const [appFilter, setAppFilter] = useState("")
  const [applications, setApplications] = useState<string[]>([])

  // Detail panel
  const [selected, setSelected] = useState<OutboxMessage | null>(null)

  const fetchMessages = useCallback(async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) })
      if (statusFilter !== "") params.set("status", statusFilter)
      if (appFilter) params.set("application", appFilter)
      const res = await api.get<ApiResponse<{ messages: OutboxMessage[]; total: number }>>(`/api/outbox/messages?${params}`)
      if (res.data.success && res.data.data) {
        setMessages(res.data.data.messages || [])
        setTotal(res.data.data.total)
      }
    } catch { toast.error("Failed to load messages") } finally { setLoading(false) }
  }, [page, limit, statusFilter, appFilter])

  const fetchApps = useCallback(async () => {
    try {
      const res = await api.get<ApiResponse<string[]>>("/api/blast-outbox/available-applications")
      if (res.data.success && res.data.data) setApplications(res.data.data)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => { fetchMessages() }, [fetchMessages])
  useEffect(() => { fetchApps() }, [fetchApps])

  const totalPages = Math.ceil(total / limit)

  return (
    <div className="flex gap-4">
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-cyber-green flex items-center gap-2">
            <Mail size={20} /> Outbox Queue
          </h2>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={fetchMessages}>
              <RefreshCw size={14} className="mr-1.5" /> Refresh
            </Button>
          </div>
        </div>

        {/* Filters */}
        <div className="flex gap-3 mb-4 items-end flex-wrap">
          <div>
            <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5">Application</label>
            <select value={appFilter} onChange={(e) => { setAppFilter(e.target.value); setPage(1) }}
              className="bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50">
              <option value="">All</option>
              {applications.map((a) => <option key={a} value={a}>{a}</option>)}
            </select>
          </div>
          <div>
            <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5">Status</label>
            <div className="flex gap-1">
              {[{ v: "", l: "All" }, { v: "0", l: "Pending" }, { v: "1", l: "Sent" }, { v: "2", l: "Failed" }, { v: "3", l: "Processing" }].map((s) => (
                <button key={s.v} onClick={() => { setStatusFilter(s.v); setPage(1) }}
                  className={`px-2 py-1.5 text-[10px] font-mono border cursor-pointer transition-all ${statusFilter === s.v ? "border-cyber-green bg-cyber-green/10 text-cyber-green" : "border-border text-cyber-green-muted hover:border-cyber-green/30"}`}>
                  {s.l}
                </button>
              ))}
            </div>
          </div>
          <div className="ml-auto text-xs text-cyber-green-muted">{total} messages</div>
        </div>

        {/* Table */}
        {loading ? (
          <Card className="animate-pulse"><div className="h-48 bg-bg-hover rounded" /></Card>
        ) : messages.length === 0 ? (
          <Card><p className="text-cyber-green-muted text-sm text-center py-8">No messages found.</p></Card>
        ) : (
          <Card className="p-0 overflow-hidden">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border text-cyber-green-muted uppercase">
                  <th className="text-left px-3 py-2 w-16">ID</th>
                  <th className="text-left px-3 py-2">Destination</th>
                  <th className="text-left px-3 py-2">Message</th>
                  <th className="text-left px-3 py-2">App</th>
                  <th className="text-left px-3 py-2">Status</th>
                  <th className="text-right px-3 py-2">Time</th>
                </tr>
              </thead>
              <tbody>
                {messages.map((msg) => (
                  <tr key={msg.id_outbox}
                    onClick={() => setSelected(msg)}
                    className={`border-b border-border/50 cursor-pointer transition-colors ${selected?.id_outbox === msg.id_outbox ? "bg-cyber-green/5" : "hover:bg-bg-hover"}`}>
                    <td className="px-3 py-2 text-cyber-green-muted">#{msg.id_outbox}</td>
                    <td className="px-3 py-2 text-cyber-green font-mono" title={msg.destination}>{formatPhoneDisplay(msg.destination)}</td>
                    <td className="px-3 py-2 text-cyber-green-muted max-w-xs truncate">{msg.messages}</td>
                    <td className="px-3 py-2">{msg.application && <Badge variant="muted">{msg.application}</Badge>}</td>
                    <td className="px-3 py-2"><Badge variant={statusVariant(msg.status)}>{statusLabel(msg.status)}</Badge></td>
                    <td className="px-3 py-2 text-right text-cyber-green-muted whitespace-nowrap">{new Date(msg.insert_date_time).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="flex items-center justify-center gap-3 mt-4">
            <Button variant="ghost" size="sm" onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page <= 1}>
              <ChevronLeft size={14} />
            </Button>
            <span className="text-xs text-cyber-green-muted">Page {page} / {totalPages}</span>
            <Button variant="ghost" size="sm" onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page >= totalPages}>
              <ChevronRight size={14} />
            </Button>
          </div>
        )}
      </div>

      {/* Detail Panel */}
      {selected && (
        <div className="w-80 shrink-0">
          <Card>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Message #{selected.id_outbox}</h3>
              <button onClick={() => setSelected(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>
            <div className="space-y-3 text-xs">
              <div>
                <span className="text-cyber-green-muted">Status: </span>
                <Badge variant={statusVariant(selected.status)}>{statusLabel(selected.status)}</Badge>
              </div>
              <div>
                <span className="text-cyber-green-muted">Destination: </span>
                <span className="text-cyber-green font-mono" title={selected.destination}>{formatPhoneDisplay(selected.destination)}</span>
              </div>
              {selected.from_number && (
                <div>
                  <span className="text-cyber-green-muted">From: </span>
                  <span className="text-cyber-green font-mono">{selected.from_number}</span>
                </div>
              )}
              {selected.application && (
                <div>
                  <span className="text-cyber-green-muted">Application: </span>
                  <Badge variant="info">{selected.application}</Badge>
                </div>
              )}
              {selected.table_id && (
                <div>
                  <span className="text-cyber-green-muted">Ref ID: </span>
                  <span className="text-cyber-green font-mono text-[10px]">{selected.table_id}</span>
                </div>
              )}
              <div>
                <span className="text-cyber-green-dim uppercase tracking-wider block mb-1">Message</span>
                <p className="text-cyber-green whitespace-pre-wrap break-words bg-bg-hover p-2 border border-border">{selected.messages}</p>
              </div>
              {selected.file && (
                <div>
                  <span className="text-cyber-green-muted">Media: </span>
                  <span className="text-cyber-green text-[10px] break-all">{selected.file}</span>
                </div>
              )}
              {selected.msg_error && (
                <div>
                  <span className="text-cyber-green-dim uppercase tracking-wider block mb-1">Error</span>
                  <p className="text-cyber-danger text-[10px] bg-cyber-danger/5 p-2 border border-cyber-danger/20">{selected.msg_error}</p>
                </div>
              )}
              <div className="pt-2 border-t border-border space-y-1">
                <div>
                  <span className="text-cyber-green-muted">Queued: </span>
                  <span className="text-cyber-green">{new Date(selected.insert_date_time).toLocaleString()}</span>
                </div>
                {selected.sending_date_time && (
                  <div>
                    <span className="text-cyber-green-muted">Sent: </span>
                    <span className="text-cyber-green">{new Date(selected.sending_date_time).toLocaleString()}</span>
                  </div>
                )}
                <div>
                  <span className="text-cyber-green-muted">Priority: </span>
                  <span className="text-cyber-green">{selected.priority}</span>
                </div>
              </div>
            </div>
          </Card>
        </div>
      )}
    </div>
  )
}
