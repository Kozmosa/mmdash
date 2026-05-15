"use client"

import { useState, useEffect } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import api from "@/lib/api"

interface Props {
  projectId: string
  onConfigChange: () => void
}

export function ZoteroConfigPanel({ projectId, onConfigChange }: Props) {
  const [config, setConfig] = useState<any>(null)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ api_key: "", library_id: "", library_type: "user" })

  const fetchConfig = async () => {
    try {
      const res = await api.get(`/references/${projectId}/zotero-config`)
      setConfig(res.data)
    } catch (e) {
      setConfig(null)
    }
  }

  useEffect(() => {
    fetchConfig()
  }, [projectId])

  const handleSave = async () => {
    try {
      const data = new URLSearchParams()
      data.append("api_key", form.api_key)
      data.append("library_id", form.library_id)
      data.append("library_type", form.library_type)
      await api.post(`/references/${projectId}/zotero-config`, data)
      setShowForm(false)
      fetchConfig()
      onConfigChange()
    } catch (e) {
      alert("保存失败")
    }
  }

  const handleDelete = async () => {
    if (!confirm("确定删除 Zotero 配置？")) return
    try {
      await api.delete(`/references/${projectId}/zotero-config`)
      setConfig(null)
      onConfigChange()
    } catch (e) {
      alert("删除失败")
    }
  }

  if (!config && !showForm) {
    return (
      <Button variant="outline" size="sm" onClick={() => setShowForm(true)}>
        连接 Zotero
      </Button>
    )
  }

  if (showForm) {
    return (
      <div className="border rounded-lg p-4 space-y-3 bg-card">
        <h4 className="text-sm font-medium">Zotero 配置</h4>
        <div>
          <Label className="text-xs">API Key</Label>
          <Input type="password" value={form.api_key} onChange={(e) => setForm({ ...form, api_key: e.target.value })} placeholder="从 zotero.org/settings/keys 获取" />
        </div>
        <div>
          <Label className="text-xs">Library ID</Label>
          <Input value={form.library_id} onChange={(e) => setForm({ ...form, library_id: e.target.value })} placeholder="User ID 或 Group ID" />
        </div>
        <div>
          <Label className="text-xs">Library Type</Label>
          <Select value={form.library_type} onValueChange={(v) => setForm({ ...form, library_type: v })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="user">User</SelectItem>
              <SelectItem value="group">Group</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="flex gap-2">
          <Button size="sm" onClick={handleSave}>保存</Button>
          <Button size="sm" variant="outline" onClick={() => setShowForm(false)}>取消</Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <span>Zotero: {config?.library_id} ({config?.library_type})</span>
      <Button variant="ghost" size="sm" onClick={() => { setForm({ api_key: "", library_id: config?.library_id || "", library_type: config?.library_type || "user" }); setShowForm(true) }}>编辑</Button>
      <Button variant="ghost" size="sm" className="text-destructive" onClick={handleDelete}>删除</Button>
    </div>
  )
}
