"use client"

import { useEffect, useState } from "react"
import { useAuthStore } from "@/stores/auth"
import api from "@/lib/api"
import { CitationTable } from "@/components/references/CitationTable"
import { CitationFilters } from "@/components/references/CitationFilters"
import { CitationForm } from "@/components/references/CitationForm"
import { ZoteroConfigPanel } from "@/components/references/ZoteroConfigPanel"
import { SyncStatusBadge } from "@/components/references/SyncStatusBadge"
import { ExportButton } from "@/components/references/ExportButton"
import { Button } from "@/components/ui/button"
import { Plus } from "lucide-react"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  volume?: string
  issue?: string
  pages?: string
  doi?: string
  url?: string
  abstract?: string
  bibtex_key?: string
  bibtex_type?: string
  source: string
  user_id: string
  created_at?: string
}

interface SyncStatus {
  last_sync_at: string | null
  last_sync_status: string
  last_sync_error: string | null
}

export default function ReferencesPage() {
  const [citations, setCitations] = useState<Citation[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [showForm, setShowForm] = useState(false)
  const [editingCitation, setEditingCitation] = useState<Citation | null>(null)
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null)
  const [filters, setFilters] = useState({ q: "", year_from: "", year_to: "", source: "", sort_by: "created_at", sort_order: "desc" })
  const [currentProjectId, setCurrentProjectId] = useState("")

  useEffect(() => {
    const pid = localStorage.getItem("current_project_id")
    if (pid) setCurrentProjectId(pid)
  }, [])

  const fetchCitations = async () => {
    if (!currentProjectId) return
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filters.q) params.append("q", filters.q)
      if (filters.year_from) params.append("year_from", filters.year_from)
      if (filters.year_to) params.append("year_to", filters.year_to)
      if (filters.source) params.append("source", filters.source)
      params.append("sort_by", filters.sort_by)
      params.append("sort_order", filters.sort_order)

      const res = await api.get(`/references/${currentProjectId}?${params}`)
      setCitations(res.data.items)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  const fetchSyncStatus = async () => {
    if (!currentProjectId) return
    try {
      const res = await api.get(`/references/${currentProjectId}/sync-status`)
      setSyncStatus(res.data)
    } catch (e) {
      setSyncStatus(null)
    }
  }

  useEffect(() => {
    fetchCitations()
  }, [currentProjectId, filters])

  useEffect(() => {
    fetchSyncStatus()
  }, [currentProjectId])

  const handleDelete = async (id: string) => {
    if (!confirm("确定删除这条引文？")) return
    try {
      await api.delete(`/references/${currentProjectId}/${id}`)
      fetchCitations()
    } catch (e) {
      console.error(e)
    }
  }

  const handleSync = async () => {
    try {
      await api.post(`/references/${currentProjectId}/sync`)
      setTimeout(() => {
        fetchSyncStatus()
        fetchCitations()
      }, 2000)
    } catch (e) {
      console.error(e)
    }
  }

  if (!currentProjectId) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-muted-foreground">请先在主页选择一个项目</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <CitationFilters filters={filters} onChange={setFilters} />
        <div className="flex items-center gap-2">
          <SyncStatusBadge status={syncStatus} />
          <Button variant="outline" size="sm" onClick={handleSync}>同步</Button>
          <ExportButton projectId={currentProjectId} selectedIds={Array.from(selectedIds)} />
          <Button size="sm" onClick={() => { setEditingCitation(null); setShowForm(true) }}>
            <Plus className="mr-1 h-4 w-4" />添加引文
          </Button>
        </div>
      </div>

      <ZoteroConfigPanel projectId={currentProjectId} onConfigChange={fetchSyncStatus} />

      <CitationTable
        citations={citations}
        loading={loading}
        selectedIds={selectedIds}
        onSelectChange={setSelectedIds}
        onEdit={(c) => { setEditingCitation(c); setShowForm(true) }}
        onDelete={handleDelete}
      />

      <CitationForm
        open={showForm}
        onClose={() => setShowForm(false)}
        projectId={currentProjectId}
        citation={editingCitation}
        onSuccess={() => { setShowForm(false); fetchCitations() }}
      />
    </div>
  )
}
