"use client";

import { useEffect, useRef, useState } from "react";
import {
  connectLocalAgent,
  disconnectLocalAgent,
  getLocalAgentState,
  type LocalAgentConnectionState,
  LocalAgentError,
  sendAction,
  subscribeLocalAgentState,
} from "@/lib/local_agent";
import api from "@/lib/api";
import { useDataCache } from "@/stores/data-cache";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Textarea } from "@/components/ui/textarea";
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
  Wifi,
  WifiOff,
  Terminal,
  GitBranch,
  Play,
  Save,
  Upload,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  FolderOpen,
  FlaskConical,
  Loader2,
} from "lucide-react";

interface EnvInfo {
  python_version: string;
  conda_available: boolean;
  conda_envs: string[];
  gcc_available: boolean;
  git_available: boolean;
  cpu_count: number;
  memory_gb: number;
}

export default function ExperimentPage() {
  const [connected, setConnected] = useState(false);
  const [connectionState, setConnectionState] = useState<LocalAgentConnectionState>(
    getLocalAgentState()
  );
  const [envInfo, setEnvInfo] = useState<EnvInfo | null>(null);
  const [command, setCommand] = useState("");
  const [shellOutput, setShellOutput] = useState("");
  const [lastAgentError, setLastAgentError] = useState("");
  const [solverPath, setSolverPath] = useState("");
  const [repoPath, setRepoPath] = useState("");
  const [paramName, setParamName] = useState("");
  const [paramValues, setParamValues] = useState("");
  const [params, setParams] = useState<Record<string, string[]>>({});
  const [rangeName, setRangeName] = useState("");
  const [rangeStart, setRangeStart] = useState("");
  const [rangeEnd, setRangeEnd] = useState("");
  const [rangeStep, setRangeStep] = useState("");
  const [experimentResult, setExperimentResult] = useState<any>(null);
  const [loading, setLoading] = useState(false);

  const [projectId, setProjectId] = useState("");
  const [solvers, setSolvers] = useState<any[]>([]);
  const [gitCommits, setGitCommits] = useState<string[]>([]);
  const [selectedSolver, setSelectedSolver] = useState<string>("");
  const [extractedParams, setExtractedParams] = useState<any[]>([]);
  const [analysisContent, setAnalysisContent] = useState("");
  const [resultFiles, setResultFiles] = useState<string[]>([]);
  const [experiments, setExperiments] = useState<any[]>([]);
  const [selectedExperiment, setSelectedExperiment] = useState<any>(null);
  const dataCache = useDataCache();

  const [activeTab, setActiveTab] = useState("run");
  const [projects, setProjects] = useState<any[]>([]);
  const [loadingProjects, setLoadingProjects] = useState(true);
  const isMountedRef = useRef(false);

  useEffect(() => {
    fetchProjects();
  }, []);

  useEffect(() => {
    isMountedRef.current = true;
    const unsubscribe = subscribeLocalAgentState((state) => {
      if (!isMountedRef.current) return;
      setConnectionState(state);
      setConnected(state === "ready");
    });
    checkConnection();
    return () => {
      isMountedRef.current = false;
      unsubscribe();
      disconnectLocalAgent();
    };
  }, []);

  const fetchProjects = async () => {
    setLoadingProjects(true);
    try {
      const teamsRes = await api.get("/teams");
      if (teamsRes.data.length > 0) {
        const teamId = teamsRes.data[0].id;
        dataCache.setTeams(teamsRes.data);
        const cachedProjects = dataCache.getProjects(teamId);
        if (cachedProjects && !dataCache.isProjectsStale(teamId)) {
          setProjects(cachedProjects);
          if (cachedProjects.length > 0) {
            setProjectId(cachedProjects[0].id);
          }
        } else {
          const projectsRes = await api.get("/projects", {
            params: { team_id: teamId },
          });
          setProjects(projectsRes.data);
          dataCache.setProjects(teamId, projectsRes.data);
          if (projectsRes.data.length > 0) {
            setProjectId(projectsRes.data[0].id);
          }
        }
      }
    } catch {
      setProjects([]);
    } finally {
      setLoadingProjects(false);
    }
  };

  const scanSolvers = async () => {
    if (!projectId || !repoPath) {
      toast.error("请选择项目并指定仓库路径");
      return;
    }
    setLoading(true);
    try {
      const res = await api.get(`/git/${projectId}/scan`, {
        params: { repo_path: repoPath },
      });
      setSolvers(res.data.solvers || []);
      toast.success("扫描完成");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "扫描失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchGitLog = async () => {
    if (!projectId || !repoPath) {
      toast.error("请选择项目并指定仓库路径");
      return;
    }
    setLoading(true);
    try {
      const res = await api.get(`/git/${projectId}/log`, {
        params: { repo_path: repoPath },
      });
      setGitCommits(res.data.commits || []);
      toast.success("Git 日志加载完成");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "加载 Git 日志失败");
    } finally {
      setLoading(false);
    }
  };

  const extractSolverParams = async (path: string) => {
    if (!projectId) return;
    setSelectedSolver(path);
    try {
      const res = await api.get(`/git/${projectId}/params`, {
        params: { solver_path: path },
      });
      setExtractedParams(res.data.params || []);
      const newParams: Record<string, string[]> = {};
      res.data.params.forEach((p: any) => {
        if (p.type === "number") {
          newParams[p.name] = [p.default, String(Number(p.default) * 2)];
        } else {
          newParams[p.name] = [p.default];
        }
      });
      setParams(newParams);
    } catch {}
  };

  const checkConnection = async () => {
    try {
      await connectLocalAgent();
      if (!isMountedRef.current) return;
      setConnected(true);
      setConnectionState("ready");
      await detectEnv();
    } catch {
      if (!isMountedRef.current) return;
      setConnected(false);
      setConnectionState(getLocalAgentState());
    }
  };

  const detectEnv = async () => {
    try {
      const data = await sendAction("detect_env");
      if (!isMountedRef.current) return;
      setEnvInfo(data);
      setLastAgentError("");
    } catch (err: any) {
      if (!isMountedRef.current || shouldIgnoreLocalAgentError(err)) {
        return;
      }
      const message = formatLocalAgentError(err);
      setLastAgentError(message);
      toast.error("环境检测失败: " + message);
    }
  };

  const runShell = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!command) return;
    if (!repoPath) {
      toast.error("请先指定 Git 仓库路径，Shell 调试仅允许在仓库范围内执行");
      return;
    }
    setLoading(true);
    try {
      const data = await sendAction("shell.run", {
        command,
        repo_path: repoPath,
        cwd: repoPath,
      });
      setLastAgentError("");
      setShellOutput(
        `Exit code: ${data.returncode}\n\nSTDOUT:\n${data.stdout}\n\nSTDERR:\n${data.stderr}`
      );
    } catch (err: any) {
      const message = formatLocalAgentError(err);
      setLastAgentError(message);
      toast.error("命令执行失败: " + message);
    } finally {
      setLoading(false);
    }
  };

  const addParam = () => {
    if (!paramName || !paramValues) return;
    const vals = paramValues.split(",").map((v) => v.trim());
    setParams((prev) => ({ ...prev, [paramName]: vals }));
    setParamName("");
    setParamValues("");
  };

  const addRangeParam = () => {
    if (!rangeName || !rangeStart || !rangeEnd || !rangeStep) return;
    const start = parseFloat(rangeStart);
    const end = parseFloat(rangeEnd);
    const step = parseFloat(rangeStep);
    if (isNaN(start) || isNaN(end) || isNaN(step) || step === 0) {
      toast.error("参数范围设置无效");
      return;
    }
    const vals: string[] = [];
    for (let v = start; v <= end + 1e-9; v += step) {
      vals.push(String(Number(v.toFixed(6))));
    }
    setParams((prev) => ({ ...prev, [rangeName]: vals }));
    setRangeName("");
    setRangeStart("");
    setRangeEnd("");
    setRangeStep("");
  };

  const removeParam = (name: string) => {
    setParams((prev) => {
      const copy = { ...prev };
      delete copy[name];
      return copy;
    });
  };

  const runExperiment = async () => {
    if (!solverPath) {
      toast.error("请指定 Solver 文件路径");
      return;
    }
    setLoading(true);
    try {
      const data = await sendAction("experiment.run", {
        solver_path: solverPath,
        param_grid: params,
        git_repo_path: repoPath || ".",
      });
      setLastAgentError("");
      setExperimentResult(data);
      if (data.result_dir) {
        scanResultFiles(data.result_dir);
      }
      toast.success("实验执行完成");
    } catch (err: any) {
      const message = formatLocalAgentError(err);
      setLastAgentError(message);
      toast.error("实验执行失败: " + message);
    } finally {
      setLoading(false);
    }
  };

  const scanResultFiles = async (dir: string) => {
    if (!repoPath) return;
    try {
      const data = await sendAction("fs.list", {
        repo_path: repoPath,
        path: dir,
      });
      const files = (data.files || []).filter((f: string) => f.trim());
      setResultFiles(files);
      const analysisPath = files.find((f: string) => f.endsWith("analysis.md"));
      if (analysisPath) {
        const readData = await sendAction("fs.read", {
          repo_path: repoPath,
          path: analysisPath,
        });
        setAnalysisContent(readData.content || "");
      }
    } catch (err) {
      setLastAgentError(formatLocalAgentError(err));
    }
  };

  const saveAnalysis = async () => {
    if (!experimentResult?.result_dir || !repoPath) return;
    const analysisPath = `${experimentResult.result_dir}/analysis.md`;
    try {
      await sendAction("fs.write", {
        repo_path: repoPath,
        path: analysisPath,
        content: analysisContent,
      });
      setLastAgentError("");
      toast.success("analysis.md 已保存");
    } catch (err: any) {
      const message = formatLocalAgentError(err);
      setLastAgentError(message);
      toast.error("保存失败: " + message);
    }
  };

  const gitAddCommitPush = async () => {
    if (!repoPath) {
      toast.error("请指定 Git 仓库路径");
      return;
    }
    setLoading(true);
    try {
      const result = await sendAction("git.add_commit_push", {
        repo_path: repoPath,
        commit_message: "experiment results",
      });
      if (result.returncode !== 0) {
        const failedStep = result.steps?.find((step: any) => step.returncode !== 0);
        const message = `Git 同步失败: ${failedStep?.command || "unknown"}${failedStep?.stderr ? `: ${failedStep.stderr}` : ""}`;
        setLastAgentError(message);
        toast.error(
          message
        );
        return;
      }
      setLastAgentError("");
      toast.success("Git 同步完成: add -> commit -> push");
    } catch (err: any) {
      const message = formatLocalAgentError(err);
      setLastAgentError(message);
      toast.error("Git 操作失败: " + message);
    } finally {
      setLoading(false);
    }
  };

  const formatLocalAgentError = (error: unknown) => {
    if (error instanceof LocalAgentError) {
      return error.requestId ? `${error.message} [req:${error.requestId}]` : error.message;
    }
    if (error instanceof Error) {
      return error.message;
    }
    return "未知错误";
  };

  const shouldIgnoreLocalAgentError = (error: unknown) => {
    if (!(error instanceof LocalAgentError)) {
      return false;
    }
    if (error.code === "connection_closed" || error.code === "send_failed") {
      return true;
    }
    if (error.code === "connection_failed" && getLocalAgentState() !== "ready") {
      return true;
    }
    return false;
  };

  const fetchExperiments = async () => {
    if (!projectId || !repoPath) {
      toast.error("请选择项目并指定仓库路径");
      return;
    }
    setLoading(true);
    try {
      const res = await api.get(`/git/${projectId}/experiments`, {
        params: { repo_path: repoPath },
      });
      setExperiments(res.data.experiments || []);
      toast.success("实验记录加载完成");
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "加载实验记录失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchExperimentDetail = async (dirPath: string) => {
    if (!projectId) return;
    setLoading(true);
    try {
      const res = await api.get(`/git/${projectId}/experiment`, {
        params: { experiment_dir: dirPath },
      });
      setSelectedExperiment(res.data);
    } catch (err: any) {
      toast.error(err.response?.data?.detail || "加载实验详情失败");
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">实验和求解</h1>
          <p className="text-sm text-muted-foreground">
            连接本地 Agent，运行实验并管理结果
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 xl:grid-cols-4 gap-6">
        {/* 左侧 */}
        <div className="space-y-6">
          {/* Agent 状态 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                {connected ? (
                  <Wifi className="h-4 w-4 text-green-500" />
                ) : (
                  <WifiOff className="h-4 w-4 text-destructive" />
                )}
                Local Agent
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Badge variant={connected ? "default" : "destructive"}>
                  {connected ? "已连接" : "未连接"}
                </Badge>
                <span className="text-xs text-muted-foreground">
                  状态: {connectionState}
                </span>
              </div>
              {!connected && (
                <Button onClick={checkConnection} className="w-full">
                  <RefreshCw className="h-4 w-4 mr-1" />
                  重新连接
                </Button>
              )}
              {lastAgentError && (
                <p className="text-xs text-destructive break-all">
                  最近错误: {lastAgentError}
                </p>
              )}
            </CardContent>
          </Card>

          {/* 环境信息 */}
          {envInfo && (
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-base">本地环境</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <span className="text-muted-foreground">Python:</span>{" "}
                    {envInfo.python_version.split(" ")[0]}
                  </div>
                  <div>
                    <span className="text-muted-foreground">CPU:</span>{" "}
                    {envInfo.cpu_count} 核
                  </div>
                  <div>
                    <span className="text-muted-foreground">内存:</span>{" "}
                    {envInfo.memory_gb} GB
                  </div>
                  <div>
                    <span className="text-muted-foreground">Conda:</span>{" "}
                    {envInfo.conda_available ? "✅" : "❌"}
                  </div>
                  <div>
                    <span className="text-muted-foreground">GCC:</span>{" "}
                    {envInfo.gcc_available ? "✅" : "❌"}
                  </div>
                  <div>
                    <span className="text-muted-foreground">Git:</span>{" "}
                    {envInfo.git_available ? "✅" : "❌"}
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {/* Shell 执行 */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Terminal className="h-4 w-4" />
                Shell 执行
              </CardTitle>
            </CardHeader>
            <CardContent>
              <form onSubmit={runShell} className="space-y-3">
                <Input
                  placeholder="命令"
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                />
                <Input
                  placeholder="Git 仓库路径（Shell 执行范围）"
                  value={repoPath}
                  onChange={(e) => setRepoPath(e.target.value)}
                />
                <Button
                  type="submit"
                  disabled={!connected || loading}
                  variant="secondary"
                  className="w-full"
                >
                  执行
                </Button>
              </form>
            </CardContent>
          </Card>
        </div>

        {/* 右侧 */}
        <div className="lg:col-span-2 xl:col-span-3">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full mb-4">
              <TabsTrigger value="run">
                <Play className="h-4 w-4 mr-1" />
                运行实验
              </TabsTrigger>
              <TabsTrigger value="history">
                <FolderOpen className="h-4 w-4 mr-1" />
                实验记录
              </TabsTrigger>
            </TabsList>

            <TabsContent value="run" className="space-y-6 mt-0">
              {/* Git 集成 */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-base flex items-center gap-2">
                    <GitBranch className="h-4 w-4" />
                    Git 集成
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <Input
                    placeholder="Git 仓库路径"
                    value={repoPath}
                    onChange={(e) => setRepoPath(e.target.value)}
                  />
                  <div className="flex gap-2">
                    <Button
                      onClick={scanSolvers}
                      disabled={loading}
                      variant="outline"
                      className="flex-1"
                    >
                      扫描 Solver
                    </Button>
                    <Button
                      onClick={fetchGitLog}
                      disabled={loading}
                      variant="outline"
                      className="flex-1"
                    >
                      Git 日志
                    </Button>
                  </div>

                  {solvers.length > 0 && (
                    <div className="mt-4">
                      <Label className="text-sm">扫描到的 Solver 文件</Label>
                      <ScrollArea className="h-32 mt-2 border rounded-lg">
                        <div className="p-2 space-y-1">
                          {solvers.map((s, i) => (
                            <Button
                              key={i}
                              variant={selectedSolver === s.path ? "default" : "ghost"}
                              size="sm"
                              className="w-full justify-start text-xs h-auto py-1.5"
                              onClick={() => {
                                setSolverPath(s.path);
                                extractSolverParams(s.path);
                              }}
                            >
                              {s.rel_path}
                            </Button>
                          ))}
                        </div>
                      </ScrollArea>
                    </div>
                  )}

                  {extractedParams.length > 0 && (
                    <div className="mt-4">
                      <Label className="text-sm">提取到的参数</Label>
                      <div className="mt-2 space-y-1">
                        {extractedParams.map((p, i) => (
                          <div key={i} className="text-sm bg-muted p-2 rounded-lg">
                            {p.name}: {p.default} ({p.type})
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {gitCommits.length > 0 && (
                    <div className="mt-4">
                      <Label className="text-sm">最近提交</Label>
                      <ScrollArea className="h-32 mt-2">
                        <div className="space-y-1">
                          {gitCommits.map((c, i) => (
                            <div key={i} className="text-xs font-mono text-muted-foreground">
                              {c}
                            </div>
                          ))}
                        </div>
                      </ScrollArea>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* 自动化实验 */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-base flex items-center gap-2">
                    <FlaskConical className="h-4 w-4" />
                    自动化实验
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <Label>Solver 文件路径</Label>
                    <Input
                      placeholder="/path/to/solver.py"
                      value={solverPath}
                      onChange={(e) => setSolverPath(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>Git 仓库路径</Label>
                    <Input
                      placeholder="/path/to/repo"
                      value={repoPath}
                      onChange={(e) => setRepoPath(e.target.value)}
                    />
                  </div>

                  <div className="border rounded-lg p-4 space-y-4">
                    <h3 className="font-medium">参数网格</h3>

                    <div className="space-y-2">
                      <Label className="text-sm text-muted-foreground">
                        范围模式（起始、结束、步长）
                      </Label>
                      <div className="grid grid-cols-4 gap-2">
                        <Input
                          placeholder="参数名"
                          value={rangeName}
                          onChange={(e) => setRangeName(e.target.value)}
                        />
                        <Input
                          type="number"
                          placeholder="起始"
                          value={rangeStart}
                          onChange={(e) => setRangeStart(e.target.value)}
                        />
                        <Input
                          type="number"
                          placeholder="结束"
                          value={rangeEnd}
                          onChange={(e) => setRangeEnd(e.target.value)}
                        />
                        <Input
                          type="number"
                          placeholder="步长"
                          value={rangeStep}
                          onChange={(e) => setRangeStep(e.target.value)}
                        />
                      </div>
                      <Button onClick={addRangeParam} variant="outline" className="w-full">
                        添加范围参数
                      </Button>
                    </div>

                    <Separator />

                    <div className="space-y-2">
                      <Label className="text-sm text-muted-foreground">手动模式</Label>
                      <div className="flex gap-2">
                        <Input
                          placeholder="参数名"
                          value={paramName}
                          onChange={(e) => setParamName(e.target.value)}
                        />
                        <Input
                          placeholder="值，逗号分隔"
                          value={paramValues}
                          onChange={(e) => setParamValues(e.target.value)}
                        />
                        <Button onClick={addParam} variant="secondary">
                          添加
                        </Button>
                      </div>
                    </div>

                    {Object.entries(params).length > 0 && (
                      <div className="space-y-1">
                        {Object.entries(params).map(([name, vals]) => (
                          <div
                            key={name}
                            className="flex justify-between items-center bg-muted p-2 rounded-lg text-sm"
                          >
                            <span>
                              {name}: [{vals.join(", ")}]
                            </span>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-6 text-destructive hover:text-destructive"
                              onClick={() => removeParam(name)}
                            >
                              删除
                            </Button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  <Button
                    onClick={runExperiment}
                    disabled={!connected || loading}
                    className="w-full"
                  >
                    {loading ? (
                      <>
                        <Loader2 className="h-4 w-4 mr-1 animate-spin" />
                        执行中...
                      </>
                    ) : (
                      <>
                        <Play className="h-4 w-4 mr-1" />
                        开始实验
                      </>
                    )}
                  </Button>
                </CardContent>
              </Card>

              {/* 实验结果 */}
              {experimentResult && (
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-base">实验结果与同步</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="grid grid-cols-3 gap-4 text-sm">
                      <div>
                        <span className="text-muted-foreground">状态:</span>{" "}
                        <Badge variant={experimentResult.status === "success" ? "default" : "destructive"}>
                          {experimentResult.status}
                        </Badge>
                      </div>
                      <div>
                        <span className="text-muted-foreground">运行次数:</span>{" "}
                        {experimentResult.results?.length}
                      </div>
                      <div>
                        <span className="text-muted-foreground">任务状态:</span>{" "}
                        <Badge
                          variant={
                            experimentResult.task?.status === "succeeded"
                              ? "default"
                              : experimentResult.task?.status === "failed"
                                ? "destructive"
                                : "secondary"
                          }
                        >
                          {experimentResult.task?.status || "unknown"}
                        </Badge>
                      </div>
                    </div>
                    {experimentResult.task?.task_id && (
                      <div className="text-xs text-muted-foreground font-mono break-all">
                        task_id: {experimentResult.task.task_id}
                      </div>
                    )}

                    {resultFiles.length > 0 && (
                      <div>
                        <Label className="text-sm">结果文件</Label>
                        <ScrollArea className="h-32 mt-2 border rounded-lg">
                          <div className="p-2 space-y-1">
                            {resultFiles.map((f, i) => (
                              <div key={i} className="text-xs font-mono bg-muted p-1.5 rounded">
                                {f}
                              </div>
                            ))}
                          </div>
                        </ScrollArea>
                      </div>
                    )}

                    <div>
                      <Label className="text-sm">分析草稿 (analysis.md)</Label>
                      <Textarea
                        value={analysisContent}
                        onChange={(e) => setAnalysisContent(e.target.value)}
                        placeholder="在此撰写实验分析结论..."
                        className="mt-2 min-h-[120px]"
                      />
                      <Button onClick={saveAnalysis} variant="outline" size="sm" className="mt-2">
                        <Save className="h-3.5 w-3.5 mr-1" />
                        保存分析
                      </Button>
                    </div>

                    <Button
                      onClick={gitAddCommitPush}
                      disabled={!connected || loading}
                      variant="secondary"
                      className="w-full"
                    >
                      <Upload className="h-4 w-4 mr-1" />
                      Git Add → Commit → Push
                    </Button>
                    <p className="text-xs text-muted-foreground">
                      未 Push 的实验结果仅本地可见，Push 后团队成员可查看
                    </p>
                  </CardContent>
                </Card>
              )}

              {/* Shell 输出 */}
              {shellOutput && (
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-base">Shell 输出</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm font-mono max-h-96">
                      {shellOutput}
                    </pre>
                  </CardContent>
                </Card>
              )}

              {/* 实验结果列表 */}
              {experimentResult && (
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-base">运行详情</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2">
                    {experimentResult.results?.map((r: any, i: number) => (
                      <div
                        key={i}
                        className={`border rounded-lg p-3 text-sm ${
                          r.returncode === 0
                            ? "border-green-200 bg-green-50/50"
                            : "border-red-200 bg-red-50/50"
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          {r.returncode === 0 ? (
                            <CheckCircle2 className="h-4 w-4 text-green-500" />
                          ) : (
                            <AlertTriangle className="h-4 w-4 text-red-500" />
                          )}
                          <span className="font-medium">
                            运行 {i + 1}{" "}
                            {r.params && `(${JSON.stringify(r.params)})`}
                          </span>
                        </div>
                        <div className="text-muted-foreground mt-1">
                          返回码: {r.returncode}
                        </div>
                        {r.stdout && (
                          <pre className="mt-2 text-xs bg-background p-2 rounded border overflow-x-auto">
                            {r.stdout}
                          </pre>
                        )}
                      </div>
                    ))}
                  </CardContent>
                </Card>
              )}
            </TabsContent>

            <TabsContent value="history" className="mt-0">
              <Card>
                <CardHeader className="flex flex-row items-center justify-between pb-3">
                  <div>
                    <CardTitle className="text-base">实验记录</CardTitle>
                    <CardDescription>
                      展示本地 Git 仓库 results/ 目录下的所有实验记录
                    </CardDescription>
                  </div>
                  <Button onClick={fetchExperiments} disabled={loading} size="sm">
                    <RefreshCw className="h-4 w-4 mr-1" />
                    刷新
                  </Button>
                </CardHeader>
                <CardContent>
                  {experiments.length === 0 ? (
                    <div className="text-center py-12">
                      <FolderOpen className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
                      <p className="text-muted-foreground">暂无实验记录</p>
                      <p className="text-sm text-muted-foreground mt-1">
                        请先运行实验或确认 results/ 目录存在
                      </p>
                    </div>
                  ) : (
                    <div className="space-y-3">
                      {experiments.map((exp, i) => (
                        <div key={i} className="border rounded-lg overflow-hidden">
                          <button
                            onClick={() =>
                              selectedExperiment?.dir_path === exp.dir_path
                                ? setSelectedExperiment(null)
                                : fetchExperimentDetail(exp.dir_path)
                            }
                            className="w-full text-left p-4 hover:bg-muted/50 flex items-center justify-between transition-colors"
                          >
                            <div className="flex items-center gap-3">
                              <div
                                className={`w-2 h-2 rounded-full ${
                                  exp.structure?.is_complete
                                    ? "bg-green-500"
                                    : "bg-yellow-500"
                                }`}
                              />
                              <div>
                                <div className="font-medium">{exp.solver_name}</div>
                                <div className="text-sm text-muted-foreground">
                                  {exp.timestamp}
                                </div>
                              </div>
                            </div>
                            <div className="flex items-center gap-2">
                              {!exp.structure?.is_complete && (
                                <Badge variant="destructive" className="text-xs">
                                  结构异常
                                </Badge>
                              )}
                              {selectedExperiment?.dir_path === exp.dir_path ? (
                                <ChevronUp className="h-4 w-4 text-muted-foreground" />
                              ) : (
                                <ChevronDown className="h-4 w-4 text-muted-foreground" />
                              )}
                            </div>
                          </button>

                          {selectedExperiment?.dir_path === exp.dir_path && (
                            <div className="border-t p-4 space-y-4">
                              {!selectedExperiment.structure?.is_complete && (
                                <Alert variant="warning">
                                  <AlertTriangle className="h-4 w-4" />
                                  <AlertTitle>目录结构异常</AlertTitle>
                                  <AlertDescription>
                                    缺少 {selectedExperiment.structure?.missing?.join(", ")}
                                  </AlertDescription>
                                </Alert>
                              )}

                              <div className="grid grid-cols-2 gap-4 text-sm">
                                <div>
                                  <span className="text-muted-foreground">目录:</span>{" "}
                                  {selectedExperiment.dir_name}
                                </div>
                                <div>
                                  <span className="text-muted-foreground">Solver:</span>{" "}
                                  {selectedExperiment.solver_name}
                                </div>
                              </div>

                              {Object.keys(selectedExperiment.params_snapshot || {}).length > 0 && (
                                <div>
                                  <h4 className="font-medium mb-2 text-sm">参数快照</h4>
                                  <pre className="bg-muted p-3 rounded-lg text-xs overflow-x-auto">
                                    {JSON.stringify(selectedExperiment.params_snapshot, null, 2)}
                                  </pre>
                                </div>
                              )}

                              {selectedExperiment.fig_files?.length > 0 && (
                                <div>
                                  <h4 className="font-medium mb-2 text-sm">图表文件</h4>
                                  <div className="space-y-1">
                                    {selectedExperiment.fig_files.map((fig: string, fi: number) => (
                                      <div key={fi} className="text-sm font-mono bg-muted p-2 rounded-lg">
                                        fig/{fig}
                                      </div>
                                    ))}
                                  </div>
                                </div>
                              )}

                              {selectedExperiment.log && (
                                <div>
                                  <h4 className="font-medium mb-2 text-sm">运行日志</h4>
                                  <ScrollArea className="h-48 border rounded-lg">
                                    <pre className="p-3 text-xs font-mono">
                                      {selectedExperiment.log}
                                    </pre>
                                  </ScrollArea>
                                </div>
                              )}

                              {selectedExperiment.analysis && (
                                <div>
                                  <h4 className="font-medium mb-2 text-sm">分析草稿</h4>
                                  <div className="bg-muted p-3 rounded-lg text-sm whitespace-pre-wrap">
                                    {selectedExperiment.analysis}
                                  </div>
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        </div>
      </div>
    </>
  );
}
