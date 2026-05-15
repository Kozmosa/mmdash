"use client"

import { useState, useEffect } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import api from "@/lib/api"

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
}

interface Props {
  open: boolean
  onClose: () => void
  projectId: string
  citation: Citation | null
  onSuccess: () => void
}

const BIBTEX_TYPES = ["article", "book", "incollection", "inproceedings", "phdthesis", "techreport", "misc"]

export function CitationForm({ open, onClose, projectId, citation, onSuccess }: Props) {
  const [form, setForm] = useState({
    title: "",
    authors: "",
    journal: "",
    year: "",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    url: "",
    abstract: "",
    bibtex_key: "",
    bibtex_type: "article",
  })
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (citation) {
      setForm({
        title: citation.title || "",
        authors: citation.authors || "",
        journal: citation.journal || "",
        year: citation.year?.toString() || "",
        volume: citation.volume || "",
        issue: citation.issue || "",
        pages: citation.pages || "",
        doi: citation.doi || "",
        url: citation.url || "",
        abstract: citation.abstract || "",
        bibtex_key: citation.bibtex_key || "",
        bibtex_type: citation.bibtex_type || "article",
      })
    } else {
      setForm({
        title: "", authors: "", journal: "", year: "", volume: "",
        issue: "", pages: "", doi: "", url: "", abstract: "",
        bibtex_key: "", bibtex_type: "article",
      })
    }
  }, [citation, open])

  const update = (key: string, value: string) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    try {
      const data = new URLSearchParams()
      Object.entries(form).forEach(([k, v]) => {
        if (v) data.append(k, v)
      })

      if (citation) {
        await api.put(`/references/${projectId}/${citation.id}`, data)
      } else {
        await api.post(`/references/${projectId}`, data)
      }
      onSuccess()
    } catch (e) {
      console.error(e)
      alert("保存失败")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{citation ? "编辑引文" : "添加引文"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <Label>标题 *</Label>
              <Input value={form.title} onChange={(e) => update("title", e.target.value)} required />
            </div>
            <div className="col-span-2">
              <Label>作者</Label>
              <Input value={form.authors} onChange={(e) => update("authors", e.target.value)} placeholder="Zhang, S. and Li, M." />
            </div>
            <div>
              <Label>刊名</Label>
              <Input value={form.journal} onChange={(e) => update("journal", e.target.value)} />
            </div>
            <div>
              <Label>年份</Label>
              <Input type="number" value={form.year} onChange={(e) => update("year", e.target.value)} />
            </div>
            <div>
              <Label>卷号</Label>
              <Input value={form.volume} onChange={(e) => update("volume", e.target.value)} />
            </div>
            <div>
              <Label>期号</Label>
              <Input value={form.issue} onChange={(e) => update("issue", e.target.value)} />
            </div>
            <div>
              <Label>页码</Label>
              <Input value={form.pages} onChange={(e) => update("pages", e.target.value)} />
            </div>
            <div>
              <Label>DOI</Label>
              <Input value={form.doi} onChange={(e) => update("doi", e.target.value)} />
            </div>
            <div className="col-span-2">
              <Label>URL</Label>
              <Input value={form.url} onChange={(e) => update("url", e.target.value)} />
            </div>
            <div>
              <Label>BibTeX Key</Label>
              <Input value={form.bibtex_key} onChange={(e) => update("bibtex_key", e.target.value)} />
            </div>
            <div>
              <Label>BibTeX Type</Label>
              <Select value={form.bibtex_type} onValueChange={(v) => update("bibtex_type", v)}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {BIBTEX_TYPES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div>
            <Label>摘要</Label>
            <Textarea value={form.abstract} onChange={(e) => update("abstract", e.target.value)} rows={4} />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>取消</Button>
            <Button type="submit" disabled={submitting}>{submitting ? "保存中..." : "保存"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
