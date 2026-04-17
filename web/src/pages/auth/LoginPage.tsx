import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { useAuthStore } from "../../stores/authStore"
import { Button } from "../../components/ui/Button"
import { Input } from "../../components/ui/Input"
import toast from "react-hot-toast"

export function LoginPage() {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [loading, setLoading] = useState(false)
  const login = useAuthStore((s) => s.login)
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      await login(username, password)
      toast.success("Access granted")
      navigate("/")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Login failed")
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-primary">
      <div className="w-full max-w-sm">
        {/* Header */}
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-cyber-green tracking-wider">
            Charon
          </h1>
          <p className="text-xs text-cyber-green-muted mt-2 uppercase tracking-[0.3em]">
            Authentication Required
          </p>
          <div className="mt-4 h-px bg-gradient-to-r from-transparent via-cyber-green/30 to-transparent" />
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="bg-bg-card border border-border p-6 space-y-4">
          <Input
            label="Username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Enter username"
            autoFocus
            required
          />
          <Input
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter password"
            required
          />
          <Button type="submit" loading={loading} className="w-full">
            Initialize Session
          </Button>
        </form>
      </div>
    </div>
  )
}
