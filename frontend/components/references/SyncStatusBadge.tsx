"use client"

import { Badge } from "@/components/ui/badge"
import { Loader2, CheckCircle, AlertCircle } from "lucide-react"

interface SyncStatus {
  last_sync_at: string | null
  last_sync_status: string
  last_sync_error: string | null
}

interface Props {
  status: SyncStatus | null
}

export function SyncStatusBadge({ status }: Props) {
  if (!status) return null

  const formatTime = (ts: string | null) => {
    if (!ts) return "从未同步"
    const date = new Date(ts)
    return date.toLocaleString("zh-CN")
  }

  if (status.last_sync_status === "syncing") {
    return (
      <Badge variant="outline" className="gap-1">
        <Loader2 className="h-3 w-3 animate-spin" />
        同步中...
      </Badge>
    )
  }

  if (status.last_sync_status === "error") {
    return (
      <Badge variant="destructive" className="gap-1" title={status.last_sync_error || ""}>
        <AlertCircle className="h-3 w-3" />
        同步失败
      </Badge>
    )
  }

  return (
    <Badge variant="outline" className="gap-1 text-muted-foreground">
      <CheckCircle className="h-3 w-3" />
      上次同步: {formatTime(status.last_sync_at)}
    </Badge>
  )
}
