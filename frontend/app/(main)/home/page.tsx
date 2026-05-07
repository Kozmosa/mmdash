"use client";

import { useEffect, useState } from "react";
import api from "@/lib/api";
import { useDataCache } from "@/stores/data-cache";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Plus,
  Users,
  FolderOpen,
  Upload,
  CheckCircle2,
  Circle,
  Loader2,
  Trash2,
} from "lucide-react";

interface Team {
  id: string;
  name: string;
  invite_code: string;
}

interface Project {
  id: string;
  name: string;
  description: string | null;
  git_remote_url: string | null;
}

interface Todo {
  id: string;
  content: string;
  completed: boolean;
  is_team_todo: boolean;
}

interface Progress {
  total_todos: number;
  completed_todos: number;
  completion_rate: number;
  team: { total: number; completed: number; rate: number };
  personal: { total: number; completed: number; rate: number };
}

interface ProblemFile {
  id: string;
  filename: string;
  file_type: string;
  uploaded_at: string;
}

export default function HomePage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeam, setSelectedTeam] = useState<string>("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");
  const [todos, setTodos] = useState<Todo[]>([]);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [teamName, setTeamName] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [projectName, setProjectName] = useState("");
  const [projectDesc, setProjectDesc] = useState("");
  const [gitUrl, setGitUrl] = useState("");
  const [todoContent, setTodoContent] = useState("");
  const [isTeamTodo, setIsTeamTodo] = useState(false);
  const [loadingTeams, setLoadingTeams] = useState(true);
  const [loadingProjects, setLoadingProjects] = useState(false);
  const [loadingTodos, setLoadingTodos] = useState(false);
  const [problemFiles, setProblemFiles] = useState<ProblemFile[]>([]);
  const [loadingProblems, setLoadingProblems] = useState(false);
  const [createTeamOpen, setCreateTeamOpen] = useState(false);
  const [joinTeamOpen, setJoinTeamOpen] = useState(false);
  const [createProjectOpen, setCreateProjectOpen] = useState(false);

  useEffect(() => {
    fetchTeams();
  }, []);

  useEffect(() => {
    if (selectedTeam) {
      fetchProjects(selectedTeam);
    }
  }, [selectedTeam]);

  useEffect(() => {
    if (selectedProject) {
      setLoadingTodos(true);
      setLoadingProblems(true);
      Promise.all([
        api.get(`/home/${selectedProject}/todos`),
        api.get(`/home/${selectedProject}/progress`),
        api.get(`/home/${selectedProject}/problems`),
      ])
        .then(([todosRes, progressRes, problemsRes]) => {
          setTodos(todosRes.data);
          setProgress(progressRes.data);
          setProblemFiles(problemsRes.data);
        })
        .catch(() => {
          setTodos([]);
          setProgress(null);
          setProblemFiles([]);
        })
        .finally(() => {
          setLoadingTodos(false);
          setLoadingProblems(false);
        });
    }
  }, [selectedProject]);

  const dataCache = useDataCache();

  const fetchTeams = async () => {
    const cached = dataCache.getTeams();
    if (cached && !dataCache.isTeamsStale()) {
      setTeams(cached);
      if (cached.length > 0 && !selectedTeam) {
        setSelectedTeam(cached[0].id);
      }
      setLoadingTeams(false);
      return;
    }
    setLoadingTeams(true);
    try {
      const res = await api.get("/teams");
      setTeams(res.data);
      dataCache.setTeams(res.data);
      if (res.data.length > 0 && !selectedTeam) {
        setSelectedTeam(res.data[0].id);
      }
    } catch {
      setTeams([]);
    } finally {
      setLoadingTeams(false);
    }
  };

  const fetchProjects = async (teamId: string) => {
    const cached = dataCache.getProjects(teamId);
    if (cached && !dataCache.isProjectsStale(teamId)) {
      setProjects(cached);
      if (cached.length > 0) {
        setSelectedProject(cached[0].id);
      } else {
        setSelectedProject("");
      }
      setLoadingProjects(false);
      return;
    }
    setLoadingProjects(true);
    try {
      const res = await api.get("/projects", { params: { team_id: teamId } });
      setProjects(res.data);
      dataCache.setProjects(teamId, res.data);
      if (res.data.length > 0) {
        setSelectedProject(res.data[0].id);
      } else {
        setSelectedProject("");
      }
    } catch {
      setProjects([]);
      setSelectedProject("");
    } finally {
      setLoadingProjects(false);
    }
  };

  const fetchTodos = async (projectId: string) => {
    setLoadingTodos(true);
    try {
      const res = await api.get(`/home/${projectId}/todos`);
      setTodos(res.data);
    } catch {
      setTodos([]);
    } finally {
      setLoadingTodos(false);
    }
  };

  const fetchProgress = async (projectId: string) => {
    try {
      const res = await api.get(`/home/${projectId}/progress`);
      setProgress(res.data);
    } catch {
      setProgress(null);
    }
  };

  const fetchProblemFiles = async (projectId: string) => {
    setLoadingProblems(true);
    try {
      const res = await api.get(`/home/${projectId}/problems`);
      setProblemFiles(res.data);
    } catch {
      setProblemFiles([]);
    } finally {
      setLoadingProblems(false);
    }
  };

  const deleteProblemFile = async (problemId: string) => {
    if (!selectedProject) return;
    if (!confirm("确定要删除这个文件吗？")) return;
    try {
      await api.delete(`/home/${selectedProject}/problems/${problemId}`);
      setProblemFiles((prev) => prev.filter((f) => f.id !== problemId));
      toast.success("文件已删除");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "删除失败");
    }
  };

  const createTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.post("/teams", { name: teamName });
      setTeamName("");
      setCreateTeamOpen(false);
      fetchTeams();
      toast.success("团队创建成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "创建失败");
    }
  };

  const joinTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api.post("/teams/join", { invite_code: inviteCode });
      setInviteCode("");
      setJoinTeamOpen(false);
      fetchTeams();
      toast.success("加入团队成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "加入失败");
    }
  };

  const createProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedTeam) {
      toast.error("请先选择团队");
      return;
    }
    try {
      await api.post(
        "/projects",
        {
          name: projectName,
          description: projectDesc || undefined,
          git_remote_url: gitUrl || undefined,
        },
        { params: { team_id: selectedTeam } }
      );
      setProjectName("");
      setProjectDesc("");
      setGitUrl("");
      setCreateProjectOpen(false);
      fetchProjects(selectedTeam);
      toast.success("项目创建成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "创建项目失败");
    }
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!selectedProject || !e.target.files?.[0]) return;
    const file = e.target.files[0];
    const formData = new FormData();
    formData.append("file", file);
    try {
      await api.post(`/home/${selectedProject}/upload`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      toast.success("题目上传成功");
      fetchProblemFiles(selectedProject);
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "上传失败");
    }
  };

  const createTodo = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject) return;
    try {
      await api.post(`/home/${selectedProject}/todos`, null, {
        params: { content: todoContent, is_team_todo: isTeamTodo },
      });
      setTodoContent("");
      setIsTeamTodo(false);
      fetchTodos(selectedProject);
      fetchProgress(selectedProject);
      toast.success("TODO 添加成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "添加失败");
    }
  };

  const toggleTodo = async (todoId: string) => {
    if (!selectedProject) return;
    try {
      await api.put(`/home/${selectedProject}/todos/${todoId}`);
      fetchTodos(selectedProject);
      fetchProgress(selectedProject);
    } catch {}
  };

  const selectedTeamName = teams.find((t) => t.id === selectedTeam)?.name;
  const selectedProjectName = projects.find((p) => p.id === selectedProject)?.name;

  return (
    <>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">主页</h1>
          <p className="text-sm text-muted-foreground">
            管理团队、项目、任务和进度
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 xl:grid-cols-5 gap-6">
        {/* 左侧：团队和项目 */}
        <div className="space-y-6">
          {/* 团队选择 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Users className="h-4 w-4" />
                我的团队
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {loadingTeams ? (
                <Skeleton className="h-9 w-full" />
              ) : teams.length === 0 ? (
                <p className="text-sm text-muted-foreground">暂无团队</p>
              ) : (
                <Select value={selectedTeam} onValueChange={setSelectedTeam}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择团队" />
                  </SelectTrigger>
                  <SelectContent>
                    {teams.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              {selectedTeamName && (
                <p className="text-xs text-muted-foreground">
                  邀请码: {teams.find((t) => t.id === selectedTeam)?.invite_code}
                </p>
              )}
              <div className="flex gap-2">
                <Dialog open={createTeamOpen} onOpenChange={setCreateTeamOpen}>
                  <DialogTrigger asChild>
                    <Button variant="outline" size="sm" className="flex-1">
                      <Plus className="h-3.5 w-3.5 mr-1" />
                      创建
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>创建团队</DialogTitle>
                      <DialogDescription>创建一个新的团队以开始协作</DialogDescription>
                    </DialogHeader>
                    <form onSubmit={createTeam} className="space-y-4">
                      <div className="space-y-2">
                        <Label htmlFor="teamName">团队名称</Label>
                        <Input
                          id="teamName"
                          value={teamName}
                          onChange={(e) => setTeamName(e.target.value)}
                          placeholder="输入团队名称"
                          required
                        />
                      </div>
                      <DialogFooter>
                        <Button type="submit">创建团队</Button>
                      </DialogFooter>
                    </form>
                  </DialogContent>
                </Dialog>
                <Dialog open={joinTeamOpen} onOpenChange={setJoinTeamOpen}>
                  <DialogTrigger asChild>
                    <Button variant="outline" size="sm" className="flex-1">
                      加入
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>加入团队</DialogTitle>
                      <DialogDescription>输入邀请码加入已有团队</DialogDescription>
                    </DialogHeader>
                    <form onSubmit={joinTeam} className="space-y-4">
                      <div className="space-y-2">
                        <Label htmlFor="inviteCode">邀请码</Label>
                        <Input
                          id="inviteCode"
                          value={inviteCode}
                          onChange={(e) => setInviteCode(e.target.value)}
                          placeholder="输入邀请码"
                          required
                        />
                      </div>
                      <DialogFooter>
                        <Button type="submit">加入团队</Button>
                      </DialogFooter>
                    </form>
                  </DialogContent>
                </Dialog>
              </div>
            </CardContent>
          </Card>

          {/* 项目选择 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <FolderOpen className="h-4 w-4" />
                项目
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {loadingProjects ? (
                <Skeleton className="h-9 w-full" />
              ) : projects.length === 0 ? (
                <p className="text-sm text-muted-foreground">暂无项目</p>
              ) : (
                <Select value={selectedProject} onValueChange={setSelectedProject}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择项目" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Dialog open={createProjectOpen} onOpenChange={setCreateProjectOpen}>
                <DialogTrigger asChild>
                  <Button variant="outline" size="sm" className="w-full">
                    <Plus className="h-3.5 w-3.5 mr-1" />
                    创建项目
                  </Button>
                </DialogTrigger>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>创建项目</DialogTitle>
                    <DialogDescription>在选定团队下创建新项目</DialogDescription>
                  </DialogHeader>
                  <form onSubmit={createProject} className="space-y-4">
                    <div className="space-y-2">
                      <Label htmlFor="projectName">项目名称</Label>
                      <Input
                        id="projectName"
                        value={projectName}
                        onChange={(e) => setProjectName(e.target.value)}
                        placeholder="输入项目名称"
                        required
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="projectDesc">项目描述</Label>
                      <Input
                        id="projectDesc"
                        value={projectDesc}
                        onChange={(e) => setProjectDesc(e.target.value)}
                        placeholder="可选"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="gitUrl">Git 远程仓库</Label>
                      <Input
                        id="gitUrl"
                        value={gitUrl}
                        onChange={(e) => setGitUrl(e.target.value)}
                        placeholder="可选"
                      />
                    </div>
                    <DialogFooter>
                      <Button type="submit">创建项目</Button>
                    </DialogFooter>
                  </form>
                </DialogContent>
              </Dialog>
            </CardContent>
          </Card>
        </div>

        {/* 中间：题目和 TODO */}
        <div className="lg:col-span-2 xl:col-span-3 space-y-6">
          {/* 题目上传 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Upload className="h-4 w-4" />
                题目上传
              </CardTitle>
              <CardDescription>上传 PDF 或纯文本格式的题目文件</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <Input
                type="file"
                accept=".pdf,.txt"
                onChange={handleFileUpload}
                disabled={!selectedProject}
              />
              {!selectedProject && (
                <p className="text-xs text-muted-foreground">
                  请先选择一个项目
                </p>
              )}

              {loadingProblems ? (
                <div className="space-y-2">
                  <Skeleton className="h-10 w-full" />
                  <Skeleton className="h-10 w-full" />
                </div>
              ) : problemFiles.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  暂无已上传文件
                </p>
              ) : (
                <div className="space-y-2">
                  {problemFiles.map((f) => (
                    <div
                      key={f.id}
                      className="flex items-center gap-3 p-3 rounded-lg border hover:bg-muted/50 transition-colors"
                    >
                      <span className="flex-1 text-sm truncate">{f.filename}</span>
                      <Badge variant="secondary" className="text-xs shrink-0">
                        {f.file_type === "pdf" ? "PDF" : "文本"}
                      </Badge>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 shrink-0 text-muted-foreground hover:text-destructive"
                        onClick={() => deleteProblemFile(f.id)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* TODO 列表 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <CheckCircle2 className="h-4 w-4" />
                TODO 列表
              </CardTitle>
              <CardDescription>
                {selectedProjectName
                  ? `当前项目: ${selectedProjectName}`
                  : "请先选择一个项目"}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <form onSubmit={createTodo} className="flex gap-2 items-end">
                <div className="flex-1 space-y-2">
                  <Input
                    placeholder="添加 TODO..."
                    value={todoContent}
                    onChange={(e) => setTodoContent(e.target.value)}
                    disabled={!selectedProject}
                  />
                </div>
                <div className="flex items-center gap-2 pb-2">
                  <Checkbox
                    id="teamTodo"
                    checked={isTeamTodo}
                    onCheckedChange={(checked) => setIsTeamTodo(checked === true)}
                    disabled={!selectedProject}
                  />
                  <Label htmlFor="teamTodo" className="text-sm whitespace-nowrap">
                    团队
                  </Label>
                </div>
                <Button type="submit" size="sm" disabled={!selectedProject || !todoContent}>
                  <Plus className="h-4 w-4" />
                </Button>
              </form>

              {loadingTodos ? (
                <div className="space-y-2">
                  <Skeleton className="h-12 w-full" />
                  <Skeleton className="h-12 w-full" />
                  <Skeleton className="h-12 w-full" />
                </div>
              ) : todos.length === 0 ? (
                <div className="text-center py-8">
                  <Circle className="h-8 w-8 text-muted-foreground mx-auto mb-2" />
                  <p className="text-sm text-muted-foreground">暂无 TODO</p>
                </div>
              ) : (
                <div className="space-y-2">
                  {todos.map((t) => (
                    <div
                      key={t.id}
                      className="flex items-center gap-3 p-3 rounded-lg border hover:bg-muted/50 transition-colors"
                    >
                      <Checkbox
                        checked={t.completed}
                        onCheckedChange={() => toggleTodo(t.id)}
                      />
                      <span
                        className={`flex-1 text-sm ${
                          t.completed ? "line-through text-muted-foreground" : ""
                        }`}
                      >
                        {t.content}
                      </span>
                      {t.is_team_todo && (
                        <Badge variant="secondary" className="text-xs">
                          团队
                        </Badge>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* 右侧：进度 */}
        <div className="space-y-6">
          {progress && (
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-base">整体进度</CardTitle>
              </CardHeader>
              <CardContent className="space-y-5">
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">总完成度</span>
                    <span className="font-medium">{progress.completion_rate}%</span>
                  </div>
                  <Progress value={progress.completion_rate} />
                </div>
                <Separator />
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">团队 TODO</span>
                    <span className="font-medium">
                      {progress.team.completed}/{progress.team.total}
                    </span>
                  </div>
                  <Progress value={progress.team.rate} />
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">个人 TODO</span>
                    <span className="font-medium">
                      {progress.personal.completed}/{progress.personal.total}
                    </span>
                  </div>
                  <Progress value={progress.personal.rate} />
                </div>
              </CardContent>
            </Card>
          )}

          {!progress && !loadingProjects && selectedProject && (
            <Card>
              <CardContent className="py-8 text-center">
                <Loader2 className="h-8 w-8 text-muted-foreground mx-auto mb-2 animate-spin" />
                <p className="text-sm text-muted-foreground">加载进度中...</p>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
