import { useState, useEffect, useCallback, type FormEvent } from "react"
import { useAuthStore } from "../../stores/authStore"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Input } from "../../components/ui/Input"
import { Badge } from "../../components/ui/Badge"
import { CountrySelect } from "../../components/ui/CountrySelect"
import { UserCircle, Key, Upload, Plus, Trash2, Copy, X } from "lucide-react"
import api from "../../lib/api"
import { apiFailure } from "../../lib/apiError"
import { useSystemRegion } from "../../lib/phone"
import type { ApiResponse, User, APIKey } from "../../lib/types"
import toast from "react-hot-toast"

export function ProfilePage() {
  const { user, setUser, fetchProfile } = useAuthStore()
  const [fullName, setFullName] = useState("")
  const [phoneRegion, setPhoneRegion] = useState("")
  const [saving, setSaving] = useState(false)
  const systemRegion = useSystemRegion()

  const [oldPassword, setOldPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [changingPassword, setChangingPassword] = useState(false)

  const [uploading, setUploading] = useState(false)

  // API Keys
  const [apiKeys, setApiKeys] = useState<APIKey[]>([])
  const [showCreateKey, setShowCreateKey] = useState(false)
  const [newKeyName, setNewKeyName] = useState("")
  const [newKeyApp, setNewKeyApp] = useState("")
  const [creatingKey, setCreatingKey] = useState(false)
  const [generatedKey, setGeneratedKey] = useState("")

  useEffect(() => {
    if (user) {
      setFullName(user.full_name || "")
      setPhoneRegion(user.phone_default_region || "")
    }
  }, [user])

  const fetchApiKeys = useCallback(async () => {
    try {
      const res = await api.get<ApiResponse<APIKey[]>>("/api/api-keys")
      if (res.data.success && res.data.data) setApiKeys(res.data.data)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => { fetchApiKeys() }, [fetchApiKeys])

  const handleCreateKey = async () => {
    if (!newKeyName) return
    setCreatingKey(true)
    try {
      const res = await api.post<ApiResponse<{ id: number; key: string }>>("/api/api-keys", {
        name: newKeyName, application: newKeyApp || undefined,
      })
      if (res.data.success && res.data.data) {
        setGeneratedKey(res.data.data.key)
        setNewKeyName("")
        setNewKeyApp("")
        fetchApiKeys()
      } else { toast.error(res.data.message) }
    } catch { toast.error("Failed to create API key") } finally { setCreatingKey(false) }
  }

  const handleDeleteKey = async (id: number) => {
    if (!confirm("Delete this API key? Any integrations using it will stop working.")) return
    try {
      await api.delete(`/api/api-keys/${id}`)
      toast.success("API key deleted")
      fetchApiKeys()
    } catch { toast.error("Failed to delete") }
  }

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success("Copied to clipboard")
  }

  const handleUpdateProfile = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const res = await api.put<ApiResponse<User>>("/api/me", {
        full_name: fullName,
        phone_default_region: phoneRegion,
      })
      if (res.data.success && res.data.data) {
        setUser(res.data.data)
        toast.success("Profile updated")
      }
    } catch (err) {
      toast.error(apiFailure(err, "Failed to update profile").message)
    } finally {
      setSaving(false)
    }
  }

  const handleChangePassword = async (e: FormEvent) => {
    e.preventDefault()
    if (newPassword.length < 6) {
      toast.error("Password must be at least 6 characters")
      return
    }
    setChangingPassword(true)
    try {
      const res = await api.put<ApiResponse>("/api/me/password", {
        old_password: oldPassword,
        new_password: newPassword,
      })
      if (res.data.success) {
        toast.success("Password changed")
        setOldPassword("")
        setNewPassword("")
      } else {
        toast.error(res.data.message)
      }
    } catch {
      toast.error("Failed to change password")
    } finally {
      setChangingPassword(false)
    }
  }

  const handleAvatarUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    const formData = new FormData()
    formData.append("avatar", file)

    setUploading(true)
    try {
      const res = await api.post<ApiResponse>("/api/me/avatar", formData, {
        headers: { "Content-Type": "multipart/form-data" },
      })
      if (res.data.success) {
        toast.success("Avatar uploaded")
        await fetchProfile()
      } else {
        toast.error(res.data.message)
      }
    } catch {
      toast.error("Failed to upload avatar")
    } finally {
      setUploading(false)
      e.target.value = ""
    }
  }

  if (!user) return null

  return (
    <div className="max-w-2xl">
      <h2 className="text-xl font-bold mb-6 text-cyber-green">Profile</h2>

      {/* User Info */}
      <Card className="mb-6">
        <div className="flex items-center gap-4">
          <div className="relative group">
            {user.avatar_url ? (
              <img
                src={user.avatar_url}
                alt="Avatar"
                className="w-16 h-16 rounded border border-border object-cover"
              />
            ) : (
              <div className="w-16 h-16 rounded border border-border bg-bg-hover flex items-center justify-center">
                <UserCircle size={32} className="text-cyber-green-muted" />
              </div>
            )}
            <label className="absolute inset-0 flex items-center justify-center bg-black/60 opacity-0 group-hover:opacity-100 cursor-pointer transition-opacity rounded">
              {uploading ? (
                <span className="h-4 w-4 animate-spin border-2 border-cyber-green border-t-transparent rounded-full" />
              ) : (
                <Upload size={16} className="text-cyber-green" />
              )}
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp"
                onChange={handleAvatarUpload}
                className="hidden"
                disabled={uploading}
              />
            </label>
          </div>
          <div>
            <p className="text-lg font-bold text-cyber-green">{user.username}</p>
            <p className="text-sm text-cyber-green-muted">{user.email}</p>
            <div className="flex gap-2 mt-1">
              <Badge variant={user.is_active ? "success" : "danger"}>
                {user.is_active ? "Active" : "Inactive"}
              </Badge>
              <Badge variant="info">{user.role}</Badge>
            </div>
          </div>
        </div>
      </Card>

      {/* Edit Profile */}
      <Card className="mb-6">
        <div className="flex items-center gap-2 mb-4">
          <UserCircle size={16} className="text-cyber-green-dim" />
          <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">
            Edit Profile
          </h3>
        </div>
        <form onSubmit={handleUpdateProfile} className="space-y-4">
          <Input label="Username" value={user.username} disabled />
          <Input label="Email" value={user.email} disabled />
          <Input
            label="Full Name"
            value={fullName}
            onChange={(e) => setFullName(e.target.value)}
            placeholder="Enter full name"
          />
          <CountrySelect
            label="Default Phone Country"
            value={phoneRegion}
            onChange={setPhoneRegion}
            emptyLabel={`Follow the system default (${systemRegion})`}
          />
          <p className="text-[10px] text-cyber-green-muted">
            Preselects the country in every phone field. It does not change which numbers the
            server accepts, because a phone field always sends the full international number.
          </p>
          <Button type="submit" loading={saving}>
            Save Changes
          </Button>
        </form>
      </Card>

      {/* Change Password */}
      <Card className="mb-6">
        <div className="flex items-center gap-2 mb-4">
          <Key size={16} className="text-cyber-green-dim" />
          <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">
            Change Password
          </h3>
        </div>
        <form onSubmit={handleChangePassword} className="space-y-4">
          <Input
            label="Current Password"
            type="password"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            required
          />
          <Input
            label="New Password"
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            placeholder="Minimum 6 characters"
            required
            minLength={6}
          />
          <Button type="submit" loading={changingPassword} variant="outline">
            Update Password
          </Button>
        </form>
      </Card>

      {/* API Keys */}
      <Card>
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <Key size={16} className="text-cyber-green-dim" />
            <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">API Keys</h3>
          </div>
          <Button size="sm" onClick={() => { setShowCreateKey(true); setGeneratedKey("") }}>
            <Plus size={14} className="mr-1" /> Generate Key
          </Button>
        </div>

        {/* Generated key banner (shown once) */}
        {generatedKey && (
          <div className="mb-4 p-3 border border-cyber-green/30 bg-cyber-green/5">
            <p className="text-xs text-cyber-green-dim uppercase mb-1.5">Your new API key (shown once):</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 text-xs text-cyber-green font-mono bg-bg-input px-2 py-1.5 border border-border select-all break-all">{generatedKey}</code>
              <button onClick={() => copyToClipboard(generatedKey)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer shrink-0"><Copy size={14} /></button>
            </div>
            <button onClick={() => setGeneratedKey("")} className="text-[10px] text-cyber-green-muted hover:text-cyber-green mt-2 cursor-pointer">Dismiss</button>
          </div>
        )}

        {/* Create key form */}
        {showCreateKey && !generatedKey && (
          <div className="mb-4 p-3 border border-border space-y-3">
            <div className="flex items-center justify-between">
              <p className="text-xs text-cyber-green-dim uppercase font-bold">New API Key</p>
              <button onClick={() => setShowCreateKey(false)} className="text-cyber-green-muted hover:text-cyber-green cursor-pointer"><X size={14} /></button>
            </div>
            <Input label="Name" value={newKeyName} onChange={(e) => setNewKeyName(e.target.value)} placeholder="e.g. Marketing CRM" />
            <Input label="Application (optional)" value={newKeyApp} onChange={(e) => setNewKeyApp(e.target.value)} placeholder="Lock to specific application" />
            <Button size="sm" onClick={handleCreateKey} loading={creatingKey} disabled={!newKeyName}>Generate</Button>
          </div>
        )}

        {/* Key list */}
        {apiKeys.length === 0 ? (
          <p className="text-cyber-green-muted text-xs text-center py-4">No API keys. Generate one to integrate external applications.</p>
        ) : (
          <div className="space-y-2">
            {apiKeys.map((k) => (
              <div key={k.id} className="flex items-center justify-between text-xs px-2 py-2 border border-border hover:bg-bg-hover transition-colors">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-cyber-green font-bold">{k.name}</span>
                    <code className="text-cyber-green-muted font-mono text-[10px]">{k.key_prefix}...</code>
                    {k.application && <Badge variant="info">{k.application}</Badge>}
                  </div>
                  <p className="text-[10px] text-cyber-green-muted mt-0.5">
                    Created: {new Date(k.created_at).toLocaleDateString()}
                    {k.last_used_at && ` | Last used: ${new Date(k.last_used_at).toLocaleDateString()}`}
                  </p>
                </div>
                <Button variant="danger" size="sm" onClick={() => handleDeleteKey(k.id)}>
                  <Trash2 size={12} />
                </Button>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
