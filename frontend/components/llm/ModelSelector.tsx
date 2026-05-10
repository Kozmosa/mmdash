import React, { useState, useEffect } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { llmApi } from "@/lib/api";
import { AlertCircle, Check, Loader2, Pencil } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";

interface LLMModel {
  id: string;
  description?: string;
}

interface ModelSelectorProps {
  teamId?: string;
  canManageTeamLlm?: boolean;
  teamName?: string;
  onModelSelected?: (bindingId: string, modelName: string) => void;
}

interface CurrentBinding {
  id: string;
  provider_type: string;
  team_id?: string | null;
  selected_model?: string | null;
  has_api_key: boolean;
}

interface ScopeState {
  binding: CurrentBinding | null;
  bindingId: string;
  provider: string;
  models: LLMModel[];
  selectedModel: string;
  configured: boolean;
  loading: boolean;
  error: string;
  success: string;
}

const emptyScopeState = (): ScopeState => ({
  binding: null,
  bindingId: "",
  provider: "openai",
  models: [],
  selectedModel: "",
  configured: false,
  loading: false,
  error: "",
  success: "",
});

export function ModelSelector({ teamId, canManageTeamLlm = false, teamName, onModelSelected }: ModelSelectorProps) {
  const [providers, setProviders] = useState<string[]>([]);
  const [selectedProvider, setSelectedProvider] = useState<string>("openai");
  const [apiKey, setApiKey] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>("");
  const [success, setSuccess] = useState<string>("");
  const [teamEditing, setTeamEditing] = useState(false)
  const [personalEditing, setPersonalEditing] = useState(false)
  const [teamScope, setTeamScope] = useState<ScopeState>(emptyScopeState());
  const [personalScope, setPersonalScope] = useState<ScopeState>(emptyScopeState());

  const teamLabel = teamName || "当前团队";

  // Load providers on mount
  useEffect(() => {
    void initialize();
  }, [teamId]);

  async function initialize() {
    setLoading(true);
    setError("");
    try {
      const provs: string[] = await llmApi.getProviders();
      setProviders(provs);

      const tryProviders = provs.includes("openai") ? ["openai", ...provs.filter((p: string) => p !== "openai")] : provs;

      let foundTeamBinding: CurrentBinding | null = null;
      let foundPersonalBinding: CurrentBinding | null = null;

      for (const p of tryProviders) {
        if (!foundTeamBinding && teamId) {
          const binding = await llmApi.getCurrentBinding(p, teamId);
          if (binding) {
            foundTeamBinding = binding;
          }
        }
        if (!foundPersonalBinding) {
          const binding = await llmApi.getCurrentBinding(p);
          if (binding) {
            foundPersonalBinding = binding;
          }
        }
      }

      if (foundTeamBinding) {
        const modelsList = await llmApi.getModels(foundTeamBinding.id, foundTeamBinding.provider_type, teamId);
        setTeamScope({
          binding: foundTeamBinding,
          bindingId: foundTeamBinding.id,
          provider: foundTeamBinding.provider_type,
          models: modelsList,
          selectedModel: foundTeamBinding.selected_model || "",
          configured: true,
          loading: false,
          error: "",
          success: "",
        });
        setSelectedProvider(foundTeamBinding.provider_type);
        setTeamEditing(false);
      } else {
        setTeamScope(emptyScopeState());
        setTeamEditing(teamId ? teamCanEdit : false);
      }

      if (foundPersonalBinding) {
        const modelsList = await llmApi.getModels(foundPersonalBinding.id, foundPersonalBinding.provider_type);
        setPersonalScope({
          binding: foundPersonalBinding,
          bindingId: foundPersonalBinding.id,
          provider: foundPersonalBinding.provider_type,
          models: modelsList,
          selectedModel: foundPersonalBinding.selected_model || "",
          configured: true,
          loading: false,
          error: "",
          success: "",
        });
        if (!foundTeamBinding) {
          setSelectedProvider(foundPersonalBinding.provider_type);
        }
        setPersonalEditing(false);
      } else {
        setPersonalScope(emptyScopeState());
        setPersonalEditing(true);
      }

    } catch (err: any) {
      setError("初始化失败: " + (err.response?.data?.detail || err.message || "未知错误"));
      setTeamEditing(teamId ? teamCanEdit : false);
      setPersonalEditing(true);
    } finally {
      setLoading(false);
    }
  }

  async function handleTestAndList(scope: "team" | "personal") {
    if (!apiKey.trim()) {
      setError("请输入 API Key");
      return;
    }

    setLoading(true);
    setError("");
    setSuccess("");

    try {
      // Create binding
      const shouldUseTeamScope = scope === "team" && !!teamId && canManageTeamLlm;
      const bindingRes = await llmApi.createBinding(
        selectedProvider,
        { api_key: apiKey },
        shouldUseTeamScope ? teamId : undefined
      );
      const modelsList = await llmApi.getModels(bindingRes.binding_id, selectedProvider, teamId);
      const nextScope = {
        binding: {
          id: bindingRes.binding_id,
          provider_type: selectedProvider,
          team_id: teamId || null,
          selected_model: "",
          has_api_key: true,
        },
        bindingId: bindingRes.binding_id,
        provider: selectedProvider,
        models: modelsList,
        selectedModel: "",
        configured: true,
        loading: false,
        error: "",
        success: "",
      } satisfies ScopeState;

      if (shouldUseTeamScope) {
        setTeamScope(nextScope);
        setTeamEditing(false);
      } else {
        setPersonalScope(nextScope);
        setPersonalEditing(false);
      }
      setSuccess(`已获取 ${modelsList.length} 个 ${selectedProvider} 模型，请选择一个。`);
    } catch (err: any) {
      setError(
        "获取模型列表失败: " +
          (err.response?.data?.detail || err.message || "未知错误")
      );
    } finally {
      setLoading(false);
    }
  }

  async function handleSelectModel(scope: "team" | "personal") {
    const activeScope = scope === "team" ? teamScope : personalScope;
    if (!activeScope.bindingId || !activeScope.selectedModel) {
      setError("请先完成提供商配置并选择模型");
      return;
    }

    setLoading(true);
    setError("");

    try {
      await llmApi.selectModel(activeScope.bindingId, activeScope.selectedModel);
      const nextBinding = activeScope.binding
        ? {
            ...activeScope.binding,
            selected_model: activeScope.selectedModel,
          }
        : activeScope.binding;
      const nextScope = {
        ...activeScope,
        binding: nextBinding,
      };
      if (scope === "team") {
        setTeamScope(nextScope);
      } else {
        setPersonalScope(nextScope);
      }
      setSuccess(`已切换到模型「${activeScope.selectedModel}」`);
      onModelSelected?.(activeScope.bindingId, activeScope.selectedModel);
    } catch (err: any) {
      setError(
        "保存模型选择失败: " +
          (err.response?.data?.detail || err.message || "未知错误")
      );
    } finally {
      setLoading(false);
    }
  }

  function handleEditConfig(scope: "team" | "personal") {
    if (scope === "team") {
      setTeamEditing(true);
    } else {
      setPersonalEditing(true);
    }
    setApiKey("");
    setError("");
    setSuccess("");
  }

  const activeTeamModel = teamScope.binding?.selected_model || teamScope.selectedModel;
  const activePersonalModel = personalScope.binding?.selected_model || personalScope.selectedModel;
  const teamCanEdit = !teamId || canManageTeamLlm;
  const activeScopeLabel = activePersonalModel ? "个人" : activeTeamModel ? "团队" : "未配置";
  const activeScopeModel = activePersonalModel || activeTeamModel || "未选择";

  return (
    <Card className="w-full">
      <CardHeader>
        <CardTitle>LLM 模型配置</CardTitle>
        <CardDescription>
          配置并选择你要使用的 LLM 模型{teamId ? `（${teamLabel}）` : ""}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="rounded-md border p-3 space-y-1 bg-muted/20">
          <div className="text-sm text-muted-foreground">当前使用</div>
          <div className="font-medium">
            {activeScopeLabel} 模型：{activeScopeModel}
          </div>
          <div className="text-sm text-muted-foreground">
            优先使用个人模型，否则使用团队模型
          </div>
        </div>

        {loading && (
          <div className="text-sm text-muted-foreground flex items-center">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            加载中...
          </div>
        )}
        {teamId && !teamCanEdit && (
          <div className="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
            团队模型由 owner/admin 管理，你只能配置个人模型。
          </div>
        )}

        {teamId && teamCanEdit && (
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="font-medium">团队模型设置</div>
                <div className="text-sm text-muted-foreground">
                  owner/admin 可见并可编辑
                </div>
              </div>
            </div>

            {teamEditing || !teamScope.configured ? (
              <>
                <div className="space-y-2">
                  <label className="text-sm font-medium">提供商</label>
                  <Select value={selectedProvider} onValueChange={setSelectedProvider}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {providers.map((p) => (
                        <SelectItem key={p} value={p}>
                          {p.charAt(0).toUpperCase() + p.slice(1)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <label className="text-sm font-medium">API Key</label>
                  <Input
                    type="password"
                    placeholder="请输入 API Key"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                  />
                </div>

                <Button
                  onClick={() => void handleTestAndList("team")}
                  disabled={loading || !apiKey.trim()}
                  className="w-full"
                >
                  {loading ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      正在获取模型...
                    </>
                  ) : (
                    "获取可用模型"
                  )}
                </Button>

                {teamScope.models.length > 0 && (
                  <div className="space-y-2">
                    <label className="text-sm font-medium">选择模型</label>
                    <Select
                      value={teamScope.selectedModel}
                      onValueChange={(value) => setTeamScope((prev) => ({ ...prev, selectedModel: value }))}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="请选择模型..." />
                      </SelectTrigger>
                      <SelectContent>
                        {teamScope.models.map((model) => (
                          <SelectItem key={model.id} value={model.id}>
                            {model.id}
                            {model.description && ` - ${model.description}`}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}

                {teamScope.models.length > 0 && (
                  <Button
                    onClick={() => void handleSelectModel("team")}
                    disabled={loading || !teamScope.selectedModel}
                    className="w-full"
                  >
                    {loading ? (
                      <>
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        保存中...
                      </>
                    ) : (
                      "保存团队模型"
                    )}
                  </Button>
                )}

                <Button variant="outline" onClick={() => handleEditConfig("team")} className="w-full">
                  <Pencil className="mr-2 h-4 w-4" />
                  编辑团队模型
                </Button>
              </>
            ) : (
              <>
                <div className="rounded-md border p-3 space-y-1 bg-muted/30">
                  <div className="text-sm text-muted-foreground">当前提供商</div>
                  <div className="font-medium">{teamScope.provider}</div>
                  <div className="text-sm text-muted-foreground">当前模型</div>
                  <div className="font-medium">{activeTeamModel || "未选择"}</div>
                  {!teamCanEdit && <div className="text-sm text-muted-foreground">由团队管理</div>}
                </div>

                {teamScope.models.length > 0 && (
                  <div className="space-y-2">
                    <div className="text-sm font-medium">可用模型列表</div>
                    <div className="max-h-64 overflow-auto rounded-md border">
                      {teamScope.models.map((model) => {
                        const isActive = activeTeamModel === model.id;
                        return (
                          <button
                            type="button"
                            key={model.id}
                            className="w-full text-left px-3 py-2 border-b last:border-b-0 hover:bg-muted/40 disabled:cursor-not-allowed disabled:opacity-60"
                            onClick={() => setTeamScope((prev) => ({ ...prev, selectedModel: model.id }))}
                            disabled={!teamCanEdit}
                          >
                            <div className="flex items-center justify-between gap-2">
                              <div className="truncate">
                                <div className="font-medium">{model.id}</div>
                                {model.description && (
                                  <div className="text-xs text-muted-foreground truncate">{model.description}</div>
                                )}
                              </div>
                              {isActive && <Check className="h-4 w-4 text-green-600" />}
                            </div>
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )}

                <Button variant="outline" onClick={() => handleEditConfig("team")} className="w-full">
                  <Pencil className="mr-2 h-4 w-4" />
                  重新配置团队模型
                </Button>
              </>
            )}
          </div>
        )}

        <div className="space-y-3 rounded-md border p-3">
          <div>
            <div className="font-medium">个人模型设置</div>
            <div className="text-sm text-muted-foreground">仅你自己使用</div>
          </div>

          {personalEditing || !personalScope.configured ? (
            <>
              <div className="space-y-2">
                <label className="text-sm font-medium">提供商</label>
                <Select value={selectedProvider} onValueChange={setSelectedProvider}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {providers.map((p) => (
                      <SelectItem key={p} value={p}>
                        {p.charAt(0).toUpperCase() + p.slice(1)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">API Key</label>
                <Input
                  type="password"
                  placeholder="请输入 API Key"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                />
              </div>

              <Button
                onClick={() => void handleTestAndList("personal")}
                disabled={loading || !apiKey.trim()}
                className="w-full"
              >
                {loading ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    正在获取模型...
                  </>
                ) : (
                  "获取可用模型"
                )}
              </Button>

              {personalScope.models.length > 0 && (
                <div className="space-y-2">
                  <label className="text-sm font-medium">选择模型</label>
                  <Select
                    value={personalScope.selectedModel}
                    onValueChange={(value) => setPersonalScope((prev) => ({ ...prev, selectedModel: value }))}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="请选择模型..." />
                    </SelectTrigger>
                    <SelectContent>
                      {personalScope.models.map((model) => (
                        <SelectItem key={model.id} value={model.id}>
                          {model.id}
                          {model.description && ` - ${model.description}`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {personalScope.models.length > 0 && (
                <Button
                  onClick={() => void handleSelectModel("personal")}
                  disabled={loading || !personalScope.selectedModel}
                  variant="default"
                  className="w-full"
                >
                  {loading ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      保存中...
                    </>
                  ) : (
                    "保存个人模型"
                  )}
                </Button>
              )}
            </>
          ) : (
            <>
              <div className="rounded-md border p-3 space-y-1 bg-muted/30">
                <div className="text-sm text-muted-foreground">当前提供商</div>
                <div className="font-medium">{personalScope.provider}</div>
                <div className="text-sm text-muted-foreground">当前模型</div>
                <div className="font-medium">{activePersonalModel || "未选择"}</div>
              </div>

              {personalScope.models.length > 0 && (
                <div className="space-y-2">
                  <div className="text-sm font-medium">可用模型列表</div>
                  <div className="max-h-64 overflow-auto rounded-md border">
                    {personalScope.models.map((model) => {
                      const isActive = activePersonalModel === model.id;
                      return (
                        <button
                          type="button"
                          key={model.id}
                          className="w-full text-left px-3 py-2 border-b last:border-b-0 hover:bg-muted/40"
                          onClick={() => setPersonalScope((prev) => ({ ...prev, selectedModel: model.id }))}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <div className="truncate">
                              <div className="font-medium">{model.id}</div>
                              {model.description && (
                                <div className="text-xs text-muted-foreground truncate">{model.description}</div>
                              )}
                            </div>
                            {isActive && <Check className="h-4 w-4 text-green-600" />}
                          </div>
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}

              <div className="flex gap-2">
                <Button variant="outline" onClick={() => handleEditConfig("personal")} className="flex-1">
                  <Pencil className="mr-2 h-4 w-4" />
                  编辑个人模型
                </Button>
                {personalScope.models.length > 0 && (
                  <Button
                    onClick={() => void handleSelectModel("personal")}
                    disabled={loading || !personalScope.selectedModel}
                    className="flex-1"
                  >
                    {loading ? (
                      <>
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        保存中...
                      </>
                    ) : (
                      "保存个人模型"
                    )}
                  </Button>
                )}
              </div>
            </>
          )}
        </div>

        {/* Error Alert */}
        {error && (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {/* Success Alert */}
        {success && (
          <Alert className="border-green-600 bg-green-50">
            <AlertCircle className="h-4 w-4 text-green-600" />
            <AlertDescription className="text-green-800">{success}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
