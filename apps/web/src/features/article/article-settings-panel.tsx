"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cable, KeyRound, Save, Unplug } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { optionalRequest } from "@/features/repo/optional-request";
import { apiClient } from "@/lib/api-client";

import { ARTICLE_RENDER_THEME_EVENT, type ArticleRenderTheme } from "./types";

const settingType = "article.zotero";
const redactedSecret = "********";

type ArticleProjectSetting = {
  updated_at: string;
  values: Record<string, unknown>;
  version: number;
};

type ConnectionTest = {
  checked_at: string;
  checks: { message?: string; name: string; status: "passed" | "failed" }[];
  status: "passed" | "failed" | "unsupported";
};

export function ArticleSettingsPanel() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const path = `/projects/${encodeURIComponent(project.id)}/settings/${encodeURIComponent(settingType)}`;
  const renderingPath = `/projects/${encodeURIComponent(project.id)}/settings/article.rendering`;
  const [libraryType, setLibraryType] = useState<"user" | "group">("user");
  const [libraryId, setLibraryId] = useState("");
  const [collectionKey, setCollectionKey] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [renderTheme, setRenderTheme] = useState<ArticleRenderTheme>("md");
  const [testResult, setTestResult] = useState<ConnectionTest>();
  const initialized = useRef<string | undefined>(undefined);
  const configuredSecret = useRef(false);
  const setting = useQuery({
    queryFn: () => optionalRequest<ArticleProjectSetting>(apiClient, path),
    queryKey: ["article-zotero-setting", project.id],
    retry: false,
  });
  const renderingSetting = useQuery({
    queryFn: () =>
      optionalRequest<ArticleProjectSetting>(apiClient, renderingPath),
    queryKey: ["article-rendering-setting", project.id],
    retry: false,
  });

  useEffect(() => {
    setRenderTheme(
      renderingSetting.data?.values.theme === "latex" ? "latex" : "md",
    );
  }, [renderingSetting.data]);

  useEffect(() => {
    const key = setting.data ? String(setting.data.version) : "empty";
    if (setting.isPending || initialized.current === key) return;
    initialized.current = key;
    const values = setting.data?.values;
    configuredSecret.current = values?.api_key === redactedSecret;
    setLibraryType(values?.library_type === "group" ? "group" : "user");
    setLibraryId(
      typeof values?.library_id === "string" ? values.library_id : "",
    );
    setCollectionKey(
      typeof values?.collection_key === "string" ? values.collection_key : "",
    );
    setApiKey("");
  }, [setting.data, setting.isPending]);

  const configured = setting.data?.values.api_key === redactedSecret;
  const configurationLoaded =
    !setting.isPending && initialized.current !== undefined;
  const canManage = project.role === "owner" || project.role === "maintainer";
  const values = () => {
    if (!libraryId.trim() || (!apiKey.trim() && !configuredSecret.current)) {
      throw new Error("请填写 Zotero Library ID 和只读 API Key");
    }
    return {
      api_key: apiKey.trim() || redactedSecret,
      collection_key: collectionKey.trim() || null,
      library_id: libraryId.trim(),
      library_type: libraryType,
    };
  };
  const invalidate = async () => {
    initialized.current = undefined;
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["article-zotero-setting", project.id],
      }),
      queryClient.invalidateQueries({ queryKey: ["article", project.id] }),
      queryClient.invalidateQueries({
        queryKey: ["article-zotero", project.id],
      }),
    ]);
  };
  const saveSetting = async () => {
    const result = await apiClient.request<ArticleProjectSetting>(path, {
      body: { values: values() },
      method: "PATCH",
    });
    await invalidate();
    return result;
  };
  const save = useMutation({
    mutationFn: saveSetting,
    onSuccess: () => toast.success("Zotero 只读 Library 配置已保存"),
  });
  const test = useMutation({
    mutationFn: async () => {
      await saveSetting();
      return apiClient.request<ConnectionTest>(`${path}/test`, {
        method: "POST",
      });
    },
    onSuccess: (result) => {
      setTestResult(result);
      if (result.status === "passed") toast.success("Zotero 连接测试通过");
      else toast.error("Zotero 连接测试未通过");
    },
  });
  const disconnect = useMutation({
    mutationFn: () =>
      apiClient.request<void>(
        `/projects/${encodeURIComponent(project.id)}/article/zotero`,
        { method: "DELETE" },
      ),
    onSuccess: async () => {
      await invalidate();
      toast.success("已断开 Zotero；历史冻结引用仍保留");
    },
  });
  const saveRendering = useMutation({
    mutationFn: (theme: ArticleRenderTheme) =>
      apiClient.request<ArticleProjectSetting>(renderingPath, {
        body: { values: { theme } },
        method: "PATCH",
      }),
    onSuccess: async (result) => {
      const theme = result.values.theme === "latex" ? "latex" : "md";
      setRenderTheme(theme);
      await queryClient.invalidateQueries({
        queryKey: ["article-rendering-setting", project.id],
      });
      window.dispatchEvent(new Event(ARTICLE_RENDER_THEME_EVENT));
      toast.success("Article 渲染主题已保存到项目设置");
    },
  });
  const error = setting.error ?? save.error ?? test.error ?? disconnect.error;
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    save.mutate();
  }

  return (
    <section
      className="scroll-mt-6 space-y-4"
      id="article-settings"
      aria-labelledby="article-settings-title"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold" id="article-settings-title">
            Article · Zotero
          </h2>
          <p className="text-sm text-muted-foreground">
            绑定一个只读 Library，可选限制到
            Collection；提交时冻结具体条目版本。
          </p>
        </div>
        <Badge>{configured ? "已配置" : "未配置"}</Badge>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <KeyRound className="size-4" />
            Zotero Library 与密钥
          </CardTitle>
          <CardDescription>
            API Key 仅由 Core 加密保存，不进入 Git、Artifact、Release
            或浏览器响应。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={submit}>
            <label className="space-y-2 text-sm">
              <span className="font-medium">Library 类型</span>
              <select
                aria-label="Zotero Library 类型"
                className="h-9 w-full rounded-md border bg-background px-3"
                onChange={(event) =>
                  setLibraryType(event.target.value as "user" | "group")
                }
                value={libraryType}
              >
                <option value="user">User library</option>
                <option value="group">Group library</option>
              </select>
            </label>
            <label className="space-y-2 text-sm">
              <span className="font-medium">Library ID</span>
              <Input
                aria-label="Zotero Library ID"
                onChange={(event) => setLibraryId(event.target.value)}
                placeholder="例如 1234567"
                value={libraryId}
              />
            </label>
            <label className="space-y-2 text-sm">
              <span className="font-medium">Collection key（可选）</span>
              <Input
                aria-label="Zotero Collection key"
                onChange={(event) => setCollectionKey(event.target.value)}
                value={collectionKey}
              />
            </label>
            <label className="space-y-2 text-sm">
              <span className="font-medium">只读 API Key</span>
              <Input
                aria-label="Zotero API Key"
                autoComplete="new-password"
                onChange={(event) => setApiKey(event.target.value)}
                placeholder={
                  configured
                    ? "已加密配置；留空保持原值"
                    : "Zotero read-only API key"
                }
                type="password"
                value={apiKey}
              />
            </label>
            {error ? (
              <p className="text-sm text-destructive">{error.message}</p>
            ) : null}
            {!canManage ? (
              <p className="text-xs text-muted-foreground">
                只有 Owner 或 Maintainer 可以修改 Zotero 配置。
              </p>
            ) : null}
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={
                  !canManage ||
                  !configurationLoaded ||
                  save.isPending ||
                  test.isPending
                }
                type="submit"
              >
                <Save className="size-4" />
                保存
              </Button>
              <Button
                disabled={
                  !canManage ||
                  !configurationLoaded ||
                  save.isPending ||
                  test.isPending
                }
                onClick={() => test.mutate()}
                type="button"
                variant="outline"
              >
                <Cable className="size-4" />
                保存并测试
              </Button>
              {configured ? (
                <Button
                  disabled={!canManage || disconnect.isPending}
                  onClick={() => disconnect.mutate()}
                  type="button"
                  variant="outline"
                >
                  <Unplug className="size-4" />
                  断开
                </Button>
              ) : null}
            </div>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Markdown 渲染格式</CardTitle>
          <CardDescription>
            作为项目设置由 Core 持久化，所有浏览器和协作者共享同一默认主题。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <label className="space-y-2 text-sm">
            <span className="font-medium">渲染主题</span>
            <select
              aria-label="Article 渲染主题"
              className="h-9 w-full rounded-md border bg-background px-3"
              disabled={
                !canManage ||
                renderingSetting.isPending ||
                saveRendering.isPending
              }
              onChange={(event) =>
                saveRendering.mutate(event.target.value as ArticleRenderTheme)
              }
              value={renderTheme}
            >
              <option value="md">默认 md</option>
              <option value="latex">LaTeX 风格 md</option>
            </select>
          </label>
          <p className="text-xs text-muted-foreground">
            LaTeX
            风格使用衬线体、章节序号、三线表，并将图注置于图片下方、表注置于表格上方。
          </p>
        </CardContent>
      </Card>
      {testResult ? (
        <Card>
          <CardContent className="space-y-2 p-4">
            <p className="text-sm font-medium">连接测试：{testResult.status}</p>
            {testResult.checks.map((check) => (
              <div
                className="flex items-center justify-between gap-3 text-sm"
                key={check.name}
              >
                <span>{check.name}</span>
                <span
                  className={
                    check.status === "passed"
                      ? "text-emerald-600"
                      : "text-destructive"
                  }
                >
                  {check.message || check.status}
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      ) : null}
    </section>
  );
}
