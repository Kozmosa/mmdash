"use client"

import { Checkbox } from "@/components/ui/checkbox"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Pencil, Trash2 } from "lucide-react"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  source: string
  user_id: string
  volume?: string
  issue?: string
  pages?: string
  doi?: string
  url?: string
  abstract?: string
  bibtex_key?: string
  bibtex_type?: string
  created_at?: string
}

interface Props {
  citations: Citation[]
  loading: boolean
  selectedIds: Set<string>
  onSelectChange: (ids: Set<string>) => void
  onEdit: (c: Citation) => void
  onDelete: (id: string) => void
}

export function CitationTable({ citations, loading, selectedIds, onSelectChange, onEdit, onDelete }: Props) {
  const allSelected = citations.length > 0 && citations.every((c) => selectedIds.has(c.id))
  const someSelected = citations.some((c) => selectedIds.has(c.id)) && !allSelected

  const toggleAll = () => {
    if (allSelected) {
      onSelectChange(new Set())
    } else {
      onSelectChange(new Set(citations.map((c) => c.id)))
    }
  }

  const toggleOne = (id: string) => {
    const next = new Set(selectedIds)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    onSelectChange(next)
  }

  if (loading) {
    return <div className="text-muted-foreground py-8 text-center">加载中...</div>
  }

  if (citations.length === 0) {
    return <div className="text-muted-foreground py-8 text-center">暂无引文</div>
  }

  return (
    <div className="border rounded-md">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-[40px]">
              <Checkbox
                checked={allSelected}
                data-state={someSelected ? "indeterminate" : allSelected ? "checked" : "unchecked"}
                onCheckedChange={toggleAll}
              />
            </TableHead>
            <TableHead>标题</TableHead>
            <TableHead>作者</TableHead>
            <TableHead>刊名</TableHead>
            <TableHead className="w-[80px]">年份</TableHead>
            <TableHead className="w-[80px]">来源</TableHead>
            <TableHead className="w-[100px]">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {citations.map((c) => (
            <TableRow key={c.id}>
              <TableCell>
                <Checkbox checked={selectedIds.has(c.id)} onCheckedChange={() => toggleOne(c.id)} />
              </TableCell>
              <TableCell className="font-medium max-w-[300px] truncate">{c.title}</TableCell>
              <TableCell className="max-w-[200px] truncate">{c.authors}</TableCell>
              <TableCell className="max-w-[150px] truncate">{c.journal}</TableCell>
              <TableCell>{c.year}</TableCell>
              <TableCell>
                <Badge variant={c.source === "zotero" ? "secondary" : "outline"}>
                  {c.source === "zotero" ? "Z" : "M"}
                </Badge>
              </TableCell>
              <TableCell>
                <div className="flex gap-1">
                  <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => onEdit(c)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" onClick={() => onDelete(c.id)}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
