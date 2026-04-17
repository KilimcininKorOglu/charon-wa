import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { Toaster } from "react-hot-toast"
import { useEffect } from "react"

import { useAuthStore } from "./stores/authStore"
import { Layout } from "./components/layout/Layout"
import { LoginPage } from "./pages/auth/LoginPage"
import { DashboardPage } from "./pages/dashboard/DashboardPage"
import { InstancesPage } from "./pages/instances/InstancesPage"
import { MessagesPage } from "./pages/messages/MessagesPage"
import { ContactsPage } from "./pages/contacts/ContactsPage"
import { FilesPage } from "./pages/files/FilesPage"
import { WarmingPage } from "./pages/warming/WarmingPage"
import { BlastPage } from "./pages/blast/BlastPage"
import { OutboxPage } from "./pages/outbox/OutboxPage"
import { AdminUsersPage } from "./pages/admin/AdminUsersPage"
import { ProfilePage } from "./pages/profile/ProfilePage"
import { SystemPage } from "./pages/system/SystemPage"

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  if (!isAuthenticated) return <Navigate to="/login" replace />
  return <>{children}</>
}

function GuestRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  if (isAuthenticated) return <Navigate to="/" replace />
  return <>{children}</>
}

export default function App() {
  const { isAuthenticated, fetchProfile } = useAuthStore()

  useEffect(() => {
    if (isAuthenticated) {
      fetchProfile()
    }
  }, [isAuthenticated, fetchProfile])

  return (
    <BrowserRouter>
      <Toaster
        position="top-right"
        toastOptions={{
          style: {
            background: "#111111",
            color: "#00ff41",
            border: "1px solid #1f1f1f",
            fontFamily: "JetBrains Mono, monospace",
            fontSize: "13px",
          },
        }}
      />
      <Routes>
        {/* Guest routes */}
        <Route path="/login" element={<GuestRoute><LoginPage /></GuestRoute>} />

        {/* Protected routes */}
        <Route
          element={
            <ProtectedRoute>
              <Layout />
            </ProtectedRoute>
          }
        >
          <Route path="/" element={<DashboardPage />} />
          <Route path="/instances" element={<InstancesPage />} />
          <Route path="/messages" element={<MessagesPage />} />
          <Route path="/contacts" element={<ContactsPage />} />
          <Route path="/files" element={<FilesPage />} />
          <Route path="/warming/rooms" element={<WarmingPage />} />
          <Route path="/blast" element={<BlastPage />} />
          <Route path="/outbox" element={<OutboxPage />} />
          <Route path="/admin/users" element={<AdminUsersPage />} />
          <Route path="/profile" element={<ProfilePage />} />
          <Route path="/system" element={<SystemPage />} />
        </Route>

        {/* Catch-all */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
