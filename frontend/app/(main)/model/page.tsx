"use client";

import { useEffect, useState } from "react";
import api from "@/lib/api";
import { useDataCache } from "@/stores/data-cache";
import { toast } from "sonner";
import dynamic from "next/dynamic";

const MarkdownRenderer = dynamic(() => import("@/components/model/markdown-renderer"), {
  loading: () => (
    <div className="space-y-4">
      <Skeleton className="h-4 w-3/4" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-5/6" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="h-4 w-full" />
    </div>
  ),
});
const FormulaExplanationRenderer = dynamic(() => import("@/components/model/markdown-renderer"), {
  loading: () => <Skeleton className="h-32 w-full" />,
});

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert";
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
} from "@/components/ui/alert-dialog";
import {
  FileText,
  Link2,
  GitCommit,
  ArrowLeftRight,
  Sigma,
  LayoutList,
  AlertTriangle,
  Wand2,
  Download,
  Users,
  FolderOpen,
  RotateCcw,
  Loader2,
  AlertCircle,
  Plus,
} from "lucide-react";

interface Team {
  id: string;
  name: string;
}

interface Project {
  id: string;
  name: string;
  model_data_page_id: string | null;
}

interface Symbol {
  symbol: string;
  meaning: string;
  source: string;
}

interface Commit {
  id: string;
  commit_message: string;
  user_email: string;
  user_name: string | null;
  created_at: string;
}

interface ErrorItem {
  excerpt: string;
  description: string;
  severity: "warning" | "error";
}

export default function ModelPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeam, setSelectedTeam] = useState<string>("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");
  const [pageId, setPageId] = useState("");
  const [markdown, setMarkdown] = useState<string>("");
  const [loading, setLoading] = useState(false);

  const [symbols, setSymbols] = useState<Symbol[]>([]);
  const [structure, setStructure] = useState<any>(null);
  const [formulaInput, setFormulaInput] = useState("");
  const [formulaExplanation, setFormulaExplanation] = useState("");
  const [analysisLoading, setAnalysisLoading] = useState(false);
  const [activeTab, setActiveTab] = useState("content");

  const [commits, setCommits] = useState<Commit[]>([]);
  const [commitMessage, setCommitMessage] = useState("");
  const [selectedBaseCommit, setSelectedBaseCommit] = useState("");
  const [selectedCompareCommit, setSelectedCompareCommit] = useState("");
  const [diffResult, setDiffResult] = useState<string>("");

  const [errors, setErrors] = useState<ErrorItem[]>([]);
  const [dismissedErrors, setDismissedErrors] = useState<Set<number>>(new Set());

  const [editMode, setEditMode] = useState(false);
  const [editContent, setEditContent] = useState("");

  const dataCache = useDataCache();

  const [loadingTeams, setLoadingTeams] = useState(true);
  const [loadingProjects, setLoadingProjects] = useState(false);

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
      const proj = projects.find((p) => p.id === selectedProject);
      const contentPromise = proj?.model_data_page_id
        ? api.get(`/model/${selectedProject}/content`).then((res) => {
            setMarkdown(res.data.markdown || "");
          }).catch((err: any) => {
            toast.error(err.response?.data?.detail || "获取模型内容失败");
            setMarkdown("");
          })
        : Promise.resolve(setMarkdown(""));
      const commitsPromise = api.get(`/model-version/${selectedProject}/commits`)
        .then((res) => setCommits(res.data))
        .catch(() => setCommits([]));
      setLoading(true);
      Promise.all([contentPromise, commitsPromise]).finally(() => {
        setLoading(false);
      });
    }
  }, [selectedProject]);

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
        setMarkdown("");
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
        setMarkdown("");
      }
    } catch {
      setProjects([]);
      setSelectedProject("");
      setMarkdown("");
    } finally {
      setLoadingProjects(false);
    }
  };

  const fetchCommits = async (projectId: string) => {
    try {
      const res = await api.get(`/model-version/${projectId}/commits`);
      setCommits(res.data);
    } catch {
      setCommits([]);
    }
  };

  const linkPage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject || !pageId) return;
    try {
      await api.post(`/model/${selectedProject}/link`, null, {
        params: { page_id: pageId },
      });
      fetchProjects(selectedTeam);
      setPageId("");
      toast.success("页面已绑定");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "绑定失败");
    }
  };

  const saveContent = async () => {
    if (!selectedProject || !editContent) return;
    try {
      await api.post(`/model/${selectedProject}/content`, {
        markdown: editContent,
      });
      setMarkdown(editContent);
      setEditMode(false);
      toast.success("保存成功");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "保存失败");
    }
  };

  const createDocument = async () => {
    if (!selectedProject) return;
    const proj = projects.find((p) => p.id === selectedProject);
    const title = proj ? `${proj.name} 模型` : "新模型文档";
    try {
      await api.post(`/model/${selectedProject}/create-page`, {
        title,
      });
      fetchProjects(selectedTeam);
      toast.success("文档已创建");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "创建失败");
    }
  };

  const exportMarkdown = async () => {
    if (!selectedProject) return;
    try {
      const res = await api.get(`/model/${selectedProject}/export/md`, {
        responseType: "blob",
      });
      const url = window.URL.createObjectURL(new Blob([res.data]));
      const link = document.createElement("a");
      link.href = url;
      link.setAttribute("download", `model_${selectedProject}.md`);
      document.body.appendChild(link);
      link.click();
      link.remove();
      toast.success("导出成功");
    } catch {
      toast.error("导出失败");
    }
  };

  const fetchSymbols = async () => {
    if (!selectedProject) return;
    setAnalysisLoading(true);
    try {
      const res = await api.get(`/model/${selectedProject}/analyze/symbols`);
      setSymbols(res.data.symbols || []);
      setActiveTab("symbols");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "符号分析失败");
    } finally {
      setAnalysisLoading(false);
    }
  };

  const fetchStructure = async () => {
    if (!selectedProject) return;
    setAnalysisLoading(true);
    try {
      const res = await api.get(`/model/${selectedProject}/analyze/structure`);
      setStructure(res.data.structure || {});
      setActiveTab("structure");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "结构分析失败");
    } finally {
      setAnalysisLoading(false);
    }
  };

  const explainFormula = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject || !formulaInput) return;
    setAnalysisLoading(true);
    try {
      const res = await api.post(`/model/${selectedProject}/analyze/formula`, null, {
        params: { formula: formulaInput },
      });
      setFormulaExplanation(res.data.explanation || "");
      setActiveTab("formula");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "公式解释失败");
    } finally {
      setAnalysisLoading(false);
    }
  };

  const createCommit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject || !commitMessage) return;
    try {
      await api.post(`/model-version/${selectedProject}/commit`, null, {
        params: { message: commitMessage },
      });
      setCommitMessage("");
      fetchCommits(selectedProject);
      toast.success("版本已提交");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "提交失败");
    }
  };

  const viewDiff = async () => {
    if (!selectedProject || !selectedBaseCommit || !selectedCompareCommit) return;
    try {
      const res = await api.get(`/model-version/${selectedProject}/diff`, {
        params: { base_id: selectedBaseCommit, compare_id: selectedCompareCommit },
      });
      setDiffResult(res.data.diff || "");
      setActiveTab("version");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "Diff 失败");
    }
  };

  const fetchErrors = async () => {
    if (!selectedProject) return;
    setAnalysisLoading(true);
    try {
      const res = await api.get(`/model/${selectedProject}/analyze/errors`);
      setErrors(res.data.errors || []);
      setDismissedErrors(new Set());
      setActiveTab("correction");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "纠错分析失败");
    } finally {
      setAnalysisLoading(false);
    }
  };

  const dismissError = (index: number) => {
    setDismissedErrors((prev) => new Set(prev).add(index));
  };

  const rollback = async (snapshotId: string) => {
    if (!selectedProject) return;
    try {
      await api.post(`/model-version/${selectedProject}/rollback`, null, {
        params: { snapshot_id: snapshotId },
      });
      fetchCommits(selectedProject);
      toast.success("回滚准备完成，请检查 Notion 页面");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "回滚失败");
    }
  };

  return (
    <>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">模型</h1>
          <p className="text-sm text-muted-foreground">
            查看、分析和版本管理数学模型
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 xl:grid-cols-5 gap-6">
        {/* 左侧 */}
        <div className="space-y-6">
          {/* 团队/项目选择 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Users className="h-4 w-4" />
                选择项目
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {loadingTeams ? (
                <Skeleton className="h-9 w-full" />
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
              {loadingProjects ? (
                <Skeleton className="h-9 w-full" />
              ) : (
                <Select value={selectedProject} onValueChange={setSelectedProject}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择项目" />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        <div className="flex items-center gap-2">
                          {p.name}
                          {p.model_data_page_id && (
                            <Badge variant="outline" className="text-xs">已绑定</Badge>
                          )}
                        </div>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </CardContent>
          </Card>

          {/* 绑定文档页面 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Link2 className="h-4 w-4" />
                绑定文档页面
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <Button
                variant="outline"
                className="w-full"
                disabled={!selectedProject}
                onClick={createDocument}
              >
                <Plus className="h-4 w-4 mr-1" />
                创建新文档
              </Button>
              <div className="text-xs text-muted-foreground text-center">或手动绑定现有页面</div>
              <form onSubmit={linkPage} className="space-y-3">
                <Input
                  placeholder="Page ID"
                  value={pageId}
                  onChange={(e) => setPageId(e.target.value)}
                  required
                />
                <Button type="submit" className="w-full" disabled={!selectedProject}>
                  绑定
                </Button>
              </form>
            </CardContent>
          </Card>

          {/* 版本控制 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <GitCommit className="h-4 w-4" />
                版本控制
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <form onSubmit={createCommit} className="space-y-3">
                <Input
                  placeholder="提交信息..."
                  value={commitMessage}
                  onChange={(e) => setCommitMessage(e.target.value)}
                  required
                />
                <Button type="submit" className="w-full" disabled={!selectedProject}>
                  提交版本
                </Button>
              </form>
              <Separator />
              <ScrollArea className="h-40">
                <div className="space-y-2">
                  {commits.map((c) => (
                    <div key={c.id} className="text-sm border rounded-lg p-2">
                      <div className="font-medium truncate">{c.commit_message}</div>
                      <div className="text-xs text-muted-foreground">
                        {c.user_email} · {new Date(c.created_at).toLocaleDateString()}
                      </div>
                      <AlertDialog>
                        <AlertDialogTrigger asChild>
                          <Button variant="link" size="sm" className="h-auto p-0 text-xs">
                            <RotateCcw className="h-3 w-3 mr-1" />
                            回滚
                          </Button>
                        </AlertDialogTrigger>
                        <AlertDialogContent>
                          <AlertDialogHeader>
                            <AlertDialogTitle>确认回滚</AlertDialogTitle>
                            <AlertDialogDescription>
                              确定要回滚到「{c.commit_message}」吗？当前内容将自动备份。
                            </AlertDialogDescription>
                          </AlertDialogHeader>
                          <AlertDialogFooter>
                            <AlertDialogCancel>取消</AlertDialogCancel>
                            <AlertDialogAction onClick={() => rollback(c.id)}>
                              确认回滚
                            </AlertDialogAction>
                          </AlertDialogFooter>
                        </AlertDialogContent>
                      </AlertDialog>
                    </div>
                  ))}
                  {commits.length === 0 && (
                    <p className="text-xs text-muted-foreground text-center py-4">暂无提交记录</p>
                  )}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>

          {/* Diff */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <ArrowLeftRight className="h-4 w-4" />
                版本对比
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <Select value={selectedBaseCommit} onValueChange={setSelectedBaseCommit}>
                <SelectTrigger>
                  <SelectValue placeholder="基础版本" />
                </SelectTrigger>
                <SelectContent>
                  {commits.map((c) => (
                    <SelectItem key={c.id} value={c.id}>{c.commit_message}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={selectedCompareCommit} onValueChange={setSelectedCompareCommit}>
                <SelectTrigger>
                  <SelectValue placeholder="对比版本" />
                </SelectTrigger>
                <SelectContent>
                  {commits.map((c) => (
                    <SelectItem key={c.id} value={c.id}>{c.commit_message}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button onClick={viewDiff} variant="secondary" className="w-full">
                查看 Diff
              </Button>
            </CardContent>
          </Card>

          {/* AI 分析 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Wand2 className="h-4 w-4" />
                AI 分析
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <Button
                onClick={fetchSymbols}
                disabled={!selectedProject || analysisLoading}
                variant="outline"
                className="w-full"
              >
                <Sigma className="h-4 w-4 mr-1" />
                符号表
              </Button>
              <Button
                onClick={fetchStructure}
                disabled={!selectedProject || analysisLoading}
                variant="outline"
                className="w-full"
              >
                <LayoutList className="h-4 w-4 mr-1" />
                结构解析
              </Button>
              <Button
                onClick={fetchErrors}
                disabled={!selectedProject || analysisLoading}
                variant="outline"
                className="w-full"
              >
                <AlertTriangle className="h-4 w-4 mr-1" />
                纠错检查
              </Button>
              <form onSubmit={explainFormula} className="space-y-2 pt-2">
                <Input
                  placeholder="输入公式..."
                  value={formulaInput}
                  onChange={(e) => setFormulaInput(e.target.value)}
                />
                <Button
                  type="submit"
                  disabled={!selectedProject || analysisLoading}
                  variant="outline"
                  className="w-full"
                >
                  公式解释
                </Button>
              </form>
            </CardContent>
          </Card>

          {/* 导出 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Download className="h-4 w-4" />
                导出
              </CardTitle>
            </CardHeader>
            <CardContent>
              <Button onClick={exportMarkdown} disabled={!selectedProject} className="w-full">
                导出 Markdown
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* 右侧：内容区 */}
        <div className="lg:col-span-3 xl:col-span-4">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full grid grid-cols-6">
              <TabsTrigger value="content">内容</TabsTrigger>
              <TabsTrigger value="symbols">符号表</TabsTrigger>
              <TabsTrigger value="structure">结构解析</TabsTrigger>
              <TabsTrigger value="formula">公式解释</TabsTrigger>
              <TabsTrigger value="correction">纠错</TabsTrigger>
              <TabsTrigger value="version">版本/Diff</TabsTrigger>
            </TabsList>

            <div className="mt-4 min-h-[600px]">
              {analysisLoading && (
                <div className="flex flex-col items-center justify-center py-20">
                  <Loader2 className="h-8 w-8 animate-spin text-primary" />
                  <p className="text-muted-foreground mt-2">AI 分析中...</p>
                </div>
              )}

              <TabsContent value="content" className="mt-0">
                {loading ? (
                  <div className="space-y-4">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-4 w-full" />
                    <Skeleton className="h-4 w-5/6" />
                    <Skeleton className="h-4 w-full" />
                  </div>
                ) : markdown || editMode ? (
                  <div className="space-y-4">
                    <div className="flex items-center justify-end gap-2">
                      {editMode ? (
                        <>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setEditMode(false);
                              setEditContent(markdown);
                            }}
                          >
                            取消
                          </Button>
                          <Button size="sm" onClick={saveContent}>
                            保存
                          </Button>
                        </>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setEditContent(markdown);
                            setEditMode(true);
                          }}
                        >
                          编辑
                        </Button>
                      )}
                    </div>
                    {editMode ? (
                      <textarea
                        className="w-full min-h-[500px] p-4 rounded-md border bg-background font-mono text-sm resize-y focus:outline-none focus:ring-2 focus:ring-ring"
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                      />
                    ) : (
                      <MarkdownRenderer markdown={markdown} />
                    )}
                  </div>
                ) : (
                  <div className="text-center py-20">
                    <FileText className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">
                      {selectedProject ? "暂无文档内容" : "请选择一个项目"}
                    </p>
                    {selectedProject && (
                      <Button
                        variant="outline"
                        className="mt-4"
                        onClick={createDocument}
                      >
                        <Plus className="h-4 w-4 mr-1" />
                        创建模型文档
                      </Button>
                    )}
                  </div>
                )}
              </TabsContent>

              <TabsContent value="symbols" className="mt-0">
                <Alert variant="warning" className="mb-4">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>仅供参考</AlertTitle>
                  <AlertDescription>请人工审核符号含义</AlertDescription>
                </Alert>
                {symbols.length === 0 ? (
                  <div className="text-center py-12">
                    <Sigma className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">暂无符号分析结果</p>
                    <p className="text-sm text-muted-foreground">点击左侧"符号表"按钮开始分析</p>
                  </div>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>符号</TableHead>
                        <TableHead>含义</TableHead>
                        <TableHead>来源</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {symbols.map((s, i) => (
                        <TableRow key={i}>
                          <TableCell className="font-mono text-lg">{s.symbol}</TableCell>
                          <TableCell>{s.meaning}</TableCell>
                          <TableCell>
                            <Badge variant={s.source === "user" ? "default" : "secondary"}>
                              {s.source === "user" ? "手工定义" : "上下文推断"}
                            </Badge>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </TabsContent>

              <TabsContent value="structure" className="mt-0">
                <Alert variant="warning" className="mb-4">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>仅供参考</AlertTitle>
                  <AlertDescription>请人工审核结构分析</AlertDescription>
                </Alert>
                {!structure ? (
                  <div className="text-center py-12">
                    <LayoutList className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">暂无结构分析结果</p>
                    <p className="text-sm text-muted-foreground">点击左侧"结构解析"按钮开始分析</p>
                  </div>
                ) : (
                  <div className="space-y-6">
                    {structure.summary && (
                      <div>
                        <h3 className="font-semibold text-lg mb-2">总体概述</h3>
                        <p className="text-muted-foreground">{structure.summary}</p>
                      </div>
                    )}
                    {structure.sections && structure.sections.length > 0 && (
                      <div>
                        <h3 className="font-semibold text-lg mb-2">关键章节</h3>
                        <ul className="list-disc pl-5 space-y-1">
                          {structure.sections.map((s: string, i: number) => (
                            <li key={i} className="text-muted-foreground">{s}</li>
                          ))}
                        </ul>
                      </div>
                    )}
                    {structure.problem_relationship && (
                      <div>
                        <h3 className="font-semibold text-lg mb-2">与题目对应关系</h3>
                        <p className="text-muted-foreground">{structure.problem_relationship}</p>
                      </div>
                    )}
                  </div>
                )}
              </TabsContent>

              <TabsContent value="formula" className="mt-0">
                <Alert variant="warning" className="mb-4">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>仅供参考</AlertTitle>
                  <AlertDescription>请人工审核公式解释</AlertDescription>
                </Alert>
                {!formulaExplanation ? (
                  <div className="text-center py-12">
                    <Sigma className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">输入公式并点击"公式解释"按钮</p>
                  </div>
                ) : (
                  <div className="space-y-4">
                    <div className="bg-muted p-4 rounded-lg">
                      <div className="text-sm text-muted-foreground mb-1">原始公式</div>
                      <div className="font-mono text-lg">{formulaInput}</div>
                    </div>
                    <div>
                      <div className="text-sm text-muted-foreground mb-1">解释</div>
                      <FormulaExplanationRenderer markdown={formulaExplanation} />
                    </div>
                  </div>
                )}
              </TabsContent>

              <TabsContent value="correction" className="mt-0">
                <Alert variant="warning" className="mb-4">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertTitle>仅供参考</AlertTitle>
                  <AlertDescription>请人工审核纠错建议</AlertDescription>
                </Alert>
                {errors.length === 0 ? (
                  <div className="text-center py-12">
                    <AlertCircle className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">暂无纠错结果</p>
                    <p className="text-sm text-muted-foreground">点击左侧"纠错检查"按钮开始分析</p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {errors.map((err, i) => (
                      !dismissedErrors.has(i) && (
                        <Alert
                          key={i}
                          variant={err.severity === "error" ? "destructive" : "default"}
                          className="relative"
                        >
                          <div className="absolute top-2 right-2">
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6"
                              onClick={() => dismissError(i)}
                            >
                              ✕
                            </Button>
                          </div>
                          <AlertTitle className="flex items-center gap-2">
                            {err.severity === "error" ? "错误" : "警告"}
                          </AlertTitle>
                          <AlertDescription className="space-y-2 mt-2">
                            <div className="bg-muted p-2 rounded font-mono text-sm">
                              {err.excerpt}
                            </div>
                            <p>{err.description}</p>
                          </AlertDescription>
                        </Alert>
                      )
                    ))}
                    {errors.every((_, i) => dismissedErrors.has(i)) && (
                      <div className="text-center py-8 text-muted-foreground">
                        所有批注已关闭
                      </div>
                    )}
                  </div>
                )}
              </TabsContent>

              <TabsContent value="version" className="mt-0">
                <h3 className="font-semibold text-lg mb-4">版本对比</h3>
                {diffResult ? (
                  <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm font-mono">
                    {diffResult}
                  </pre>
                ) : (
                  <div className="text-center py-12">
                    <ArrowLeftRight className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                    <p className="text-muted-foreground">在左侧选择两个版本并点击"查看 Diff"</p>
                  </div>
                )}
              </TabsContent>
            </div>
          </Tabs>
        </div>
      </div>
    </>
  );
}
