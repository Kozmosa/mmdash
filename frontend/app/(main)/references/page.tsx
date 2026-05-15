"use client"

import { useEffect, useState } from "react"
import { useAuthStore } from "@/stores/auth"
import api from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Plus } from "lucide-react"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  volume: string
  issue: string
  pages: string
  doi: string
  url: string
  abstract: string
  bibtex_key: string
  bibtex_type: string
  source: string
  user_id: string
  created_at: string
}

export default function ReferencesPage() {
  const [citations, setCitations] = useState<Citation[]>([])
  const [loading, setLoading] = useState(true)
  const [currentProjectId, setCurrentProjectId] = useState("")

  useEffect(() => {
    const pid = localStorage.getItem("current_project_id")
    if (pid) setCurrentProjectId(pid)
  }, [])

  const fetchCitations = async () => {
    if (!currentProjectId) return
    setLoading(true)
    try {
      const res = await api.get(`/references/${currentProjectId}`)
      setCitations(res.data.items)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchCitations()
  }, [currentProjectId])

  if (!currentProjectId) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-muted-foreground">请先在主页选择一个项目</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">引文管理</h2>
        <Button size="sm">
          <Plus className="mr-1 h-4 w-4" />
          添加引文
        </Button>
      </div>

      {loading ? (
        <p className="text-muted-foreground">加载中...</p>
      ) : citations.length === 0 ? (
        <p className="text-muted-foreground">暂无引文</p>
      ) : (
        <div className="border rounded-md">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="px-4 py-2 text-left">标题</th>
                <th className="px-4 py-2 text-left">作者</th>
                <th className="px-4 py-2 text-left">刊名</th>
                <th className="px-4 py-2 text-left">年份</th>
              </tr>
            </thead>
            <tbody>
              {citations.map((c) => (
                <tr key={c.id} className="border-b">
                  <td className="px-4 py-2">{c.title}</td>
                  <td className="px-4 py-2">{c.authors}</td>
                  <td className="px-4 py-2">{c.journal}</td>
                  <td className="px-4 py-2">{c.year}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
