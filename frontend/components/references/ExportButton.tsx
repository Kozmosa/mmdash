"use client"

import { Button } from "@/components/ui/button"
import { Download } from "lucide-react"
import api from "@/lib/api"

interface Props {
  projectId: string
  selectedIds: string[]
}

export function ExportButton({ projectId, selectedIds }: Props) {
  const handleExport = async (exportAll: boolean) => {
    try {
      const payload = exportAll ? {} : { ids: selectedIds }
      const res = await api.post(`/references/${projectId}/export`, payload, {
        responseType: "text",
      })

      const blob = new Blob([res.data], { type: "text/plain;charset=utf-8" })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = `references_${projectId}.bib`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (e) {
      console.error(e)
      alert("导出失败")
    }
  }

  return (
    <div className="flex gap-1">
      {selectedIds.length > 0 && (
        <Button variant="outline" size="sm" onClick={() => handleExport(false)}>
          <Download className="mr-1 h-4 w-4" />
          导出选中 ({selectedIds.length})
        </Button>
      )}
      <Button variant="outline" size="sm" onClick={() => handleExport(true)}>
        <Download className="mr-1 h-4 w-4" />
        导出全部
      </Button>
    </div>
  )
}
