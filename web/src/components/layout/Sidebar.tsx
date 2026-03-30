import { NavLink, useNavigate } from "react-router-dom"
import {
  LayoutDashboard,
  Smartphone,
  MessageSquare,
  BookUser,
  FolderOpen,
  Flame,
  Rocket,
  Users,
  UserCircle,
  Settings,
  LogOut,
} from "lucide-react"
import { useAuthStore } from "../../stores/authStore"

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/instances", icon: Smartphone, label: "Instances" },
  { to: "/messages", icon: MessageSquare, label: "Messages" },
  { to: "/contacts", icon: BookUser, label: "Contacts" },
  { to: "/files", icon: FolderOpen, label: "Files" },
  { to: "/warming/rooms", icon: Flame, label: "Warming" },
  { to: "/blast", icon: Rocket, label: "Blast" },
]

const adminItems = [
  { to: "/admin/users", icon: Users, label: "Users" },
]

const bottomItems = [
  { to: "/profile", icon: UserCircle, label: "Profile" },
  { to: "/system", icon: Settings, label: "System" },
]

export function Sidebar() {
  const { user, logout } = useAuthStore()
  const navigate = useNavigate()
  const isAdmin = user?.role === "admin" || user?.role === "superadmin"

  const handleLogout = async () => {
    await logout()
    navigate("/login")
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-3 px-3 py-2 text-sm transition-all duration-200 border-l-2 ${
      isActive
        ? "border-cyber-green text-cyber-green bg-cyber-green/5 shadow-[inset_0_0_20px_rgba(0,255,65,0.05)]"
        : "border-transparent text-cyber-green-muted hover:text-cyber-green hover:border-cyber-green/30 hover:bg-bg-hover"
    }`

  return (
    <aside className="fixed left-0 top-0 h-screen w-56 bg-bg-secondary border-r border-border flex flex-col z-50">
      {/* Logo */}
      <div className="px-4 py-5 border-b border-border">
        <h1 className="text-lg font-bold text-cyber-green tracking-wider">
          HERMES<span className="text-cyber-green-dim">WA</span>
        </h1>
        <p className="text-[10px] text-cyber-green-muted mt-0.5 uppercase tracking-widest">
          WhatsApp Automation
        </p>
      </div>

      {/* Nav */}
      <nav className="flex-1 py-3 overflow-y-auto">
        <div className="px-3 mb-2">
          <span className="text-[10px] text-cyber-green-muted uppercase tracking-widest">Main</span>
        </div>
        {navItems.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.to === "/"} className={linkClass}>
            <item.icon size={16} />
            {item.label}
          </NavLink>
        ))}

        {isAdmin && (
          <>
            <div className="px-3 mt-4 mb-2">
              <span className="text-[10px] text-cyber-green-muted uppercase tracking-widest">
                Admin
              </span>
            </div>
            {adminItems.map((item) => (
              <NavLink key={item.to} to={item.to} className={linkClass}>
                <item.icon size={16} />
                {item.label}
              </NavLink>
            ))}
          </>
        )}
      </nav>

      {/* Bottom */}
      <div className="border-t border-border py-2">
        {bottomItems.map((item) => (
          <NavLink key={item.to} to={item.to} className={linkClass}>
            <item.icon size={16} />
            {item.label}
          </NavLink>
        ))}
        <button
          onClick={handleLogout}
          className="flex items-center gap-3 px-3 py-2 text-sm text-cyber-danger/70 hover:text-cyber-danger hover:bg-cyber-danger/5 w-full transition-all border-l-2 border-transparent cursor-pointer"
        >
          <LogOut size={16} />
          Logout
        </button>
      </div>

      {/* User info */}
      {user && (
        <div className="px-4 py-3 border-t border-border bg-bg-primary/50">
          <p className="text-xs text-cyber-green truncate">{user.username}</p>
          <p className="text-[10px] text-cyber-green-muted">{user.role}</p>
        </div>
      )}
    </aside>
  )
}
