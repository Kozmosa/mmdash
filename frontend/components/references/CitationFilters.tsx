"use client"

import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Search } from "lucide-react"

interface Filters {
  q: string
  year_from: string
  year_to: string
  source: string
  sort_by: string
  sort_order: string
}

interface Props {
  filters: Filters
  onChange: (filters: Filters) => void
}

export function CitationFilters({ filters, onChange }: Props) {
  const update = (key: keyof Filters, value: string) => {
    onChange({ ...filters, [key]: value })
  }

  return (
    <div className="flex items-center gap-2 flex-wrap">
      <div className="relative">
        <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="搜索标题、作者、刊名..."
          className="pl-8 w-[280px]"
          value={filters.q}
          onChange={(e) => update("q", e.target.value)}
        />
      </div>
      <Input
        placeholder="年份起"
        className="w-[80px]"
        value={filters.year_from}
        onChange={(e) => update("year_from", e.target.value)}
      />
      <Input
        placeholder="年份止"
        className="w-[80px]"
        value={filters.year_to}
        onChange={(e) => update("year_to", e.target.value)}
      />
      <Select value={filters.source} onValueChange={(v) => update("source", v)}>
        <SelectTrigger className="w-[120px]">
          <SelectValue placeholder="来源" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部</SelectItem>
          <SelectItem value="manual">手动</SelectItem>
          <SelectItem value="zotero">Zotero</SelectItem>
        </SelectContent>
      </Select>
      <Select value={filters.sort_by} onValueChange={(v) => update("sort_by", v)}>
        <SelectTrigger className="w-[120px]">
          <SelectValue placeholder="排序" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="created_at">创建时间</SelectItem>
          <SelectItem value="title">标题</SelectItem>
          <SelectItem value="year">年份</SelectItem>
        </SelectContent>
      </Select>
      <Select value={filters.sort_order} onValueChange={(v) => update("sort_order", v)}>
        <SelectTrigger className="w-[100px]">
          <SelectValue placeholder="顺序" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="desc">降序</SelectItem>
          <SelectItem value="asc">升序</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}
