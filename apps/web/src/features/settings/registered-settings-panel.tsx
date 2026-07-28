"use client";

import { useQuery } from "@tanstack/react-query";
import { Cable, KeyRound } from "lucide-react";

import { EmptyState } from "@/components/states/empty-state";
import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useCurrentProject } from "@/components/providers/project-provider";
import { apiClient } from "@/lib/api-client";

type SettingField = {
  description?: string;
  key: string;
  kind: "boolean" | "number" | "secret" | "select" | "string" | "url";
  label: string;
  options?: string[];
  required: boolean;
};

type SettingType = {
  description: string;
  fields: SettingField[];
  key: string;
  order: number;
  owner: string;
  scopes: ("project" | "system")[];
  test_supported: boolean;
  title: string;
};

export function RegisteredSettingsPanel() {
  const project = useCurrentProject();
  const query = useQuery({
    queryFn: () =>
      apiClient.request<{ items: SettingType[] }>(
        `/projects/${encodeURIComponent(project.id)}/settings/types`,
      ),
    queryKey: ["settings-types", project.id],
  });

  if (query.isLoading) {
    return <LoadingState label="正在读取配置类型…" />;
  }
  if (query.isError) {
    return (
      <ErrorState
        description="请检查 BFF 与 Core 的连接后重试。"
        onRetry={() => void query.refetch()}
        title="无法读取项目配置类型"
      />
    );
  }
  if (!query.data?.items.length) {
    return (
      <EmptyState
        description="具体 Git、Hermes、Notion、Zotero、通知、Box 与 MCP 配置将在所属模块注册后显示。"
        title="配置中心已就绪，暂无模块配置"
      />
    );
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {query.data.items.map((definition) => (
        <Card key={definition.key}>
          <CardHeader>
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-base">{definition.title}</CardTitle>
                <CardDescription>{definition.description}</CardDescription>
              </div>
              <code className="text-xs text-muted-foreground">
                {definition.owner}
              </code>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <ul className="space-y-2 text-sm">
              {definition.fields.map((field) => (
                <li
                  className="flex items-center justify-between gap-3"
                  key={field.key}
                >
                  <span className="inline-flex items-center gap-2">
                    {field.kind === "secret" ? (
                      <KeyRound aria-hidden="true" className="size-3.5" />
                    ) : (
                      <span className="size-3.5" aria-hidden="true" />
                    )}
                    {field.label}
                  </span>
                  <code className="text-xs text-muted-foreground">
                    {field.kind}
                    {field.required ? " · required" : ""}
                  </code>
                </li>
              ))}
            </ul>
            {definition.test_supported ? (
              <p className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                <Cable aria-hidden="true" className="size-3.5" />
                支持保存后连接测试
              </p>
            ) : null}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
