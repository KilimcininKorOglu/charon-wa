import { useEffect, useState, type FormEvent } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Input } from "../../components/ui/Input"
import { CountrySelect } from "../../components/ui/CountrySelect"
import { Settings, Upload, Building2, Phone } from "lucide-react"
import api from "../../lib/api"
import { apiFailure } from "../../lib/apiError"
import { reloadSystemRegion } from "../../lib/phone"
import { useAuthStore } from "../../stores/authStore"
import type { ApiResponse } from "../../lib/types"
import toast from "react-hot-toast"

interface SystemIdentity {
  company_name?: string
  company_short_name?: string
  company_address?: string
  company_phone?: string
  company_email?: string
  company_website?: string
  company_description?: string
  ico_url?: string
  logo_url?: string
  second_logo_url?: string
}

interface PhoneConfig {
  defaultRegion?: string
}

export function SystemPage() {
  const [identity, setIdentity] = useState<SystemIdentity>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [phoneRegion, setPhoneRegion] = useState("")
  const [savingRegion, setSavingRegion] = useState(false)
  const isAdmin = useAuthStore((state) => state.user?.role === "admin")

  useEffect(() => {
    const fetch = async () => {
      try {
        const res = await api.get<ApiResponse<SystemIdentity>>("/api/system/identity")
        if (res.data.success && res.data.data) {
          setIdentity(res.data.data)
        }
      } catch { /* ignore */ } finally { setLoading(false) }
    }
    fetch()
  }, [])

  useEffect(() => {
    const fetchPhoneConfig = async () => {
      try {
        const res = await api.get<ApiResponse<PhoneConfig>>("/api/system/phone-config")
        setPhoneRegion(res.data.data?.defaultRegion ?? "")
      } catch (err) {
        toast.error(apiFailure(err, "Failed to load the phone configuration").message)
      }
    }
    fetchPhoneConfig()
  }, [])

  const handlePhoneRegionSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSavingRegion(true)

    try {
      const res = await api.post<ApiResponse>("/api/system/phone-config", {
        default_region: phoneRegion,
      })
      if (!res.data.success) {
        toast.error(res.data.message)
        return
      }
      // Open phone fields read the region from a module-scope value, so refresh
      // it here instead of asking the operator to reload the page.
      await reloadSystemRegion()
      toast.success("Default phone country updated")
    } catch (err) {
      toast.error(apiFailure(err, "Failed to update the default phone country").message)
    } finally {
      setSavingRegion(false)
    }
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)

    const formData = new FormData()
    if (identity.company_name) formData.append("company_name", identity.company_name)
    if (identity.company_short_name) formData.append("company_short_name", identity.company_short_name)
    if (identity.company_address) formData.append("company_address", identity.company_address)
    if (identity.company_phone) formData.append("company_phone", identity.company_phone)
    if (identity.company_email) formData.append("company_email", identity.company_email)
    if (identity.company_website) formData.append("company_website", identity.company_website)
    if (identity.company_description) formData.append("company_description", identity.company_description)

    // File inputs
    const icoInput = document.getElementById("ico-upload") as HTMLInputElement
    const logoInput = document.getElementById("logo-upload") as HTMLInputElement
    const secondLogoInput = document.getElementById("second-logo-upload") as HTMLInputElement
    if (icoInput?.files?.[0]) formData.append("ico", icoInput.files[0])
    if (logoInput?.files?.[0]) formData.append("logo", logoInput.files[0])
    if (secondLogoInput?.files?.[0]) formData.append("second_logo", secondLogoInput.files[0])

    try {
      const res = await api.post<ApiResponse>("/api/system/identity", formData, {
        headers: { "Content-Type": "multipart/form-data" },
      })
      if (res.data.success) {
        toast.success("System identity updated")
      } else {
        toast.error(res.data.message)
      }
    } catch {
      toast.error("Failed to update system identity")
    } finally {
      setSaving(false)
    }
  }

  const update = (field: keyof SystemIdentity, value: string) => {
    setIdentity((prev) => ({ ...prev, [field]: value }))
  }

  if (loading) {
    return (
      <div>
        <h2 className="text-xl font-bold mb-6 text-cyber-green">System Settings</h2>
        <Card className="animate-pulse"><div className="h-60 bg-bg-hover rounded" /></Card>
      </div>
    )
  }

  return (
    <div className="max-w-2xl">
      <h2 className="text-xl font-bold mb-6 text-cyber-green flex items-center gap-2">
        <Settings size={20} /> System Settings
      </h2>

      <form onSubmit={handleSubmit}>
        {/* Company Info */}
        <Card className="mb-6">
          <div className="flex items-center gap-2 mb-4">
            <Building2 size={16} className="text-cyber-green-dim" />
            <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">
              Company Identity
            </h3>
          </div>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <Input label="Company Name" value={identity.company_name || ""} onChange={(e) => update("company_name", e.target.value)} />
              <Input label="Short Name" value={identity.company_short_name || ""} onChange={(e) => update("company_short_name", e.target.value)} />
            </div>
            <Input label="Address" value={identity.company_address || ""} onChange={(e) => update("company_address", e.target.value)} />
            <div className="grid grid-cols-2 gap-3">
              <Input label="Phone" value={identity.company_phone || ""} onChange={(e) => update("company_phone", e.target.value)} />
              <Input label="Email" value={identity.company_email || ""} onChange={(e) => update("company_email", e.target.value)} />
            </div>
            <Input label="Website" value={identity.company_website || ""} onChange={(e) => update("company_website", e.target.value)} />
            <Input label="Description" value={identity.company_description || ""} onChange={(e) => update("company_description", e.target.value)} />
          </div>
        </Card>

        {/* Branding */}
        <Card className="mb-6">
          <div className="flex items-center gap-2 mb-4">
            <Upload size={16} className="text-cyber-green-dim" />
            <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">
              Branding
            </h3>
          </div>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-2">Favicon</label>
              {identity.ico_url && (
                <img src={identity.ico_url} alt="Favicon" className="w-10 h-10 mb-2 border border-border rounded bg-bg-hover" />
              )}
              <input id="ico-upload" type="file" accept="image/*" className="text-xs text-cyber-green-muted file:bg-bg-hover file:border file:border-border file:text-cyber-green file:px-2 file:py-1 file:text-xs file:font-mono file:mr-2 file:cursor-pointer" />
            </div>
            <div>
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-2">Logo</label>
              {identity.logo_url && (
                <img src={identity.logo_url} alt="Logo" className="h-10 mb-2 border border-border rounded bg-bg-hover" />
              )}
              <input id="logo-upload" type="file" accept="image/*" className="text-xs text-cyber-green-muted file:bg-bg-hover file:border file:border-border file:text-cyber-green file:px-2 file:py-1 file:text-xs file:font-mono file:mr-2 file:cursor-pointer" />
            </div>
            <div>
              <label className="text-xs text-cyber-green-dim uppercase tracking-wider block mb-2">Secondary Logo</label>
              {identity.second_logo_url && (
                <img src={identity.second_logo_url} alt="Secondary Logo" className="h-10 mb-2 border border-border rounded bg-bg-hover" />
              )}
              <input id="second-logo-upload" type="file" accept="image/*" className="text-xs text-cyber-green-muted file:bg-bg-hover file:border file:border-border file:text-cyber-green file:px-2 file:py-1 file:text-xs file:font-mono file:mr-2 file:cursor-pointer" />
            </div>
          </div>
        </Card>

        <Button type="submit" loading={saving} size="lg">
          Save System Identity
        </Button>
      </form>

      {isAdmin && (
        <form onSubmit={handlePhoneRegionSubmit} className="mt-6">
          <Card className="mb-6">
            <div className="flex items-center gap-2 mb-4">
              <Phone size={16} className="text-cyber-green-dim" />
              <h3 className="text-sm font-bold text-cyber-green-dim uppercase tracking-wider">
                Phone Numbers
              </h3>
            </div>
            <CountrySelect
              label="Default Phone Country"
              value={phoneRegion}
              onChange={setPhoneRegion}
              emptyLabel="No default (every number needs a country code)"
              disabled={savingRegion}
            />
            <p className="mt-2 text-xs text-cyber-green-muted">
              Phone fields preselect this country, and a number typed without a country
              code is read as a number of this country. A user can override it on their
              own profile page.
            </p>
          </Card>

          <Button type="submit" loading={savingRegion} size="lg">
            Save Phone Settings
          </Button>
        </form>
      )}
    </div>
  )
}
