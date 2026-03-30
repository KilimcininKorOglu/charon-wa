import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import {
  BookUser,
  Search,
  Download,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
  Smartphone,
  Users,
  X,
  CheckCircle,
} from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, Instance, Contact } from "../../lib/types"
import toast from "react-hot-toast"

export function ContactsPage() {
  const [instances, setInstances] = useState<Instance[]>([])
  const [selectedInstance, setSelectedInstance] = useState("")
  const [contacts, setContacts] = useState<Contact[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState("")
  const [loading, setLoading] = useState(false)
  const limit = 50

  // Detail panel
  const [selectedContact, setSelectedContact] = useState<Contact | null>(null)
  const [mutualGroups, setMutualGroups] = useState<string[]>([])

  // Number check
  const [checkPhone, setCheckPhone] = useState("")
  const [checkResult, setCheckResult] = useState<{ isRegistered: boolean; jid: string } | null>(null)
  const [checking, setChecking] = useState(false)

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

  const fetchContacts = useCallback(async () => {
    if (!selectedInstance) return
    setLoading(true)
    try {
      const res = await api.get<ApiResponse<{ contacts: Contact[]; total: number }>>(`/api/contacts/${selectedInstance}?page=${page}&limit=${limit}&search=${search}`)
      if (res.data.success && res.data.data) {
        setContacts(res.data.data.contacts || [])
        setTotal(res.data.data.total || 0)
      }
    } catch { setContacts([]); setTotal(0) } finally { setLoading(false) }
  }, [selectedInstance, page, search])

  useEffect(() => { fetchContacts() }, [fetchContacts])

  const handleSelectContact = async (contact: Contact) => {
    setSelectedContact(contact)
    setMutualGroups([])
    if (contact.jid && selectedInstance) {
      try {
        const res = await api.get<ApiResponse<{ mutualGroups: string[]; total: number }>>(`/api/contacts/${selectedInstance}/${encodeURIComponent(contact.jid)}/mutual-groups`)
        if (res.data.success && res.data.data) {
          setMutualGroups(res.data.data.mutualGroups || [])
        }
      } catch { /* ignore */ }
    }
  }

  const handleExport = async (format: "xlsx" | "csv") => {
    if (!selectedInstance) return
    try {
      const res = await api.get(`/api/contacts/${selectedInstance}/export?format=${format}`, { responseType: "blob" })
      const url = window.URL.createObjectURL(new Blob([res.data]))
      const a = document.createElement("a")
      a.href = url
      a.download = `contacts_${selectedInstance}.${format}`
      a.click()
      window.URL.revokeObjectURL(url)
      toast.success(`Exported as ${format.toUpperCase()}`)
    } catch { toast.error("Export failed") }
  }

  const handleCheck = async () => {
    if (!checkPhone || !selectedInstance) return
    setChecking(true)
    setCheckResult(null)
    try {
      const res = await api.post<ApiResponse<{ isRegistered: boolean; jid: string }>>(`/api/check/${selectedInstance}`, { phone: checkPhone })
      if (res.data.success && res.data.data) setCheckResult(res.data.data)
      else toast.error(res.data.message)
    } catch { toast.error("Check failed") } finally { setChecking(false) }
  }

  const totalPages = Math.ceil(total / limit)

  return (
    <div className="flex gap-4">
      {/* Left: Contact list */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-cyber-green flex items-center gap-2"><BookUser size={20} /> Contacts</h2>
          {selectedInstance && (
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" onClick={() => handleExport("xlsx")}><Download size={14} className="mr-1" /> XLSX</Button>
              <Button variant="ghost" size="sm" onClick={() => handleExport("csv")}><Download size={14} className="mr-1" /> CSV</Button>
            </div>
          )}
        </div>

        {/* Instance selector */}
        <Card className="mb-4">
          <div className="flex gap-3 items-end">
            <div className="flex-1">
              <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5"><Smartphone size={10} className="inline mr-1" /> Instance</label>
              <div className="relative">
                <select value={selectedInstance} onChange={(e) => { setSelectedInstance(e.target.value); setPage(1); setSelectedContact(null) }}
                  className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50 appearance-none cursor-pointer">
                  <option value="">Select instance</option>
                  {instances.map((inst) => <option key={inst.instanceId} value={inst.instanceId}>{inst.instanceId} {inst.phoneNumber ? `(${inst.phoneNumber})` : ""}</option>)}
                </select>
                <ChevronDown size={14} className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-green-muted pointer-events-none" />
              </div>
            </div>
            <div className="flex-1">
              <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5"><Search size={10} className="inline mr-1" /> Search</label>
              <div className="relative">
                <Search size={14} className="absolute left-2 top-1/2 -translate-y-1/2 text-cyber-green-muted" />
                <input value={search} onChange={(e) => { setSearch(e.target.value); setPage(1) }} placeholder="Name, phone, JID..."
                  className="w-full bg-bg-input border border-border text-cyber-green pl-8 pr-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
              </div>
            </div>
          </div>
        </Card>

        {/* Number Check */}
        <Card className="mb-4">
          <div className="flex gap-2 items-end">
            <div className="flex-1">
              <label className="text-[10px] text-cyber-green-dim uppercase tracking-wider block mb-1.5"><CheckCircle size={10} className="inline mr-1" /> Number Check</label>
              <input value={checkPhone} onChange={(e) => setCheckPhone(e.target.value)} placeholder="628xxxxxxxxxx"
                className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-xs font-mono focus:outline-none focus:border-cyber-green/50" />
            </div>
            <Button size="sm" onClick={handleCheck} loading={checking} disabled={!checkPhone || !selectedInstance}>Check</Button>
          </div>
          {checkResult && (
            <div className="mt-2">
              <Badge variant={checkResult.isRegistered ? "success" : "danger"}>{checkResult.isRegistered ? "Registered" : "Not Found"}</Badge>
              {checkResult.isRegistered && <span className="text-[10px] text-cyber-green-muted ml-2">{checkResult.jid}</span>}
            </div>
          )}
        </Card>

        {/* Contact Table */}
        {!selectedInstance ? (
          <Card><p className="text-cyber-green-muted text-sm text-center py-8">Select an instance to view contacts</p></Card>
        ) : loading ? (
          <Card className="animate-pulse"><div className="h-60 bg-bg-hover rounded" /></Card>
        ) : contacts.length === 0 ? (
          <Card><p className="text-cyber-green-muted text-sm text-center py-8">No contacts found</p></Card>
        ) : (
          <>
            <Card className="p-0 overflow-hidden">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border text-cyber-green-muted uppercase">
                    <th className="text-left px-3 py-2.5">Name</th>
                    <th className="text-left px-3 py-2.5">Phone</th>
                    <th className="text-left px-3 py-2.5">Type</th>
                  </tr>
                </thead>
                <tbody>
                  {contacts.map((c) => (
                    <tr key={c.jid} onClick={() => handleSelectContact(c)}
                      className={`border-b border-border/50 hover:bg-bg-hover cursor-pointer transition-colors ${selectedContact?.jid === c.jid ? "bg-cyber-green/5" : ""}`}>
                      <td className="px-3 py-2 text-cyber-green">{c.name || "--"}</td>
                      <td className="px-3 py-2 text-cyber-green font-mono">{c.phoneNumber}</td>
                      <td className="px-3 py-2"><Badge variant={c.isGroup ? "info" : "muted"}>{c.isGroup ? "Group" : "Contact"}</Badge></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
            {totalPages > 1 && (
              <div className="flex items-center justify-between mt-3">
                <span className="text-xs text-cyber-green-muted">Page {page} of {totalPages} ({total} contacts)</span>
                <div className="flex gap-1">
                  <Button variant="ghost" size="sm" onClick={() => setPage(page - 1)} disabled={page <= 1}><ChevronLeft size={14} /></Button>
                  <Button variant="ghost" size="sm" onClick={() => setPage(page + 1)} disabled={page >= totalPages}><ChevronRight size={14} /></Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Right: Detail panel */}
      {selectedContact && (
        <div className="w-72 shrink-0">
          <Card>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">Contact Detail</h3>
              <button onClick={() => setSelectedContact(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>
            <div className="space-y-2 text-xs">
              <div><span className="text-cyber-green-muted">Name: </span><span className="text-cyber-green">{selectedContact.name || "--"}</span></div>
              <div><span className="text-cyber-green-muted">Phone: </span><span className="text-cyber-green font-mono">{selectedContact.phoneNumber}</span></div>
              <div><span className="text-cyber-green-muted">JID: </span><span className="text-cyber-green font-mono text-[10px]">{selectedContact.jid}</span></div>
              {selectedContact.pushName && <div><span className="text-cyber-green-muted">Push Name: </span><span className="text-cyber-green">{selectedContact.pushName}</span></div>}
              {selectedContact.businessName && <div><span className="text-cyber-green-muted">Business: </span><span className="text-cyber-green">{selectedContact.businessName}</span></div>}
              {selectedContact.about && <div><span className="text-cyber-green-muted">About: </span><span className="text-cyber-green">{selectedContact.about}</span></div>}
              <div><span className="text-cyber-green-muted">Type: </span><Badge variant={selectedContact.isGroup ? "info" : "muted"}>{selectedContact.isGroup ? "Group" : "Contact"}</Badge></div>
            </div>

            {/* Mutual Groups */}
            {mutualGroups.length > 0 && (
              <div className="mt-4 pt-4 border-t border-border">
                <h4 className="text-xs font-bold text-cyber-green-dim uppercase mb-2 flex items-center gap-1"><Users size={12} /> Mutual Groups ({mutualGroups.length})</h4>
                <div className="space-y-1">
                  {mutualGroups.map((g, i) => (
                    <div key={i} className="text-xs text-cyber-green bg-bg-hover px-2 py-1">{g}</div>
                  ))}
                </div>
              </div>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}
