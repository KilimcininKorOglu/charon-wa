import { useEffect, useState, useCallback } from "react"
import { Card } from "../../components/ui/Card"
import { Button } from "../../components/ui/Button"
import { Badge } from "../../components/ui/Badge"
import {
  Folder,
  FileImage,
  File as FileIcon,
  Trash2,
  ChevronRight,
  Home,
  RefreshCw,
  ExternalLink,
} from "lucide-react"
import api from "../../lib/api"
import type { ApiResponse, FileEntry } from "../../lib/types"
import { useAuthStore } from "../../stores/authStore"
import toast from "react-hot-toast"

function formatSize(bytes?: number): string {
  if (!bytes) return "--"
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fileIcon(entry: FileEntry) {
  if (entry.isDir) return <Folder size={16} className="text-cyber-green-dim" />
  if (/\.(jpg|jpeg|png|webp|gif|svg)$/i.test(entry.name))
    return <FileImage size={16} className="text-cyber-green" />
  return <FileIcon size={16} className="text-cyber-green-muted" />
}

export function FilesPage() {
  const [files, setFiles] = useState<FileEntry[]>([])
  const [currentPath, setCurrentPath] = useState("")
  const [loading, setLoading] = useState(true)
  const [preview, setPreview] = useState<string | null>(null)
  const user = useAuthStore((s) => s.user)
  const isAdmin = user?.role === "admin" || user?.role === "superadmin"

  const fetchFiles = useCallback(async (path: string) => {
    setLoading(true)
    try {
      const res = await api.get<ApiResponse<FileEntry[]>>("/api/files", {
        params: { path },
      })
      if (res.data.success && res.data.data) {
        setFiles(res.data.data)
      } else {
        setFiles([])
      }
    } catch {
      toast.error("Failed to load files")
      setFiles([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchFiles(currentPath)
  }, [currentPath, fetchFiles])

  const navigateTo = (path: string) => {
    setCurrentPath(path)
    setPreview(null)
  }

  const handleDelete = async (entry: FileEntry) => {
    if (!confirm(`Delete ${entry.name}?`)) return
    try {
      await api.delete("/api/files", { params: { path: entry.path } })
      toast.success("File deleted")
      fetchFiles(currentPath)
    } catch {
      toast.error("Failed to delete file")
    }
  }

  const handlePreview = (entry: FileEntry) => {
    if (/\.(jpg|jpeg|png|webp|gif|svg)$/i.test(entry.name)) {
      setPreview(`/uploads/${entry.path}`)
    } else {
      window.open(`/uploads/${entry.path}`, "_blank")
    }
  }

  // Breadcrumb
  const pathParts = currentPath ? currentPath.split("/") : []

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-cyber-green">File Manager</h2>
        <Button variant="ghost" size="sm" onClick={() => fetchFiles(currentPath)}>
          <RefreshCw size={14} className="mr-1.5" /> Refresh
        </Button>
      </div>

      {/* Breadcrumb */}
      <Card className="mb-4">
        <div className="flex items-center gap-1 text-xs">
          <button
            onClick={() => navigateTo("")}
            className="text-cyber-green hover:text-cyber-green-dim cursor-pointer flex items-center gap-1"
          >
            <Home size={12} /> uploads
          </button>
          {pathParts.map((part, i) => (
            <span key={i} className="flex items-center gap-1">
              <ChevronRight size={12} className="text-cyber-green-muted" />
              <button
                onClick={() => navigateTo(pathParts.slice(0, i + 1).join("/"))}
                className="text-cyber-green hover:text-cyber-green-dim cursor-pointer"
              >
                {part}
              </button>
            </span>
          ))}
        </div>
      </Card>

      <div className="flex gap-4">
        {/* File list */}
        <div className="flex-1">
          {loading ? (
            <Card className="animate-pulse"><div className="h-40 bg-bg-hover rounded" /></Card>
          ) : files.length === 0 ? (
            <Card>
              <p className="text-cyber-green-muted text-sm text-center py-8">
                Empty directory
              </p>
            </Card>
          ) : (
            <Card className="p-0 overflow-hidden">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-xs text-cyber-green-muted uppercase">
                    <th className="text-left px-4 py-2.5">Name</th>
                    <th className="text-right px-4 py-2.5 w-24">Size</th>
                    <th className="text-right px-4 py-2.5 w-40">Modified</th>
                    <th className="text-right px-4 py-2.5 w-20">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {files.map((entry) => (
                    <tr
                      key={entry.path}
                      className="border-b border-border/50 hover:bg-bg-hover transition-colors"
                    >
                      <td className="px-4 py-2.5">
                        <button
                          onClick={() =>
                            entry.isDir ? navigateTo(entry.path) : handlePreview(entry)
                          }
                          className="flex items-center gap-2 text-cyber-green hover:text-cyber-green-dim cursor-pointer"
                        >
                          {fileIcon(entry)}
                          <span>{entry.name}</span>
                          {entry.isDir && <Badge variant="muted">DIR</Badge>}
                        </button>
                      </td>
                      <td className="text-right px-4 py-2.5 text-cyber-green-muted text-xs">
                        {entry.isDir ? "--" : formatSize(entry.size)}
                      </td>
                      <td className="text-right px-4 py-2.5 text-cyber-green-muted text-xs">
                        {new Date(entry.modTime).toLocaleString()}
                      </td>
                      <td className="text-right px-4 py-2.5">
                        <div className="flex justify-end gap-1">
                          {!entry.isDir && (
                            <a
                              href={`/uploads/${entry.path}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="text-cyber-green-muted hover:text-cyber-green p-1"
                            >
                              <ExternalLink size={13} />
                            </a>
                          )}
                          {isAdmin && !entry.isDir && (
                            <button
                              onClick={() => handleDelete(entry)}
                              className="text-cyber-danger/50 hover:text-cyber-danger p-1 cursor-pointer"
                            >
                              <Trash2 size={13} />
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          )}
        </div>

        {/* Preview panel */}
        {preview && (
          <div className="w-72 shrink-0">
            <Card>
              <h3 className="text-xs text-cyber-green-dim uppercase tracking-wider mb-3">Preview</h3>
              <img
                src={preview}
                alt="Preview"
                className="w-full rounded border border-border bg-bg-hover"
                onError={() => setPreview(null)}
              />
              <button
                onClick={() => setPreview(null)}
                className="text-xs text-cyber-green-muted hover:text-cyber-green mt-2 cursor-pointer"
              >
                Close preview
              </button>
            </Card>
          </div>
        )}
      </div>
    </div>
  )
}
