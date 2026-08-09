import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ModelListPage } from "@/features/model/model-list-page";
import { ModelQuestionPage } from "@/features/model/model-question-page";
import { ModelSettingsPanel } from "@/features/model/model-settings-panel";

const apiRequest = vi.hoisted(() => vi.fn());
const projectId = "00000000-0000-4000-8000-000000000001";
const questionId = "00000000-0000-4000-8000-000000000003";
const firstSnapshot = "00000000-0000-4000-8000-000000000004";
const secondSnapshot = "00000000-0000-4000-8000-000000000005";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: projectId, name: "Model Project", role: "owner" }),
}));

vi.mock("@/lib/api-client", () => ({ apiClient: { request: apiRequest } }));

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

describe("Model pages", () => {
  afterEach(() => { cleanup(); apiRequest.mockReset(); });

  it("renders one full-width card per bound question", async () => {
    apiRequest.mockResolvedValue({
      project_id: projectId,
      generated_at: "2026-08-09T00:00:00Z",
      configured: true,
      source: { source_id: "source", project_id: projectId, notion_root_page_id: "root", notion_root_page_url: "https://www.notion.so/root", notion_root_title: "模型", auto_sync_enabled: true, auto_sync_interval_seconds: 300, next_sync_at: "2026-08-09T00:05:00Z", sync_status: "succeeded", discovered_page_count: 1 },
      discovered_pages: [],
      questions: [{ question_id: questionId, project_id: projectId, code: "Q1", title: "人口模型", notion_page_id: "page", notion_page_url: "https://www.notion.so/page", position: 0, snapshot_count: 2, sync_status: "succeeded", created_at: "2026-08-09T00:00:00Z", updated_at: "2026-08-09T00:00:00Z" }],
    });

    render(<ModelListPage />, { wrapper: Providers });

    expect(await screen.findByText("人口模型")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Q1/ })).toHaveAttribute("href", `/projects/${projectId}/models/${questionId}`);
  });

  it("defaults new question codes to the next Q number and allows a blank code", async () => {
    const pageId = "00000000-0000-4000-8000-000000000006";
    const overview = {
      project_id: projectId,
      generated_at: "2026-08-09T00:00:00Z",
      configured: true,
      source: { source_id: "source", project_id: projectId, notion_root_page_id: "root", notion_root_page_url: "https://www.notion.so/root", notion_root_title: "模型", auto_sync_enabled: true, auto_sync_interval_seconds: 300, sync_status: "succeeded", discovered_page_count: 1 },
      discovered_pages: [{ notion_page_id: pageId, title: "人口模型", url: "https://www.notion.so/page", depth: 1, has_children: false }],
      questions: [{ question_id: questionId, project_id: projectId, code: "Q1", title: "已有模型", notion_page_id: "old-page", notion_page_url: "https://www.notion.so/old-page", position: 0, snapshot_count: 0, sync_status: "idle", created_at: "2026-08-09T00:00:00Z", updated_at: "2026-08-09T00:00:00Z" }],
    };
    apiRequest.mockImplementation(async (path: string, options?: { body?: unknown; method?: string }) => {
      if (path.endsWith("/questions") && options?.method === "POST") return overview.questions[0];
      return overview;
    });

    render(<ModelListPage />, { wrapper: Providers });

    const codeInput = await screen.findByLabelText("题号");
    expect(codeInput).toHaveValue("Q2");
    fireEvent.change(codeInput, { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("题目标题"), { target: { value: "新模型" } });
    const createButton = screen.getByRole("button", { name: "新建题号" });
    expect(createButton).toBeEnabled();
    fireEvent.click(createButton);

    await waitFor(() => expect(apiRequest).toHaveBeenCalledWith(`/projects/${projectId}/models/questions`, {
      body: { code: "Q2", title: "新模型", notion_page_id: pageId, position: 1 },
      method: "POST",
    }));
  });

  it("renders contiguous character additions and deletions without line numbers", async () => {
    apiRequest.mockImplementation(async (path: string) => {
      const artifactVersion = { version_id: "version-1", artifact_id: "artifact-1", version_no: 1, storage_class: "object", filename: "结果图.png", sha256: "b".repeat(64), mime_type: "image/png", size_bytes: 1024, status: "available", available_at: "2026-08-09T00:05:00Z", git_reference: null, created_by: "user", created_at: "2026-08-09T00:05:00Z" };
      const artifactDetail = { artifact: { artifact_id: "artifact-1", project_id: projectId, kind: "model_file", source: "model", source_object_id: questionId, tags: ["模型文件"], name: "结果图.png", description: "Notion 模型图片", recommended_usage: [], current_version_id: "version-1", status: "available", created_by: "user", trashed_at: null, created_at: "2026-08-09T00:05:00Z", updated_at: "2026-08-09T00:05:00Z" }, current_version: artifactVersion };
      if (path.endsWith("/artifacts/artifact-1/versions/version-1/download")) return { artifact_id: "artifact-1", version_id: "version-1", filename: "结果图.png", mime_type: "image/png", size_bytes: 1024, transfer: { method: "GET", url: "/api/artifact-transfers/token.signature", headers: {}, expires_at: "2026-08-09T00:10:00Z" } };
      if (path.endsWith("/artifacts/artifact-1/versions/version-1/previews")) return { items: [] };
      if (path.endsWith("/artifacts/artifact-1/versions")) return { items: [artifactVersion] };
      if (path.endsWith("/artifacts/artifact-1")) return artifactDetail;
      if (path.endsWith("/diff")) {
        return { question_id: questionId, from_snapshot_id: firstSnapshot, to_snapshot_id: secondSnapshot, granularity: "character", blocks: [{ block_id: "heading", type: "heading_2", change: "unchanged", block: { block_id: "heading", type: "heading_2", text: "模型假设", level: 2, rich_text: [{ text: "模型假设" }], children: [] }, operations: [{ kind: "unchanged", text: "模型假设" }] }, { block_id: "p1", type: "paragraph", change: "modified", block: { block_id: "p1", type: "paragraph", text: "人口快速增长", rich_text: [{ text: "人口快速增长" }], children: [] }, operations: [{ kind: "unchanged", text: "人口" }, { kind: "deleted", text: "稳定" }, { kind: "added", text: "快速" }, { kind: "unchanged", text: "增长" }] }] };
      }
      return {
        question: { question_id: questionId, project_id: projectId, code: "Q1", title: "人口模型", notion_page_id: "page", notion_page_url: "https://www.notion.so/page", position: 0, latest_snapshot_id: secondSnapshot, snapshot_count: 2, sync_status: "succeeded", created_at: "2026-08-09T00:00:00Z", updated_at: "2026-08-09T00:00:00Z" },
        latest_snapshot: { snapshot_id: secondSnapshot, question_id: questionId, previous_snapshot_id: firstSnapshot, title: "人口模型", content_hash: "a".repeat(64), summary: "", tags: [], captured_at: "2026-08-09T00:05:00Z", triggered_by: "user", created_at: "2026-08-09T00:05:00Z", metadata_updated_at: "2026-08-09T00:05:00Z", project_id: projectId, notion_page_id: "page", notion_page_url: "https://www.notion.so/page", outline: [{ block_id: "heading-current", title: "状态定义", level: 2 }, { block_id: "heading-ignored", title: "四级标题", level: 4 }], blocks: [{ block_id: "heading-current", type: "heading_2", text: "状态定义", level: 2, rich_text: [{ text: "状态定义" }], children: [] }, { block_id: "p1", type: "paragraph", text: "人口快速增长 P(t)", rich_text: [{ text: "人口快速增长 " }, { text: "P(t)", expression: "P(t)" }], children: [] }, { block_id: "equation-1", type: "equation", text: "P(t)=P_0e^{rt}", expression: "P(t)=P_0e^{rt}", rich_text: [], children: [] }, { block_id: "table-1", type: "table", text: "", rich_text: [], children: [{ block_id: "row-1", type: "table_row", text: "参数\ts_t", rich_text: [], rows: [["参数", "s_t"]], cells: [[{ text: "参数" }], [{ text: "", expression: "s_t" }]], children: [] }] }, { block_id: "bookmark-1", type: "bookmark", text: "mmdash fork", rich_text: [], url: "https://github.com/imouup/mmdash-fork/tree/main", caption: "mmdash fork", children: [] }, { block_id: "synced-1", type: "synced_block", text: "", rich_text: [], children: [{ block_id: "synced-p", type: "paragraph", text: "同步区块内容", rich_text: [{ text: "同步区块内容" }], children: [] }] }, { block_id: "image-1", type: "image", text: "", rich_text: [], artifact_id: "artifact-1", artifact_version_id: "version-1", caption: "结果图", children: [] }, { block_id: "file-1", type: "file", text: "", rich_text: [], artifact_id: "artifact-2", artifact_version_id: "version-2", caption: "模型说明书", children: [] }], content_markdown: "人口快速增长", content_text: "人口快速增长", assets: [{ source_block_id: "image-1", artifact_id: "artifact-1", artifact_version_id: "version-1", filename: "结果图.png", mime_type: "image/png" }, { source_block_id: "file-1", artifact_id: "artifact-2", artifact_version_id: "version-2", filename: "说明.docx", mime_type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }] },
        snapshots: [{ snapshot_id: secondSnapshot, question_id: questionId, previous_snapshot_id: firstSnapshot, title: "人口模型", content_hash: "a".repeat(64), summary: "", tags: [], captured_at: "2026-08-09T00:05:00Z", triggered_by: "user", created_at: "2026-08-09T00:05:00Z", metadata_updated_at: "2026-08-09T00:05:00Z" }, { snapshot_id: firstSnapshot, question_id: questionId, title: "人口模型", content_hash: "b".repeat(64), summary: "", tags: [], captured_at: "2026-08-09T00:00:00Z", triggered_by: "user", created_at: "2026-08-09T00:00:00Z", metadata_updated_at: "2026-08-09T00:00:00Z" }],
      };
    });

    const { container } = render(<ModelQuestionPage questionId={questionId} />, { wrapper: Providers });
    await waitFor(() => expect(container.querySelector("td")?.textContent).toContain("参数"));
    expect(container.querySelector('[data-model-equation="inline"] .katex')).toBeInTheDocument();
    expect(container.querySelector('[data-model-equation="block"] .katex-display')).toBeInTheDocument();
    expect(document.querySelector("td .katex")).toBeInTheDocument();
    const outline = container.querySelector('nav[aria-label="文档目录"]');
    expect(outline?.parentElement).toHaveClass("overflow-y-auto");
    expect(outline?.parentElement?.parentElement).toHaveClass("max-h-[calc(100vh-2rem)]");
    expect(container.querySelector('a[href="#model-heading-heading-current"]')).toHaveTextContent("状态定义");
    expect(container.querySelector('#model-heading-heading-current')).toHaveTextContent("状态定义");
    expect(screen.queryByText("四级标题")).not.toBeInTheDocument();
    expect(screen.getByText("mmdash fork").closest("a")).toHaveAttribute("href", "https://github.com/imouup/mmdash-fork/tree/main");
    expect(screen.getByText("同步区块内容")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector('img[alt="结果图"]')).toHaveAttribute("src", "/api/artifact-transfers/token.signature"));
    expect(screen.getByText("模型说明书").closest("button")).toBeInTheDocument();
    fireEvent.click(container.querySelector('button[aria-label="查看图片 Artifact：结果图"]')!);
    await waitFor(() => expect(container.querySelector('[role="dialog"][aria-label="文件详情"]')).toBeInTheDocument());
    expect(await screen.findByText("结果图.png", { selector: "h2" })).toBeInTheDocument();
    fireEvent.click(container.querySelector('button[aria-label="关闭"]')!);
    fireEvent.click((await screen.findByText("比较版本")).closest("button")!);

    expect(container.querySelector('select[aria-label="Diff 基准版本"]')).toHaveValue(firstSnapshot);
    expect(container.querySelector('select[aria-label="Diff 目标版本"]')).toHaveValue(secondSnapshot);
    await waitFor(() => expect(apiRequest).toHaveBeenCalledWith(`/projects/${projectId}/models/questions/${questionId}/diff`, {
      query: { from_snapshot_id: firstSnapshot, to_snapshot_id: secondSnapshot },
    }));
    await waitFor(() => expect(container.querySelector('[data-diff-change]')).toBeInTheDocument());
    expect(container.querySelector('h2 [data-diff-kind="unchanged"]')?.textContent).toBe("模型假设");
    expect(await screen.findByText("稳定")).toHaveClass("line-through");
    expect(screen.getByText("快速")).toHaveClass("bg-blue-100");
    expect(screen.queryByText(/^\d+$/)).not.toBeInTheDocument();
  });

  it("binds the single Notion source while preserving a redacted token and saving the interval", async () => {
    const settingPath = `/projects/${projectId}/settings/model.notion`;
    apiRequest.mockImplementation(async (path: string, options?: { body?: unknown; method?: string }) => {
      if (path === settingPath && options?.method === "PATCH") {
        return { values: (options.body as { values: Record<string, unknown> }).values, version: 3, updated_at: "2026-08-09T00:00:00Z" };
      }
      if (path === settingPath) {
        return {
          values: { integration_token: "********", root_page_url: "https://www.notion.so/Test-00000000000040008000000000000001", auto_sync_enabled: true, auto_sync_interval_seconds: 300 },
          version: 2,
          updated_at: "2026-08-09T00:00:00Z",
        };
      }
      if (path.endsWith("/models")) {
        return {
          project_id: projectId,
          configured: true,
          generated_at: "2026-08-09T00:00:00Z",
          discovered_pages: [],
          questions: [],
          source: { source_id: "source", project_id: projectId, notion_root_page_id: "root", notion_root_page_url: "https://www.notion.so/root", notion_root_title: "Test", auto_sync_enabled: true, auto_sync_interval_seconds: 300, next_sync_at: new Date(Date.now() + 300_000).toISOString(), sync_status: "succeeded", discovered_page_count: 0 },
        };
      }
      throw new Error(`unexpected request ${path}`);
    });

    render(<ModelSettingsPanel />, { wrapper: Providers });

    expect(await screen.findByDisplayValue("https://www.notion.so/Test-00000000000040008000000000000001")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("自动同步间隔（分钟）"), { target: { value: "10" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并绑定" }));

    await waitFor(() => expect(apiRequest).toHaveBeenCalledWith(settingPath, {
      body: { values: { root_page_url: "https://www.notion.so/Test-00000000000040008000000000000001", auto_sync_enabled: true, auto_sync_interval_seconds: 600, integration_token: "********" } },
      method: "PATCH",
    }));
    expect(screen.getByText(/自动同步倒计时/)).toBeInTheDocument();
  });
});
