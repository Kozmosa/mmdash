"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import {
  User,
  Users,
  Database,
  Copy,
  Check,
  Pencil,
  Trash2,
  LogOut,
  Eye,
  Plus,
  KeyRound,
  Save,
  AlertTriangle,
  Shield,
  Server,
  BookOpen,
  Zap,
} from "lucide-react"
import { ModelSelector } from "@/components/llm/ModelSelector"
import { PromptSettings } from "@/components/llm/PromptSettings"
import api from "@/lib/api"
import { useAuthStore } from "@/stores/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { Alert, AlertDescription } from "@/components/ui/alert"

interface Team {
  id: string
  name: string
  owner_id: string
  invite_code: string
  created_at: string
}

interface TeamMember {
  id: string
  team_id: string
  user_id: string
  role: string
  joined_at: string
  user_email?: string | null
  user_name?: string | null
}

export default function SettingsPage() {
  const router = useRouter()
  const user = useAuthStore((s) => s.user)
  const setAuth = useAuthStore((s) => s.setAuth)
  const logout = useAuthStore((s) => s.logout)

  // ─── Profile State ─────────────────────────────────────────────
  const [username, setUsername] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [profileError, setProfileError] = useState("")
  const [profileSuccess, setProfileSuccess] = useState("")
  const [savingProfile, setSavingProfile] = useState(false)

  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [passwordError, setPasswordError] = useState("")
  const [passwordSuccess, setPasswordSuccess] = useState("")
  const [savingPassword, setSavingPassword] = useState(false)

  // ─── Teams State ───────────────────────────────────────────────
  const [teams, setTeams] = useState<Team[]>([])
  const [loadingTeams, setLoadingTeams] = useState(false)
  const [teamError, setTeamError] = useState("")

  const [newTeamName, setNewTeamName] = useState("")
  const [joinCode, setJoinCode] = useState("")
  const [creatingTeam, setCreatingTeam] = useState(false)
  const [joiningTeam, setJoiningTeam] = useState(false)

  const [renameTeamId, setRenameTeamId] = useState<string | null>(null)
  const [renameTeamName, setRenameTeamName] = useState("")
  const [renaming, setRenaming] = useState(false)

  const [membersTeamId, setMembersTeamId] = useState<string | null>(null)
  const [members, setMembers] = useState<TeamMember[]>([])
  const [loadingMembers, setLoadingMembers] = useState(false)
  const [llmTeamId, setLlmTeamId] = useState<string>("")
  const [llmTeamRole, setLlmTeamRole] = useState<string>("")

  // ─── Provider State ────────────────────────────────────────────
  const [currentProvider, setCurrentProvider] = useState<string>("local_file")
  const [providerLoading, setProviderLoading] = useState(false)
  const [providerError, setProviderError] = useState("")
  const [providerSuccess, setProviderSuccess] = useState("")

  // ─── Clipboard ─────────────────────────────────────────────────
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const currentMembersTeamRole = members.find((m) => m.user_id === user?.id)?.role
  const canManageMembers = currentMembersTeamRole === "owner" || currentMembersTeamRole === "admin"

  useEffect(() => {
    if (user) {
      setUsername(user.username || "")
      setDisplayName(user.display_name || "")
    }
    loadTeams()
    loadProvider()
  }, [user])

  useEffect(() => {
    if (!llmTeamId && teams.length > 0) {
      setLlmTeamId(teams[0].id)
    }
  }, [teams, llmTeamId])

  useEffect(() => {
    if (!llmTeamId || !user) return
    void loadLlmTeamRole(llmTeamId)
  }, [llmTeamId, user])

  // ─── Profile Handlers ──────────────────────────────────────────
  const handleSaveProfile = async () => {
    setProfileError("")
    setProfileSuccess("")
    setSavingProfile(true)
    try {
      const res = await api.put("/auth/me", {
        username: username || undefined,
        display_name: displayName || undefined,
      })
      const token = localStorage.getItem("token") || ""
      setAuth(res.data, token)
      setProfileSuccess("个人资料已保存")
    } catch (err: any) {
      setProfileError(err.response?.data?.detail || "保存失败")
    } finally {
      setSavingProfile(false)
    }
  }

  const handleChangePassword = async () => {
    setPasswordError("")
    setPasswordSuccess("")
    if (newPassword !== confirmPassword) {
      setPasswordError("两次输入的新密码不一致")
      return
    }
    if (newPassword.length < 6) {
      setPasswordError("新密码至少需要6位")
      return
    }
    setSavingPassword(true)
    try {
      await api.post("/auth/change-password", {
        current_password: currentPassword,
        new_password: newPassword,
      })
      setPasswordSuccess("密码已修改")
      setCurrentPassword("")
      setNewPassword("")
      setConfirmPassword("")
    } catch (err: any) {
      setPasswordError(err.response?.data?.detail || "修改密码失败")
    } finally {
      setSavingPassword(false)
    }
  }

  // ─── Teams Handlers ────────────────────────────────────────────
  const loadTeams = async () => {
    setLoadingTeams(true)
    try {
      const res = await api.get("/teams")
      setTeams(res.data)
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "加载团队失败")
    } finally {
      setLoadingTeams(false)
    }
  }

  const handleCreateTeam = async () => {
    if (!newTeamName.trim()) return
    setCreatingTeam(true)
    try {
      await api.post("/teams", { name: newTeamName.trim() })
      setNewTeamName("")
      await loadTeams()
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "创建团队失败")
    } finally {
      setCreatingTeam(false)
    }
  }

  const handleJoinTeam = async () => {
    if (!joinCode.trim()) return
    setJoiningTeam(true)
    try {
      await api.post("/teams/join", { invite_code: joinCode.trim().toUpperCase() })
      setJoinCode("")
      await loadTeams()
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "加入团队失败")
    } finally {
      setJoiningTeam(false)
    }
  }

  const handleRenameTeam = async () => {
    if (!renameTeamId || !renameTeamName.trim()) return
    setRenaming(true)
    try {
      await api.put(`/teams/${renameTeamId}`, { name: renameTeamName.trim() })
      setRenameTeamId(null)
      setRenameTeamName("")
      await loadTeams()
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "重命名失败")
    } finally {
      setRenaming(false)
    }
  }

  const handleDeleteTeam = async (teamId: string) => {
    try {
      await api.delete(`/teams/${teamId}`)
      await loadTeams()
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "删除团队失败")
    }
  }

  const handleLeaveTeam = async (teamId: string) => {
    try {
      await api.post(`/teams/${teamId}/leave`)
      await loadTeams()
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "退出团队失败")
    }
  }

  const openMembers = async (teamId: string) => {
    setMembersTeamId(teamId)
    setLoadingMembers(true)
    try {
      const res = await api.get(`/teams/${teamId}/members`)
      setMembers(res.data)
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "加载成员失败")
    } finally {
      setLoadingMembers(false)
    }
  }

  const handleUpdateMemberRole = async (teamId: string, userId: string, role: string) => {
    try {
      await api.put(`/teams/${teamId}/members/${userId}/role`, null, { params: { role } })
      if (membersTeamId === teamId) {
        await openMembers(teamId)
      }
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "更新成员角色失败")
    }
  }

  const handleRemoveMember = async (teamId: string, userId: string) => {
    try {
      await api.delete(`/teams/${teamId}/members/${userId}`)
      if (membersTeamId === teamId) {
        await openMembers(teamId)
      }
    } catch (err: any) {
      setTeamError(err.response?.data?.detail || "移除成员失败")
    }
  }

  const loadLlmTeamRole = async (teamId: string) => {
    try {
      const res = await api.get(`/teams/${teamId}/members`)
      const current = res.data.find((member: TeamMember) => member.user_id === user?.id)
      setLlmTeamRole(current?.role || "")
    } catch {
      setLlmTeamRole("")
    }
  }

  const copyInviteCode = (code: string, teamId: string) => {
    navigator.clipboard.writeText(code)
    setCopiedId(teamId)
    setTimeout(() => setCopiedId(null), 2000)
  }

  // ─── Provider Handlers ─────────────────────────────────────────
  const loadProvider = async () => {
    try {
      const res = await api.get("/auth/me")
      // Provider info is not directly in user; we infer from binding or use default
      // We'll try to get provider url to see if there's a binding
      try {
        const urlRes = await api.get("/auth/provider/url")
        // If this succeeds, we have a binding. We can't tell type from url alone.
        // Default to notion since url endpoint is notion-specific in practice.
        setCurrentProvider("notion")
      } catch {
        setCurrentProvider("local_file")
      }
    } catch {
      // ignore
    }
  }

  const handleSwitchProvider = async (providerType: string) => {
    setProviderError("")
    setProviderSuccess("")
    if (providerType === currentProvider) return
    setProviderLoading(true)
    try {
      if (providerType === "notion") {
        // Need OAuth first
        const res = await api.get("/auth/provider/url")
        if (res.data.auth_url) {
          window.location.href = res.data.auth_url
          return
        }
      }
      await api.post("/auth/provider/switch", { provider_type: providerType })
      setCurrentProvider(providerType)
      setProviderSuccess(`已切换到 ${providerType === "local_file" ? "内置文档服务器" : "Notion"}`)
    } catch (err: any) {
      setProviderError(err.response?.data?.detail || "切换失败")
    } finally {
      setProviderLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <Tabs defaultValue="profile" className="w-full">
        <TabsList className="grid w-full grid-cols-4 max-w-2xl">
          <TabsTrigger value="profile">
            <User className="mr-2 size-4" />
            个人资料
          </TabsTrigger>
          <TabsTrigger value="teams">
            <Users className="mr-2 size-4" />
            团队管理
          </TabsTrigger>
          <TabsTrigger value="provider">
            <Database className="mr-2 size-4" />
            文档后端
          </TabsTrigger>
          <TabsTrigger value="llm">
            <Zap className="mr-2 size-4" />
            LLM 模型
          </TabsTrigger>
        </TabsList>

        {/* ─── Profile Tab ───────────────────────────────────────── */}
        <TabsContent value="profile" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>个人资料</CardTitle>
              <CardDescription>管理您的用户名和昵称</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {profileError && (
                <Alert variant="destructive">
                  <AlertDescription>{profileError}</AlertDescription>
                </Alert>
              )}
              {profileSuccess && (
                <Alert>
                  <AlertDescription>{profileSuccess}</AlertDescription>
                </Alert>
              )}
              <div className="space-y-2">
                <Label htmlFor="email">邮箱</Label>
                <Input id="email" value={user?.email || ""} disabled />
              </div>
              <div className="space-y-2">
                <Label htmlFor="username">用户名</Label>
                <Input
                  id="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="用户名"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="displayName">昵称</Label>
                <Input
                  id="displayName"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  placeholder="昵称"
                />
              </div>
              <Button onClick={handleSaveProfile} disabled={savingProfile}>
                <Save className="mr-2 size-4" />
                {savingProfile ? "保存中..." : "保存资料"}
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>修改密码</CardTitle>
              <CardDescription>更新您的登录密码</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {passwordError && (
                <Alert variant="destructive">
                  <AlertDescription>{passwordError}</AlertDescription>
                </Alert>
              )}
              {passwordSuccess && (
                <Alert>
                  <AlertDescription>{passwordSuccess}</AlertDescription>
                </Alert>
              )}
              <div className="space-y-2">
                <Label htmlFor="currentPassword">当前密码</Label>
                <Input
                  id="currentPassword"
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  placeholder="当前密码"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="newPassword">新密码</Label>
                <Input
                  id="newPassword"
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  placeholder="新密码"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="confirmPassword">确认新密码</Label>
                <Input
                  id="confirmPassword"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="再次输入新密码"
                />
              </div>
              <Button onClick={handleChangePassword} disabled={savingPassword}>
                <KeyRound className="mr-2 size-4" />
                {savingPassword ? "修改中..." : "修改密码"}
              </Button>
            </CardContent>
          </Card>
        </TabsContent>

        {/* ─── Teams Tab ─────────────────────────────────────────── */}
        <TabsContent value="teams" className="space-y-6">
          {teamError && (
            <Alert variant="destructive">
              <AlertDescription>{teamError}</AlertDescription>
            </Alert>
          )}

          <div className="grid gap-4 md:grid-cols-2">
            {/* Create Team */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Plus className="size-4" />
                  创建团队
                </CardTitle>
                <CardDescription>创建一个新的团队并获取邀请码</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="newTeamName">团队名称</Label>
                  <Input
                    id="newTeamName"
                    value={newTeamName}
                    onChange={(e) => setNewTeamName(e.target.value)}
                    placeholder="输入团队名称"
                  />
                </div>
                <Button onClick={handleCreateTeam} disabled={creatingTeam || !newTeamName.trim()}>
                  {creatingTeam ? "创建中..." : "创建团队"}
                </Button>
              </CardContent>
            </Card>

            {/* Join Team */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <LogOut className="size-4" />
                  加入团队
                </CardTitle>
                <CardDescription>输入邀请码加入已有团队</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="joinCode">邀请码</Label>
                  <Input
                    id="joinCode"
                    value={joinCode}
                    onChange={(e) => setJoinCode(e.target.value.toUpperCase())}
                    placeholder="6位邀请码"
                    maxLength={6}
                  />
                </div>
                <Button onClick={handleJoinTeam} disabled={joiningTeam || !joinCode.trim()}>
                  {joiningTeam ? "加入中..." : "加入团队"}
                </Button>
              </CardContent>
            </Card>
          </div>

          <Separator />

          {/* Team List */}
          <div className="space-y-4">
            <h3 className="text-lg font-semibold">我的团队</h3>
            {loadingTeams ? (
              <p className="text-muted-foreground">加载中...</p>
            ) : teams.length === 0 ? (
              <p className="text-muted-foreground">您还没有加入任何团队</p>
            ) : (
              <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                {teams.map((team) => {
                  const isOwner = team.owner_id === user?.id
                  return (
                    <Card key={team.id} className="flex flex-col">
                      <CardHeader className="pb-3">
                        <div className="flex items-center justify-between">
                          <CardTitle className="text-base">{team.name}</CardTitle>
                          <Badge variant={isOwner ? "default" : "secondary"}>
                            {isOwner ? "Owner" : "Member"}
                          </Badge>
                        </div>
                        <CardDescription className="text-xs">
                          创建于 {new Date(team.created_at).toLocaleDateString()}
                        </CardDescription>
                      </CardHeader>
                      <CardContent className="flex-1 space-y-3">
                        {isOwner && (
                          <div className="flex items-center gap-2">
                            <code className="rounded bg-muted px-2 py-1 text-sm font-mono">
                              {team.invite_code}
                            </code>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="size-7"
                              onClick={() => copyInviteCode(team.invite_code, team.id)}
                            >
                              {copiedId === team.id ? (
                                <Check className="size-4 text-green-500" />
                              ) : (
                                <Copy className="size-4" />
                              )}
                            </Button>
                          </div>
                        )}
                        <div className="flex flex-wrap gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => openMembers(team.id)}
                          >
                            <Eye className="mr-1 size-3" />
                            成员
                          </Button>
                          {isOwner ? (
                            <>
                              <Dialog
                                open={renameTeamId === team.id}
                                onOpenChange={(open) => {
                                  if (!open) setRenameTeamId(null)
                                  else {
                                    setRenameTeamId(team.id)
                                    setRenameTeamName(team.name)
                                  }
                                }}
                              >
                                <DialogTrigger asChild>
                                  <Button variant="outline" size="sm">
                                    <Pencil className="mr-1 size-3" />
                                    改名
                                  </Button>
                                </DialogTrigger>
                                <DialogContent>
                                  <DialogHeader>
                                    <DialogTitle>重命名团队</DialogTitle>
                                    <DialogDescription>
                                      输入新的团队名称
                                    </DialogDescription>
                                  </DialogHeader>
                                  <Input
                                    value={renameTeamName}
                                    onChange={(e) => setRenameTeamName(e.target.value)}
                                    placeholder="新名称"
                                  />
                                  <DialogFooter>
                                    <Button
                                      onClick={handleRenameTeam}
                                      disabled={renaming || !renameTeamName.trim()}
                                    >
                                      {renaming ? "保存中..." : "保存"}
                                    </Button>
                                  </DialogFooter>
                                </DialogContent>
                              </Dialog>

                              <AlertDialog>
                                <AlertDialogTrigger asChild>
                                  <Button variant="destructive" size="sm">
                                    <Trash2 className="mr-1 size-3" />
                                    删除
                                  </Button>
                                </AlertDialogTrigger>
                                <AlertDialogContent>
                                  <AlertDialogHeader>
                                    <AlertDialogTitle className="flex items-center gap-2">
                                      <AlertTriangle className="size-5 text-destructive" />
                                      确认删除团队
                                    </AlertDialogTitle>
                                    <AlertDialogDescription>
                                      此操作将永久删除团队 "{team.name}" 及其所有关联数据，无法撤销。
                                    </AlertDialogDescription>
                                  </AlertDialogHeader>
                                  <AlertDialogFooter>
                                    <AlertDialogCancel>取消</AlertDialogCancel>
                                    <AlertDialogAction
                                      onClick={() => handleDeleteTeam(team.id)}
                                      className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                                    >
                                      删除
                                    </AlertDialogAction>
                                  </AlertDialogFooter>
                                </AlertDialogContent>
                              </AlertDialog>
                            </>
                          ) : (
                            <AlertDialog>
                              <AlertDialogTrigger asChild>
                                <Button variant="outline" size="sm">
                                  <LogOut className="mr-1 size-3" />
                                  退出
                                </Button>
                              </AlertDialogTrigger>
                              <AlertDialogContent>
                                <AlertDialogHeader>
                                  <AlertDialogTitle>确认退出团队</AlertDialogTitle>
                                  <AlertDialogDescription>
                                    确定要退出团队 "{team.name}" 吗？
                                  </AlertDialogDescription>
                                </AlertDialogHeader>
                                <AlertDialogFooter>
                                  <AlertDialogCancel>取消</AlertDialogCancel>
                                  <AlertDialogAction onClick={() => handleLeaveTeam(team.id)}>
                                    退出
                                  </AlertDialogAction>
                                </AlertDialogFooter>
                              </AlertDialogContent>
                            </AlertDialog>
                          )}
                        </div>
                      </CardContent>
                    </Card>
                  )
                })}
              </div>
            )}
          </div>

          {/* Members Dialog */}
          <Dialog
            open={!!membersTeamId}
            onOpenChange={(open) => {
              if (!open) {
                setMembersTeamId(null)
                setMembers([])
              }
            }}
          >
            <DialogContent>
              <DialogHeader>
                <DialogTitle>团队成员</DialogTitle>
                <DialogDescription>
                  {teams.find((t) => t.id === membersTeamId)?.name} 的成员列表
                </DialogDescription>
              </DialogHeader>
              <div className="rounded-md border border-primary/40 bg-primary/5 p-3 text-sm">
                <div className="flex items-center gap-2 font-medium">
                  <Shield className="size-4 text-primary" />
                  成员权限管理
                </div>
                <p className="text-xs text-muted-foreground mt-1">
                  Owner/Admin 可以调整成员权限并移除成员
                </p>
              </div>
              <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
                <span>成员</span>
                <span>权限 / 操作</span>
              </div>
              {loadingMembers ? (
                <p className="text-muted-foreground">加载中...</p>
              ) : (
                <div className="space-y-2">
                  {members.map((m) => (
                    <div
                      key={m.id}
                      className="flex items-center justify-between gap-3 rounded-lg border p-3"
                    >
                      <div>
                        <p className="text-sm font-medium">
                          {m.user_name || m.user_email || "未知用户"}
                        </p>
                        <p className="text-xs text-muted-foreground">{m.user_email}</p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge variant={m.role === "owner" ? "default" : m.role === "admin" ? "outline" : "secondary"}>
                          {m.role === "owner" ? "Owner" : m.role === "admin" ? "Admin" : "Member"}
                        </Badge>
                        {canManageMembers && m.role !== "owner" && (
                          <Select
                            value={m.role}
                            onValueChange={(role) => handleUpdateMemberRole(membersTeamId!, m.user_id, role)}
                          >
                            <SelectTrigger className="w-28">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="member">Member</SelectItem>
                              <SelectItem value="admin">Admin</SelectItem>
                            </SelectContent>
                          </Select>
                        )}
                        {canManageMembers && m.role !== "owner" && (
                          <Button variant="outline" size="sm" onClick={() => handleRemoveMember(membersTeamId!, m.user_id)}>
                            移除
                          </Button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </DialogContent>
          </Dialog>
        </TabsContent>

        {/* ─── Provider Tab ──────────────────────────────────────── */}
        <TabsContent value="provider" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>文档后端</CardTitle>
              <CardDescription>选择文档存储和同步的后端服务</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {providerError && (
                <Alert variant="destructive">
                  <AlertDescription>{providerError}</AlertDescription>
                </Alert>
              )}
              {providerSuccess && (
                <Alert>
                  <AlertDescription>{providerSuccess}</AlertDescription>
                </Alert>
              )}
              <div className="grid gap-4 md:grid-cols-2">
                <Card
                  className={`cursor-pointer transition-colors ${
                    currentProvider === "local_file"
                      ? "border-primary bg-primary/5"
                      : "hover:bg-muted/50"
                  }`}
                  onClick={() => handleSwitchProvider("local_file")}
                >
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <Server className="size-5" />
                      内置文档服务器
                    </CardTitle>
                    <CardDescription>使用本地文件系统存储文档</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">
                      数据保存在本地服务器，无需外部授权，适合个人或局域网使用。
                    </p>
                    {currentProvider === "local_file" && (
                      <Badge className="mt-3">当前使用</Badge>
                    )}
                  </CardContent>
                </Card>

                <Card
                  className={`cursor-pointer transition-colors ${
                    currentProvider === "notion"
                      ? "border-primary bg-primary/5"
                      : "hover:bg-muted/50"
                  }`}
                  onClick={() => handleSwitchProvider("notion")}
                >
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <BookOpen className="size-5" />
                      Notion
                    </CardTitle>
                    <CardDescription>使用 Notion 作为文档后端</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">
                      通过 Notion API 同步文档，需要 OAuth 授权登录您的 Notion 工作区。
                    </p>
                    {currentProvider === "notion" && (
                      <Badge className="mt-3">当前使用</Badge>
                    )}
                  </CardContent>
                </Card>
              </div>
              {providerLoading && (
                <p className="text-sm text-muted-foreground">处理中...</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* ─── LLM Tab ───────────────────────────────────────────── */}
        <TabsContent value="llm" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>团队上下文</CardTitle>
              <CardDescription>选择要查看或配置 LLM 的团队</CardDescription>
            </CardHeader>
            <CardContent>
              {teams.length > 0 ? (
                <Select value={llmTeamId} onValueChange={setLlmTeamId}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择团队" />
                  </SelectTrigger>
                  <SelectContent>
                    {teams.map((team) => (
                      <SelectItem key={team.id} value={team.id}>
                        {team.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <p className="text-sm text-muted-foreground">你还没有加入团队，当前仅配置个人模型。</p>
              )}
            </CardContent>
          </Card>

          <ModelSelector
            teamId={llmTeamId || undefined}
            canManageTeamLlm={llmTeamRole === "owner" || llmTeamRole === "admin"}
            teamName={teams.find((team) => team.id === llmTeamId)?.name}
          />

          <PromptSettings
            teamId={llmTeamId || undefined}
            teamName={teams.find((team) => team.id === llmTeamId)?.name}
            canManage={llmTeamRole === "owner" || llmTeamRole === "admin"}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
