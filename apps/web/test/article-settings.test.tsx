import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "project-1",
    name: "Project",
    role: "owner",
  }),
}));

import { ArticleSettingsPanel } from "@/features/article/article-settings-panel";
import { apiClient } from "@/lib/api-client";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Article Zotero settings", () => {
  it("keeps the encrypted key redacted while saving and testing all registered fields", async () => {
    const request = vi
      .spyOn(apiClient, "request")
      .mockImplementation(async (path, options) => {
        if (path.endsWith("/settings/article.zotero/test")) {
          return {
            checked_at: "2026-08-19T00:00:00Z",
            checks: [{ name: "library", status: "passed" }],
            status: "passed",
          } as never;
        }
        if (path.endsWith("/settings/article.zotero")) {
          if (options?.method === "PATCH")
            return { ...setting(), version: 3 } as never;
          return setting() as never;
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });

    render(
      <TestQueryProvider>
        <ArticleSettingsPanel />
      </TestQueryProvider>,
    );

    expect(await screen.findByDisplayValue("12345")).toBeInTheDocument();
    expect(screen.getByLabelText("Zotero API Key")).toHaveAttribute(
      "placeholder",
      "已加密配置；留空保持原值",
    );
    const testButton = screen.getByRole("button", { name: "保存并测试" });
    await waitFor(() => expect(testButton).toBeEnabled());
    fireEvent.click(testButton);

    expect(await screen.findByText("连接测试：passed")).toBeInTheDocument();
    const patch = request.mock.calls.find(
      ([path, options]) =>
        path.endsWith("/settings/article.zotero") &&
        options?.method === "PATCH",
    );
    expect(patch?.[1]?.body).toEqual({
      values: {
        api_key: "********",
        collection_key: "ABC",
        library_id: "12345",
        library_type: "user",
      },
    });
    expect(screen.queryByDisplayValue("********")).not.toBeInTheDocument();
  });
});

function setting() {
  return {
    updated_at: "2026-08-19T00:00:00Z",
    values: {
      api_key: "********",
      collection_key: "ABC",
      library_id: "12345",
      library_type: "user",
    },
    version: 2,
  };
}

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
