import React, { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Loader2, Pencil } from "lucide-react"
import { llmApi } from "@/lib/api"

interface PromptSettingsProps {
  teamId?: string
  teamName?: string
  canManage: boolean
}

const PROMPT_KEYS = [
  { key: "symbols", label: "符号分析" },
  { key: "structure", label: "结构分析" },
  { key: "formula", label: "公式解释" },
  { key: "errors", label: "纠错检查" },
] as const

export function PromptSettings({ teamId, teamName, canManage }: PromptSettingsProps) {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [success, setSuccess] = useState("")
  const [prompts, setPrompts] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!teamId || !canManage) {
      setPrompts({})
      return
    }
    void loadPrompts(teamId)
  }, [teamId, canManage])

  async function loadPrompts(id: string) {
    setLoading(true)
    setError("")
    setSuccess("")
    try {
      const res = await llmApi.getPromptSettings(id)
      setPrompts(res.prompts || {})
    } catch (err: any) {
      setError(err.response?.data?.detail || "加载 prompt 失败")
    } finally {
      setLoading(false)
    }
  }

  async function handleSave() {
    if (!teamId) return
    setSaving(true)
    setError("")
    setSuccess("")
    try {
      const res = await llmApi.updatePromptSettings(teamId, prompts)
      setPrompts(res.prompts || {})
      setSuccess("Prompt 设置已保存")
    } catch (err: any) {
      setError(err.response?.data?.detail || "保存 prompt 失败")
    } finally {
      setSaving(false)
    }
  }

  if (!teamId || !canManage) {
    return null
  }

  return (
    <Card className="w-full">
      <CardHeader>
        <CardTitle>Prompt 设置</CardTitle>
        <CardDescription>
          为 {teamName || "当前团队"} 配置各个 LLM 调用场景的默认提示词
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <div className="flex items-center text-sm text-muted-foreground">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            加载中...
          </div>
        ) : (
          <div className="space-y-4">
            {PROMPT_KEYS.map((item) => (
              <div key={item.key} className="space-y-2">
                <label className="text-sm font-medium">{item.label}</label>
                <Textarea
                  rows={8}
                  value={prompts[item.key] || ""}
                  onChange={(e) => setPrompts((prev) => ({ ...prev, [item.key]: e.target.value }))}
                  placeholder={`输入 ${item.label} 的 prompt`}
                />
              </div>
            ))}

            <Button onClick={handleSave} disabled={saving} className="w-full sm:w-auto">
              {saving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  保存中...
                </>
              ) : (
                <>
                  <Pencil className="mr-2 h-4 w-4" />
                  保存 Prompt 设置
                </>
              )}
            </Button>
          </div>
        )}

        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        {success && (
          <Alert>
            <AlertDescription>{success}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
