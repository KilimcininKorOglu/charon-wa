import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import {
  Users,
  Search,
  Trash2,
  Shield,
  ShieldOff,
  ChevronLeft,
  ChevronRight,
  Link2,
  Unlink,
  X,
} from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, PaginatedUsers, User } from "../../lib/types"
import toast from "react-hot-toast"

interface UserInstanceAssignment {
  instanceId: string
  permissionLevel: string
  createdAt: string
}

export function AdminUsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState("")
  const [loading, setLoading] = useState(true)

  // Detail panel
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [userInstances, setUserInstances] = useState<UserInstanceAssignment[]>([])
  const [assignInstanceId, setAssignInstanceId] = useState("")

  const limit = 15

  const fetchUsers = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get<ApiResponse<PaginatedUsers>>("/api/admin/users", {
        params: { page, limit, search },
      })
      if (res.data.success && res.data.data) {
        setUsers(res.data.data.users)
        setTotal(res.data.data.total)
      }
    } catch {
      toast.error("Failed to fetch users")
    } finally {
      setLoading(false)
    }
  }, [page, search])

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

  const fetchUserInstances = async (userId: number) => {
    try {
      const res = await api.get<ApiResponse<UserInstanceAssignment[]>>(`/api/admin/users/${userId}/instances`)
      if (res.data.success && res.data.data) {
        setUserInstances(res.data.data)
      }
    } catch { setUserInstances([]) }
  }

  const handleSelectUser = async (user: User) => {
    setSelectedUser(user)
    await fetchUserInstances(user.id)
  }

  const handleToggleActive = async (user: User) => {
    try {
      const res = await api.patch<ApiResponse<User>>(`/api/admin/users/${user.id}`, {
        is_active: !user.is_active,
      })
      if (res.data.success) {
        toast.success(`User ${user.is_active ? "deactivated" : "activated"}`)
        fetchUsers()
        if (selectedUser?.id === user.id && res.data.data) {
          setSelectedUser({ ...selectedUser, is_active: !user.is_active })
        }
      }
    } catch { toast.error("Failed to update user") }
  }

  const handleChangeRole = async (userId: number, role: string) => {
    try {
      await api.patch(`/api/admin/users/${userId}`, { role })
      toast.success(`Role changed to ${role}`)
      fetchUsers()
      if (selectedUser?.id === userId) {
        setSelectedUser({ ...selectedUser, role })
      }
    } catch { toast.error("Failed to change role") }
  }

  const handleDelete = async (userId: number) => {
    if (!confirm("Delete this user permanently?")) return
    try {
      await api.delete(`/api/admin/users/${userId}`)
      toast.success("User deleted")
      if (selectedUser?.id === userId) setSelectedUser(null)
      fetchUsers()
    } catch { toast.error("Failed to delete user") }
  }

  const handleAssignInstance = async () => {
    if (!selectedUser || !assignInstanceId) return
    try {
      await api.post(`/api/admin/users/${selectedUser.id}/instances`, {
        instanceId: assignInstanceId,
      })
      toast.success("Instance assigned")
      setAssignInstanceId("")
      fetchUserInstances(selectedUser.id)
    } catch { toast.error("Failed to assign instance") }
  }

  const handleRevokeInstance = async (instanceId: string) => {
    if (!selectedUser) return
    try {
      await api.delete(`/api/admin/users/${selectedUser.id}/instances/${instanceId}`)
      toast.success("Instance revoked")
      fetchUserInstances(selectedUser.id)
    } catch { toast.error("Failed to revoke instance") }
  }

  const totalPages = Math.ceil(total / limit)

  return (
    <div className="flex gap-4">
      {/* User List */}
      <div className="flex-1">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-bold text-cyber-green flex items-center gap-2">
            <Users size={20} /> User Management
          </h2>
          <Badge variant="info">{total} users</Badge>
        </div>

        {/* Search */}
        <div className="flex gap-2 mb-4">
          <div className="relative flex-1">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-cyber-green-muted" />
            <input
              value={search}
              onChange={(e) => { setSearch(e.target.value); setPage(1) }}
              placeholder="Search username, email, name..."
              className="w-full bg-bg-input border border-border text-cyber-green placeholder-cyber-green-muted/50 pl-9 pr-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50"
            />
          </div>
        </div>

        {/* Table */}
        {loading ? (
          <Card className="animate-pulse"><div className="h-60 bg-bg-hover rounded" /></Card>
        ) : (
          <Card className="p-0 overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-xs text-cyber-green-muted uppercase">
                  <th className="text-left px-3 py-2.5">User</th>
                  <th className="text-left px-3 py-2.5">Role</th>
                  <th className="text-left px-3 py-2.5">Status</th>
                  <th className="text-right px-3 py-2.5">Actions</th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr
                    key={user.id}
                    className={`border-b border-border/50 hover:bg-bg-hover cursor-pointer transition-colors ${selectedUser?.id === user.id ? "bg-cyber-green/5" : ""}`}
                    onClick={() => handleSelectUser(user)}
                  >
                    <td className="px-3 py-2.5">
                      <p className="text-cyber-green font-bold text-xs">{user.username}</p>
                      <p className="text-cyber-green-muted text-[10px]">{user.email}</p>
                    </td>
                    <td className="px-3 py-2.5">
                      <Badge variant={user.role === "admin" ? "success" : "muted"}>{user.role}</Badge>
                    </td>
                    <td className="px-3 py-2.5">
                      <Badge variant={user.is_active ? "success" : "danger"}>
                        {user.is_active ? "Active" : "Inactive"}
                      </Badge>
                    </td>
                    <td className="px-3 py-2.5 text-right">
                      <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggleActive(user)}
                          title={user.is_active ? "Deactivate" : "Activate"}
                        >
                          {user.is_active ? <ShieldOff size={13} /> : <Shield size={13} />}
                        </Button>
                        <Button variant="danger" size="sm" onClick={() => handleDelete(user.id)}>
                          <Trash2 size={13} />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="flex items-center justify-between mt-3">
            <span className="text-xs text-cyber-green-muted">
              Page {page} of {totalPages}
            </span>
            <div className="flex gap-1">
              <Button variant="ghost" size="sm" onClick={() => setPage(page - 1)} disabled={page <= 1}>
                <ChevronLeft size={14} />
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setPage(page + 1)} disabled={page >= totalPages}>
                <ChevronRight size={14} />
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* Detail Panel */}
      {selectedUser && (
        <div className="w-80 shrink-0">
          <Card>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase">User Detail</h3>
              <button onClick={() => setSelectedUser(null)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer">
                <X size={14} />
              </button>
            </div>

            <div className="space-y-3 text-xs">
              <div>
                <span className="text-cyber-green-muted">Username: </span>
                <span className="text-cyber-green">{selectedUser.username}</span>
              </div>
              <div>
                <span className="text-cyber-green-muted">Email: </span>
                <span className="text-cyber-green">{selectedUser.email}</span>
              </div>
              <div>
                <span className="text-cyber-green-muted">Full Name: </span>
                <span className="text-cyber-green">{selectedUser.full_name || "--"}</span>
              </div>
              <div>
                <span className="text-cyber-green-muted">Role: </span>
                <select
                  value={selectedUser.role}
                  onChange={(e) => handleChangeRole(selectedUser.id, e.target.value)}
                  className="bg-bg-input border border-border text-cyber-green px-2 py-1 text-xs font-mono cursor-pointer"
                >
                  <option value="user">user</option>
                  <option value="admin">admin</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
              <div>
                <span className="text-cyber-green-muted">Created: </span>
                <span className="text-cyber-green">{new Date(selectedUser.created_at).toLocaleDateString()}</span>
              </div>
            </div>

            {/* Instance Assignments */}
            <div className="mt-6 border-t border-border pt-4">
              <h4 className="text-xs font-bold text-cyber-green-dim uppercase mb-3 flex items-center gap-1.5">
                <Link2 size={12} /> Instance Access
              </h4>

              <div className="flex gap-1.5 mb-3">
                <input
                  value={assignInstanceId}
                  onChange={(e) => setAssignInstanceId(e.target.value)}
                  placeholder="Instance ID"
                  className="flex-1 bg-bg-input border border-border text-cyber-green px-2 py-1.5 text-xs font-mono focus:outline-none focus:border-cyber-green/50"
                />
                <Button size="sm" onClick={handleAssignInstance} disabled={!assignInstanceId}>
                  <Link2 size={12} />
                </Button>
              </div>

              {userInstances.length === 0 ? (
                <p className="text-cyber-green-muted text-xs">No instances assigned</p>
              ) : (
                <div className="space-y-1">
                  {userInstances.map((inst) => (
                    <div key={inst.instanceId} className="flex items-center justify-between bg-bg-hover px-2 py-1.5 text-xs">
                      <span className="text-cyber-green font-mono truncate">{inst.instanceId}</span>
                      <button
                        onClick={() => handleRevokeInstance(inst.instanceId)}
                        className="text-cyber-danger/50 hover:text-cyber-danger cursor-pointer shrink-0 ml-2"
                      >
                        <Unlink size={12} />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </Card>
        </div>
      )}
    </div>
  )
}
