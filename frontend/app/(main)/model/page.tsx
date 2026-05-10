"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type * as React from "react";
import api from "@/lib/api";
import { useDataCache } from "@/stores/data-cache";
import { toast } from "sonner";
import {
  AlertCircle,
  ArrowLeftRight,
  CheckSquare,
  ChevronDown,
  Copy,
  Download,
  FileText,
  GitCommit,
  Maximize2,
  Minimize2,
  Network,
  Plus,
  RotateCcw,
  Save,
  Sigma,
  Table2,
  Users,
  Wand2,
  X,
} from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import MarkdownRenderer from "@/components/model/markdown-renderer";
import { ModelEditorShell, type ModelEditorShellHandle } from "@/components/model/model-editor-shell";

interface Team {
  id: string;
  name: string;
}

interface Project {
  id: string;
  name: string;
  description: string | null;
  git_remote_url: string | null;
  model_data_page_id: string | null;
}

interface SymbolItem {
  symbol?: string;
  name?: string;
  meaning?: string;
  type?: string;
  source?: string;
}

interface Commit {
  id: string;
  commit_message: string;
  user_email: string;
  user_name: string | null;
  created_at: string;
}

type DiffChunk =
  | { type: "context"; lines: string[] }
  | { type: "insert"; lines: string[] }
  | { type: "delete"; lines: string[] }
  | { type: "replace"; before: string[]; after: string[] };

interface ErrorItem {
  excerpt: string;
  description: string;
  severity: "warning" | "error";
}

interface RollbackPreview {
  snapshot: {
    id: string;
    commit_message: string;
    user_email: string;
    created_at: string;
  };
  current: {
    page_id: string | null;
    markdown: string;
  };
  target: {
    page_id: string;
    markdown: string;
  };
  diff: string;
  diff_chunks?: DiffChunk[];
  can_write: boolean;
  provider_type: string | null;
}

type SaveStatus = "saved" | "saving" | "failed" | "offline" | "conflict";
type TopMenu = "editor" | "version_control" | "version_compare";
type AITool = "symbols" | "structure" | "correction" | "formula" | "table" | "formula_block";
type PanelMode = "floating" | "fullscreen";

const aiTools: Array<{
  id: AITool;
  label: string;
  description: string;
  icon: React.ElementType;
}> = [
  { id: "symbols", label: "符号表", description: "识别文档中的数学符号及定义", icon: Sigma },
  { id: "structure", label: "结构解析", description: "分析文档层级和建模逻辑", icon: Network },
  { id: "correction", label: "纠错检查", description: "检查逻辑、公式和表达问题", icon: CheckSquare },
  { id: "formula", label: "公式解析", description: "解释选中公式或输入公式", icon: Wand2 },
  { id: "table", label: "表格", description: "在文档中插入表格块", icon: Table2 },
  { id: "formula_block", label: "公式块", description: "在文档中插入 LaTeX 公式块", icon: Sigma },
];

function getSelectedText() {
  if (typeof window === "undefined") return "";
  return window.getSelection()?.toString().trim() || "";
}

export default function ModelPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeam, setSelectedTeam] = useState("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState("");
  const [pageId, setPageId] = useState("");
  const [markdown, setMarkdown] = useState("");
  const [documentTitle, setDocumentTitle] = useState("模型文档");
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("saved");
  const [loading, setLoading] = useState(false);

  const [symbols, setSymbols] = useState<SymbolItem[]>([]);
  const [structure, setStructure] = useState<any>(null);
  const [formulaInput, setFormulaInput] = useState("");
  const [formulaExplanation, setFormulaExplanation] = useState("");
  const [errors, setErrors] = useState<ErrorItem[]>([]);
  const [dismissedErrors, setDismissedErrors] = useState<Set<number>>(new Set());
  const [analysisLoading, setAnalysisLoading] = useState(false);

  const [commits, setCommits] = useState<Commit[]>([]);
  const [commitMessage, setCommitMessage] = useState("");
  const [selectedBaseCommit, setSelectedBaseCommit] = useState("");
  const [selectedCompareCommit, setSelectedCompareCommit] = useState("");
  const [diffResult, setDiffResult] = useState("");
  const [diffChunks, setDiffChunks] = useState<DiffChunk[] | null>(null);

  const [activeTopMenu, setActiveTopMenu] = useState<TopMenu>("editor");
  const [activeTool, setActiveTool] = useState<AITool | null>(null);
  const [aiPanelVisible, setAiPanelVisible] = useState(false);
  const [aiPanelMode, setAiPanelMode] = useState<PanelMode>("floating");

  const [projectDialogOpen, setProjectDialogOpen] = useState(false);
  const [versionDialogOpen, setVersionDialogOpen] = useState(false);
  const [compareDialogOpen, setCompareDialogOpen] = useState(false);
  const [rollbackPreview, setRollbackPreview] = useState<RollbackPreview | null>(null);
  const [rollbackDialogOpen, setRollbackDialogOpen] = useState(false);
  const [rollbackLoadingId, setRollbackLoadingId] = useState<string | null>(null);
  const [rollbackApplying, setRollbackApplying] = useState(false);

  const editorRef = useRef<ModelEditorShellHandle | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestMarkdownRef = useRef("");
  const saveRequestIdRef = useRef(0);
  const dataCache = useDataCache();

  const currentProject = projects.find((p) => p.id === selectedProject);
  const hasDocument = Boolean(currentProject?.model_data_page_id);

  const currentToolLabel = aiTools.find((tool) => tool.id === activeTool)?.label || "AI Tools";
  const resultText = useMemo(() => {
    if (activeTool === "symbols") {
      return symbols.map((s) => `${s.symbol || s.name || ""}\t${s.type || s.source || ""}\n${s.meaning || ""}`).join("\n\n");
    }
    if (activeTool === "structure") return JSON.stringify(structure || {}, null, 2);
    if (activeTool === "correction") return errors.map((e) => `${e.severity}: ${e.excerpt}\n${e.description}`).join("\n\n");
    if (activeTool === "formula") return formulaExplanation;
    if (activeTool === "table") return "| 符号 | 含义 | 单位 |\n| --- | --- | --- |\n| x(t) | 状态变量 | - |";
    if (activeTool === "formula_block") return "$$ x(t+1)=Ax(t)+Bu(t) $$";
    return "";
  }, [activeTool, errors, formulaExplanation, structure, symbols]);

  useEffect(() => {
    fetchTeams();
  }, []);

  useEffect(() => {
    if (selectedTeam) fetchProjects(selectedTeam);
  }, [selectedTeam]);

  useEffect(() => {
    if (!selectedProject) return;
    const project = projects.find((p) => p.id === selectedProject);
    setDocumentTitle(project ? `${project.name} 模型` : "模型文档");
    setLoading(true);
    const contentPromise = project?.model_data_page_id
      ? api
          .get(`/model/${selectedProject}/content`)
          .then((res) => {
            const md = res.data.markdown || "";
            setMarkdown(md);
            setSaveStatus("saved");
          })
          .catch((err: any) => {
            toast.error(err.response?.data?.detail || "获取模型内容失败");
            setMarkdown("");
          })
      : Promise.resolve().then(() => {
          setMarkdown("");
        });
    const commitsPromise = fetchCommits(selectedProject);
    Promise.all([contentPromise, commitsPromise]).finally(() => setLoading(false));
  }, [selectedProject, projects.length]);

  useEffect(() => {
    return () => {
      if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
    };
  }, []);

  useEffect(() => {
    latestMarkdownRef.current = markdown;
  }, [markdown]);

  const fetchTeams = async () => {
    const cached = dataCache.getTeams();
    if (cached && !dataCache.isTeamsStale()) {
      setTeams(cached);
      if (cached.length > 0 && !selectedTeam) setSelectedTeam(cached[0].id);
      return;
    }
    try {
      const res = await api.get("/teams");
      setTeams(res.data);
      dataCache.setTeams(res.data);
      if (res.data.length > 0 && !selectedTeam) setSelectedTeam(res.data[0].id);
    } catch {
      setTeams([]);
    }
  };

  const fetchProjects = async (teamId: string) => {
    const cached = dataCache.getProjects(teamId);
    if (cached && !dataCache.isProjectsStale(teamId)) {
      setProjects(cached);
      setSelectedProject((prev) => prev || cached[0]?.id || "");
      return;
    }
    try {
      const res = await api.get("/projects", { params: { team_id: teamId } });
      setProjects(res.data);
      dataCache.setProjects(teamId, res.data);
      setSelectedProject((prev) => prev || res.data[0]?.id || "");
    } catch {
      setProjects([]);
      setSelectedProject("");
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

  const updateCurrentProjectPage = (nextPageId: string) => {
    if (!selectedTeam || !selectedProject) return;
    const updatedProjects = projects.map((project) =>
      project.id === selectedProject ? { ...project, model_data_page_id: nextPageId } : project
    );
    setProjects(updatedProjects);
    dataCache.setProjects(selectedTeam, updatedProjects);
  };

  const persistContent = async (content: string, silent = true) => {
    if (!selectedProject || !hasDocument) return;
    const requestId = saveRequestIdRef.current + 1;
    saveRequestIdRef.current = requestId;
    setSaveStatus("saving");
    try {
      await api.post(`/model/${selectedProject}/content`, { markdown: content });
      const isLatestSave = requestId === saveRequestIdRef.current && latestMarkdownRef.current === content;
      if (isLatestSave) {
        setSaveStatus("saved");
        if (!silent) toast.success("已保存");
      }
    } catch (err: any) {
      const isLatestSave = requestId === saveRequestIdRef.current && latestMarkdownRef.current === content;
      if (isLatestSave) {
        setSaveStatus("failed");
        if (!silent) toast.error(err.response?.data?.detail || "保存失败");
      }
    }
  };

  const scheduleSave = (nextMarkdown: string) => {
    latestMarkdownRef.current = nextMarkdown;
    setMarkdown(nextMarkdown);
    if (!hasDocument) return;
    setSaveStatus("saving");
    if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
    saveTimerRef.current = setTimeout(() => persistContent(nextMarkdown), 1000);
  };

  const createDocument = async () => {
    if (!selectedProject) return;
    try {
      const res = await api.post(`/model/${selectedProject}/create-page`, { title: documentTitle });
      updateCurrentProjectPage(res.data.page_id);
      setMarkdown("");
      setSaveStatus("saved");
      toast.success("文档已创建");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "创建失败");
    }
  };

  const linkPage = async () => {
    if (!selectedProject || !pageId) return;
    try {
      await api.post(`/model/${selectedProject}/link`, null, { params: { page_id: pageId } });
      updateCurrentProjectPage(pageId);
      setPageId("");
      setProjectDialogOpen(false);
      toast.success("页面已绑定");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "绑定失败");
    }
  };

  const exportMarkdown = async () => {
    if (!selectedProject) return;
    try {
      const res = await api.get(`/model/${selectedProject}/export/md`, { responseType: "blob" });
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

  const openTool = async (tool: AITool) => {
    setActiveTool(tool);
    setAiPanelVisible(true);
    if (tool === "table" || tool === "formula_block") return;
    setAnalysisLoading(true);
    try {
      if (tool === "symbols") {
        const res = await api.get(`/model/${selectedProject}/analyze/symbols`);
        setSymbols(res.data.symbols || []);
      } else if (tool === "structure") {
        const res = await api.get(`/model/${selectedProject}/analyze/structure`);
        setStructure(res.data.structure || {});
      } else if (tool === "correction") {
        const res = await api.get(`/model/${selectedProject}/analyze/errors`);
        setErrors(res.data.errors || []);
        setDismissedErrors(new Set());
      } else if (tool === "formula") {
        const selected = getSelectedText();
        const formula = selected || formulaInput || markdown.match(/\$?\$?([^$\n=]+=[^$\n]+)\$?\$?/)?.[1] || "";
        setFormulaInput(formula);
        if (!formula) return;
        const res = await api.post(`/model/${selectedProject}/analyze/formula`, null, {
          params: { formula },
        });
        setFormulaExplanation(res.data.explanation || "");
      }
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "AI 工具调用失败");
    } finally {
      setAnalysisLoading(false);
    }
  };

  const handleEditorMarkdownChange = (nextMarkdown: string) => {
    scheduleSave(nextMarkdown);
  };

  const createCommit = async () => {
    if (!selectedProject || !commitMessage) return;
    await persistContent(markdown, true);
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
      setDiffChunks(Array.isArray(res.data.diff_chunks) ? res.data.diff_chunks : null);
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "Diff 失败");
    }
  };

  const openRollbackPreview = async (snapshotId: string) => {
    if (!selectedProject) return;
    setRollbackLoadingId(snapshotId);
    try {
      const res = await api.get(`/model-version/${selectedProject}/rollback-preview`, {
        params: { snapshot_id: snapshotId },
      });
      setRollbackPreview(res.data);
      setRollbackDialogOpen(true);
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "回滚预览失败");
    } finally {
      setRollbackLoadingId(null);
    }
  };

  const rollback = async () => {
    if (!selectedProject || !rollbackPreview) return;
    setRollbackApplying(true);
    try {
      await api.post(`/model-version/${selectedProject}/rollback`, null, {
        params: { snapshot_id: rollbackPreview.snapshot.id },
      });
      const nextMarkdown = rollbackPreview.target.markdown || "";
      setMarkdown(nextMarkdown);
      fetchCommits(selectedProject);
      setRollbackDialogOpen(false);
      setRollbackPreview(null);
      toast.success("已回滚并写回文档");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "回滚失败");
    } finally {
      setRollbackApplying(false);
    }
  };

  const copyResult = async (text = resultText) => {
    await navigator.clipboard.writeText(text || "");
    toast.success("已复制");
  };

  const insertResult = () => {
    if (!resultText) return;
    editorRef.current?.insertMarkdown(resultText);
    toast.success("已插入到文档");
  };

  const saveStatusText = {
    saved: "已保存",
    saving: "保存中...",
    failed: "保存失败，点击重试",
    offline: "离线编辑中",
    conflict: "检测到版本冲突",
  }[saveStatus];

  const aiPanel = aiPanelVisible && activeTool ? (
    <aside
      className={
        aiPanelMode === "fullscreen"
          ? "fixed inset-x-6 bottom-6 top-24 z-40 overflow-hidden rounded-xl border bg-background shadow-2xl"
          : "fixed bottom-6 right-28 top-24 z-40 w-[420px] overflow-hidden rounded-xl border bg-background shadow-xl"
      }
    >
      <div className="flex h-full flex-col">
        <div className="flex items-start justify-between border-b p-5">
          <div>
            <h2 className="text-xl font-semibold">{currentToolLabel}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {aiTools.find((tool) => tool.id === activeTool)?.description}
            </p>
          </div>
          <div className="flex gap-1">
            <Button variant="ghost" size="icon" onClick={() => setAiPanelMode(aiPanelMode === "fullscreen" ? "floating" : "fullscreen")}>
              {aiPanelMode === "fullscreen" ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
            </Button>
            <Button variant="ghost" size="icon" onClick={() => {
              setAiPanelVisible(false);
              setAiPanelMode("floating");
            }}>
              <X className="size-4" />
            </Button>
          </div>
        </div>
        <div className="flex gap-2 border-b px-5 py-3 text-sm">
          {(["symbols", "structure", "correction", "formula"] as AITool[]).map((tool) => (
            <button
              key={tool}
              className={`border-b-2 px-1 pb-2 ${activeTool === tool ? "border-foreground font-medium" : "border-transparent text-muted-foreground"}`}
              onClick={() => openTool(tool)}
            >
              {aiTools.find((item) => item.id === tool)?.label}
            </button>
          ))}
        </div>
        <ScrollArea className="flex-1">
          <div className="space-y-4 p-5">
            {analysisLoading ? (
              <div className="space-y-3">
                <Skeleton className="h-20 w-full" />
                <Skeleton className="h-20 w-full" />
                <Skeleton className="h-20 w-full" />
              </div>
            ) : (
              <AIResultContent
                activeTool={activeTool}
                symbols={symbols}
                structure={structure}
                errors={errors}
                dismissedErrors={dismissedErrors}
                formulaInput={formulaInput}
                setFormulaInput={setFormulaInput}
                formulaExplanation={formulaExplanation}
                onFormulaExplain={() => openTool("formula")}
                onDismissError={(index) => setDismissedErrors((prev) => new Set(prev).add(index))}
                onCopy={copyResult}
              />
            )}
          </div>
        </ScrollArea>
        <div className="grid grid-cols-2 gap-3 border-t p-5">
          <Button onClick={insertResult} className="bg-foreground text-background hover:bg-foreground/90">
            插入到文档
          </Button>
          <Button variant="outline" onClick={() => copyResult()}>
            <Copy className="mr-2 size-4" />
            复制结果
          </Button>
        </div>
      </div>
    </aside>
  ) : null;

  return (
    <div className="min-h-[calc(100vh-4rem)] bg-background text-foreground">
      <div className="fixed right-20 top-3 z-[60] flex items-center rounded-lg border bg-background/95 p-1 shadow-sm backdrop-blur">
        <Button variant="ghost" size="sm" className="gap-2 rounded-md" onClick={() => persistContent(markdown, false)} disabled={!hasDocument}>
          <Save className="size-4" />
          保存
        </Button>
        <Button variant="ghost" size="sm" className="gap-2 rounded-md" onClick={exportMarkdown} disabled={!hasDocument}>
          <Download className="size-4" />
          导出
        </Button>
        <div className="mx-1 h-5 w-px bg-border" />
        <TopMenuButton active={activeTopMenu === "editor"} onClick={() => setActiveTopMenu("editor")}>
          <FileText className="size-4" />
          编辑器
          </TopMenuButton>
          <TopMenuButton active={activeTopMenu === "version_control"} onClick={() => {
            setActiveTopMenu("version_control");
            setVersionDialogOpen(true);
          }}>
            <GitCommit className="size-4" />
            版本控制
          </TopMenuButton>
          <TopMenuButton active={activeTopMenu === "version_compare"} onClick={() => {
            setActiveTopMenu("version_compare");
            setCompareDialogOpen(true);
          }}>
            <ArrowLeftRight className="size-4" />
            版本对比
          </TopMenuButton>
          <TopMenuButton active={false} onClick={() => setProjectDialogOpen(true)}>
            <Users className="size-4" />
          项目选择
          <ChevronDown className="size-3" />
        </TopMenuButton>
      </div>

      <div className="relative flex min-h-[calc(100vh-4rem)]">
        <main className="flex-1 px-8 pb-8 pt-0 pr-28">
          <div className="w-full max-w-none">
            <div className="mb-4 flex flex-wrap items-center gap-3 border-b pb-4">
              <FileText className="size-5" />
              <button
                className="text-lg font-semibold outline-none"
                onClick={() => {
                  const nextTitle = window.prompt("重命名模型文档", documentTitle);
                  if (nextTitle) setDocumentTitle(nextTitle);
                }}
              >
                {documentTitle}
              </button>
              <span className="text-muted-foreground">·</span>
              <button
                className={`text-sm ${saveStatus === "failed" ? "text-destructive" : "text-muted-foreground"}`}
                onClick={() => saveStatus === "failed" && persistContent(markdown, false)}
              >
                {saveStatusText}
              </button>
            </div>

            {!selectedProject ? (
              <EmptyState title="请选择一个项目" description="点击右上角项目选择后开始编辑模型文档。" />
            ) : !hasDocument ? (
              <EmptyState
                title="还没有模型文档"
                description="创建内置文档或绑定现有页面后即可开始写作。"
                action={<Button onClick={createDocument}><Plus className="mr-2 size-4" />创建模型文档</Button>}
              />
            ) : loading ? (
              <div className="space-y-4 pt-12">
                <Skeleton className="h-10 w-1/2" />
                <Skeleton className="h-5 w-full" />
                <Skeleton className="h-5 w-4/5" />
                <Skeleton className="h-40 w-full" />
              </div>
            ) : (
              <div className="relative">
                <ModelEditorShell
                  ref={editorRef}
                  value={markdown}
                  onChange={handleEditorMarkdownChange}
                />
              </div>
            )}
          </div>
        </main>

        <aside className="fixed right-6 top-24 z-30 flex max-h-[calc(100vh-8rem)] w-16 flex-col items-center gap-3 rounded-xl border bg-background p-3 shadow-sm">
          <div className="mb-1 text-center text-xs text-muted-foreground">AI Tools</div>
          {aiTools.map((tool) => {
            const Icon = tool.icon;
            return (
              <Button
                key={tool.id}
                variant="outline"
                size="icon"
                className={`size-10 bg-background ${activeTool === tool.id && aiPanelVisible ? "border-foreground ring-1 ring-foreground" : ""}`}
                title={tool.label}
                onClick={() => openTool(tool.id)}
                disabled={!selectedProject || !hasDocument}
              >
                <Icon className="size-4" />
              </Button>
            );
          })}
        </aside>

        {aiPanel}
      </div>

      <Dialog open={projectDialogOpen} onOpenChange={setProjectDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>项目选择</DialogTitle>
            <DialogDescription>切换当前项目或绑定模型文档页面。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <Select value={selectedTeam} onValueChange={setSelectedTeam}>
              <SelectTrigger><SelectValue placeholder="选择团队" /></SelectTrigger>
              <SelectContent>{teams.map((team) => <SelectItem key={team.id} value={team.id}>{team.name}</SelectItem>)}</SelectContent>
            </Select>
            <Select value={selectedProject} onValueChange={(value) => {
              if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
              persistContent(markdown);
              setSelectedProject(value);
            }}>
              <SelectTrigger><SelectValue placeholder="选择项目" /></SelectTrigger>
              <SelectContent>
                {projects.map((project) => (
                  <SelectItem key={project.id} value={project.id}>
                    {project.name}{project.model_data_page_id ? " · 已绑定" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex gap-2">
              <Input placeholder="Page ID" value={pageId} onChange={(event) => setPageId(event.target.value)} />
              <Button onClick={linkPage} variant="outline">绑定</Button>
            </div>
            <Button onClick={createDocument} className="w-full" disabled={!selectedProject}>
              <Plus className="mr-2 size-4" />
              创建模型文档
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={versionDialogOpen} onOpenChange={setVersionDialogOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>版本控制</DialogTitle>
            <DialogDescription>创建快照、查看历史版本并回滚。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex gap-2">
              <Input placeholder="提交信息..." value={commitMessage} onChange={(event) => setCommitMessage(event.target.value)} />
              <Button onClick={createCommit}>提交版本</Button>
            </div>
            <ScrollArea className="h-80 rounded-md border">
              <div className="space-y-2 p-3">
                {commits.map((commit) => (
                  <div key={commit.id} className="flex items-center justify-between rounded-md border p-3">
                    <div>
                      <div className="font-medium">{commit.commit_message}</div>
                      <div className="text-xs text-muted-foreground">{commit.user_email} · {new Date(commit.created_at).toLocaleString()}</div>
                    </div>
                    <Button variant="ghost" size="sm" onClick={() => openRollbackPreview(commit.id)} disabled={rollbackLoadingId === commit.id}>
                      <RotateCcw className="mr-2 size-4" />
                      回滚
                    </Button>
                  </div>
                ))}
                {commits.length === 0 && <div className="py-10 text-center text-sm text-muted-foreground">暂无提交记录</div>}
              </div>
            </ScrollArea>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={compareDialogOpen} onOpenChange={setCompareDialogOpen}>
        <DialogContent className="sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>版本对比</DialogTitle>
            <DialogDescription>选择两个版本查看内容差异。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
            <Select value={selectedBaseCommit} onValueChange={setSelectedBaseCommit}>
              <SelectTrigger><SelectValue placeholder="基础版本" /></SelectTrigger>
              <SelectContent>{commits.map((commit) => <SelectItem key={commit.id} value={commit.id}>{commit.commit_message}</SelectItem>)}</SelectContent>
            </Select>
            <Select value={selectedCompareCommit} onValueChange={setSelectedCompareCommit}>
              <SelectTrigger><SelectValue placeholder="对比版本" /></SelectTrigger>
              <SelectContent>{commits.map((commit) => <SelectItem key={commit.id} value={commit.id}>{commit.commit_message}</SelectItem>)}</SelectContent>
            </Select>
            <Button onClick={viewDiff}>查看 Diff</Button>
          </div>
          <ScrollArea className="h-[460px] rounded-md border bg-background">
            <DiffViewer diff={diffResult} diffChunks={diffChunks} />
          </ScrollArea>
        </DialogContent>
      </Dialog>

      <AlertDialog open={rollbackDialogOpen} onOpenChange={setRollbackDialogOpen}>
        <AlertDialogContent className="sm:max-w-3xl">
          <AlertDialogHeader>
            <AlertDialogTitle>确认回滚</AlertDialogTitle>
            <AlertDialogDescription>当前内容将自动备份，然后写回所选版本。</AlertDialogDescription>
          </AlertDialogHeader>
          {rollbackPreview && (
            <div className="space-y-4">
              {!rollbackPreview.can_write && (
                <Alert variant="destructive">
                  <AlertCircle className="size-4" />
                  <AlertTitle>无法写回</AlertTitle>
                  <AlertDescription>当前文档后端不支持回滚写回。</AlertDescription>
                </Alert>
              )}
              <ScrollArea className="h-72 rounded-md border bg-background">
                <DiffViewer diff={rollbackPreview.diff} diffChunks={rollbackPreview.diff_chunks} compact />
              </ScrollArea>
            </div>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rollbackApplying}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                rollback();
              }}
              disabled={!rollbackPreview?.can_write || rollbackApplying}
            >
              确认写回
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function TopMenuButton({
  active,
  children,
  onClick,
}: {
  active: boolean;
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <Button
      variant={active ? "secondary" : "ghost"}
      size="sm"
      className="gap-2 rounded-md"
      onClick={onClick}
    >
      {children}
    </Button>
  );
}

function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex min-h-[520px] flex-col items-center justify-center rounded-lg border border-dashed text-center">
      <FileText className="mb-4 size-10 text-muted-foreground" />
      <h2 className="text-xl font-semibold">{title}</h2>
      <p className="mt-2 max-w-sm text-sm text-muted-foreground">{description}</p>
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}

function DiffViewer({
  diff,
  diffChunks,
  compact = false,
}: {
  diff: string;
  diffChunks?: DiffChunk[] | null;
  compact?: boolean;
}) {
  const hasStructured = Array.isArray(diffChunks) && diffChunks.length > 0;
  if (!diff && !hasStructured) {
    return (
      <div className="flex h-full min-h-60 items-center justify-center text-sm text-muted-foreground">
        选择两个版本后查看差异。
      </div>
    );
  }

  type DiffEntry =
    | { type: "meta"; text: string }
    | { type: "context"; lines: string[] }
    | { type: "insert"; lines: string[] }
    | { type: "delete"; lines: string[] }
    | { type: "modify"; oldLine: string; newLine: string };

  const entries: DiffEntry[] = [];

  if (hasStructured) {
    for (const chunk of diffChunks || []) {
      if (chunk.type === "context") {
        if (chunk.lines.length) entries.push({ type: "context", lines: chunk.lines });
        continue;
      }
      if (chunk.type === "insert") {
        if (chunk.lines.length) entries.push({ type: "insert", lines: chunk.lines });
        continue;
      }
      if (chunk.type === "delete") {
        if (chunk.lines.length) entries.push({ type: "delete", lines: chunk.lines });
        continue;
      }
      const before = chunk.before || [];
      const after = chunk.after || [];
      const paired = Math.min(before.length, after.length);
      for (let index = 0; index < paired; index += 1) {
        entries.push({ type: "modify", oldLine: before[index] || "", newLine: after[index] || "" });
      }
      if (before.length > paired) {
        entries.push({ type: "delete", lines: before.slice(paired) });
      }
      if (after.length > paired) {
        entries.push({ type: "insert", lines: after.slice(paired) });
      }
    }
  } else {
    let contextBuffer: string[] = [];
    let removeBuffer: string[] = [];

    const flushContext = () => {
      if (!contextBuffer.length) return;
      entries.push({ type: "context", lines: contextBuffer });
      contextBuffer = [];
    };

    const flushRemoveAsDelete = () => {
      if (!removeBuffer.length) return;
      entries.push({ type: "delete", lines: removeBuffer });
      removeBuffer = [];
    };

    const flushPending = () => {
      flushContext();
      flushRemoveAsDelete();
    };

    for (const line of diff.split("\n")) {
      if (line.startsWith("\\ No newline at end of file")) {
        continue;
      }
      if (line.startsWith("diff --git") || line.startsWith("index ") || line.startsWith("--- ") || line.startsWith("+++ ") || line.startsWith("@@")) {
        flushPending();
        entries.push({ type: "meta", text: line });
        continue;
      }

      if (line.startsWith("-")) {
        flushContext();
        removeBuffer.push(line.slice(1));
        continue;
      }

      if (line.startsWith("+")) {
        if (removeBuffer.length > 0) {
          const oldLine = removeBuffer.shift() || "";
          entries.push({ type: "modify", oldLine, newLine: line.slice(1) });
          continue;
        }
        flushContext();
        entries.push({ type: "insert", lines: [line.slice(1)] });
        continue;
      }

      flushRemoveAsDelete();
      contextBuffer.push(line.startsWith(" ") ? line.slice(1) : line);
    }

    flushPending();
  }

  const metaClasses = compact ? "px-3 py-2 text-[11px]" : "px-4 py-2 text-xs";
  const chunkClasses = compact ? "p-3" : "p-4";

  return (
    <div className={`space-y-3 p-4 ${compact ? "text-sm" : "text-base"}`}>
      {entries.map((entry, index) => {
        if (entry.type === "meta") {
          const isHunk = entry.text.startsWith("@@");
          const isPath = entry.text.startsWith("--- ") || entry.text.startsWith("+++ ");
          return (
            <div
              key={`${index}-${entry.text}`}
              className={`rounded-lg border border-dashed bg-muted/30 text-muted-foreground ${metaClasses}`}
            >
              <span className={`font-medium ${isHunk ? "text-foreground" : ""}`}>{entry.text}</span>
              {isPath && <span className="ml-2 text-[10px] uppercase tracking-[0.18em]">file</span>}
            </div>
          );
        }

        if (entry.type === "modify") {
          return (
            <div
              key={`${index}-${entry.oldLine}-${entry.newLine}`}
              className="overflow-hidden rounded-xl border border-amber-200 bg-amber-50/70 shadow-sm dark:border-amber-900/50 dark:bg-amber-950/20"
            >
              <div className="flex items-center justify-between border-b px-4 py-2 text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
                <span className="font-medium text-foreground">修改</span>
                <span>1 行</span>
              </div>
              <div className={`${chunkClasses} space-y-2`}>
                <InlineDiffLine oldLine={entry.oldLine} newLine={entry.newLine} compact={compact} />
              </div>
            </div>
          );
        }

        if (entry.type === "insert") {
          return (
            <div
              key={`${index}-${entry.lines.join("\n")}`}
              className="overflow-hidden rounded-xl border border-emerald-200 bg-emerald-50/80 shadow-sm dark:border-emerald-900/50 dark:bg-emerald-950/20"
            >
              <div className="flex items-center justify-between border-b px-4 py-2 text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
                <span className="font-medium text-foreground">新增</span>
                <span>{entry.lines.length} 行</span>
              </div>
              <div className={chunkClasses}>
                <MarkdownRenderer markdown={entry.lines.join("\n")} />
              </div>
            </div>
          );
        }

        if (entry.type === "delete") {
          return (
            <div
              key={`${index}-${entry.lines.join("\n")}`}
              className="overflow-hidden rounded-xl border border-rose-200 bg-rose-50/80 shadow-sm dark:border-rose-900/50 dark:bg-rose-950/20"
            >
              <div className="flex items-center justify-between border-b px-4 py-2 text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
                <span className="font-medium text-foreground">删除</span>
                <span>{entry.lines.length} 行</span>
              </div>
              <div className={`${chunkClasses} opacity-85`}>
                {entry.lines.map((line, lineIndex) => (
                  <div key={lineIndex} className="whitespace-pre-wrap break-words line-through decoration-rose-400 decoration-2">
                    {line || " "}
                  </div>
                ))}
              </div>
            </div>
          );
        }

        return (
          <div
            key={`${index}-${entry.lines.join("\n")}`}
            className="overflow-hidden rounded-xl border border-border bg-card shadow-sm"
          >
            <div className="flex items-center justify-between border-b px-4 py-2 text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
              <span className="font-medium text-foreground">上下文</span>
              <span>{entry.lines.length} 行</span>
            </div>
            <div className={chunkClasses}>
              <MarkdownRenderer markdown={entry.lines.join("\n")} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function InlineDiffLine({
  oldLine,
  newLine,
  compact = false,
}: {
  oldLine: string;
  newLine: string;
  compact?: boolean;
}) {
  const prefixLength = sharedPrefixLength(oldLine, newLine);
  const suffixLength = sharedSuffixLength(oldLine, newLine, prefixLength);
  const oldMiddle = oldLine.slice(prefixLength, oldLine.length - suffixLength);
  const newMiddle = newLine.slice(prefixLength, newLine.length - suffixLength);
  const prefix = oldLine.slice(0, prefixLength);
  const suffix = oldLine.slice(oldLine.length - suffixLength);
  const lineClass = compact ? "text-[13px] leading-6" : "text-sm leading-7";

  return (
    <div className="space-y-2">
      <div className={`rounded-md border border-rose-200 bg-white/70 px-3 py-2 text-muted-foreground dark:border-rose-900/50 dark:bg-background/40 ${lineClass}`}>
        <span>{prefix}</span>
        {oldMiddle ? <span className="rounded bg-rose-200/70 px-0.5 line-through decoration-rose-500 decoration-2 dark:bg-rose-900/40">{oldMiddle}</span> : null}
        <span>{suffix}</span>
      </div>
      <div className={`rounded-md border border-emerald-200 bg-white px-3 py-2 text-foreground dark:border-emerald-900/50 dark:bg-background/60 ${lineClass}`}>
        <span>{prefix}</span>
        {newMiddle ? <span className="rounded bg-emerald-200/80 px-0.5 font-medium text-emerald-900 dark:bg-emerald-900/40 dark:text-emerald-200">{newMiddle}</span> : null}
        <span>{suffix}</span>
      </div>
    </div>
  );
}

function sharedPrefixLength(left: string, right: string) {
  const maxLength = Math.min(left.length, right.length);
  let index = 0;
  while (index < maxLength && left[index] === right[index]) {
    index += 1;
  }
  return index;
}

function sharedSuffixLength(left: string, right: string, prefixLength: number) {
  const maxLength = Math.min(left.length, right.length) - prefixLength;
  let length = 0;
  while (length < maxLength && left[left.length - 1 - length] === right[right.length - 1 - length]) {
    length += 1;
  }
  return length;
}

function AIResultContent({
  activeTool,
  symbols,
  structure,
  errors,
  dismissedErrors,
  formulaInput,
  setFormulaInput,
  formulaExplanation,
  onFormulaExplain,
  onDismissError,
  onCopy,
}: {
  activeTool: AITool;
  symbols: SymbolItem[];
  structure: any;
  errors: ErrorItem[];
  dismissedErrors: Set<number>;
  formulaInput: string;
  setFormulaInput: (value: string) => void;
  formulaExplanation: string;
  onFormulaExplain: () => void;
  onDismissError: (index: number) => void;
  onCopy: (text?: string) => void;
}) {
  if (activeTool === "symbols") {
    return symbols.length ? (
      <div className="space-y-3">
        {symbols.map((item, index) => {
          const symbol = item.symbol || item.name || "符号";
          const type = item.type || item.source || "上下文推断";
          const text = `${symbol}\t${type}\n${item.meaning || ""}`;
          return (
            <ResultCard key={`${symbol}-${index}`} title={symbol} badge={type} description={item.meaning || "暂无说明"} onCopy={() => onCopy(text)} />
          );
        })}
      </div>
    ) : <PanelHint icon={Sigma} text="点击符号表后识别文档中的数学符号。" />;
  }

  if (activeTool === "structure") {
    return structure && Object.keys(structure).length ? (
      <div className="space-y-4">
        {structure.summary && <ResultCard title="总体概述" description={structure.summary} />}
        {Array.isArray(structure.sections) && (
          <div className="rounded-lg border p-4">
            <div className="font-semibold">关键章节</div>
            <ul className="mt-3 space-y-2 text-sm text-muted-foreground">
              {structure.sections.map((section: string, index: number) => <li key={index}>{section}</li>)}
            </ul>
          </div>
        )}
        {structure.problem_relationship && <ResultCard title="与题目对应关系" description={structure.problem_relationship} />}
      </div>
    ) : <PanelHint icon={Network} text="点击结构解析后查看文档大纲、建模逻辑和缺失部分。" />;
  }

  if (activeTool === "correction") {
    const visibleErrors = errors.filter((_, index) => !dismissedErrors.has(index));
    return visibleErrors.length ? (
      <div className="space-y-3">
        {visibleErrors.map((error, index) => (
          <ResultCard
            key={`${error.excerpt}-${index}`}
            title={error.severity === "error" ? "错误" : "警告"}
            badge={error.severity}
            description={`${error.excerpt}\n${error.description}`}
            onCopy={() => onCopy(`${error.excerpt}\n${error.description}`)}
            action={<Button variant="ghost" size="sm" onClick={() => onDismissError(index)}>关闭</Button>}
          />
        ))}
      </div>
    ) : <PanelHint icon={CheckSquare} text="点击纠错检查后查看潜在文字、逻辑、符号或公式问题。" />;
  }

  if (activeTool === "formula") {
    return (
      <div className="space-y-4">
        <div className="space-y-2">
          <Input placeholder="输入或选中公式..." value={formulaInput} onChange={(event) => setFormulaInput(event.target.value)} />
          <Button onClick={onFormulaExplain} className="w-full">解析公式</Button>
        </div>
        {formulaExplanation ? (
          <ResultCard title="公式解释" badge="fx" description={formulaExplanation} onCopy={() => onCopy(formulaExplanation)} />
        ) : <PanelHint icon={Wand2} text="选中公式后点击公式解析，或在这里输入公式。" />}
      </div>
    );
  }

  if (activeTool === "table") {
    return <ResultCard title="表格块" badge="Markdown" description={"| 符号 | 含义 | 单位 |\n| --- | --- | --- |\n| x(t) | 状态变量 | - |"} />;
  }

  return <ResultCard title="公式块" badge="LaTeX" description="$$ x(t+1)=Ax(t)+Bu(t) $$" />;
}

function ResultCard({
  title,
  badge,
  description,
  onCopy,
  action,
}: {
  title: string;
  badge?: string;
  description: string;
  onCopy?: () => void;
  action?: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border bg-background p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="font-mono text-lg font-semibold">{title}</span>
            {badge && <Badge variant="secondary">{badge}</Badge>}
          </div>
          <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        <div className="flex shrink-0 gap-1">
          {action}
          {onCopy && (
            <Button variant="ghost" size="icon" onClick={onCopy}>
              <Copy className="size-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

function PanelHint({ icon: Icon, text }: { icon: React.ElementType; text: string }) {
  return (
    <div className="flex min-h-60 flex-col items-center justify-center rounded-lg border border-dashed text-center text-muted-foreground">
      <Icon className="mb-3 size-8" />
      <p className="max-w-xs text-sm">{text}</p>
    </div>
  );
}
