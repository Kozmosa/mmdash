import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import HomePage from "../page";

vi.mock("@/lib/api", () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}));

const invalidateTeams = vi.fn();
const invalidateProjects = vi.fn();
const setTeams = vi.fn();
const setProjects = vi.fn();

vi.mock("@/stores/data-cache", () => ({
  useDataCache: () => ({
    getTeams: vi.fn(() => null),
    isTeamsStale: vi.fn(() => true),
    setTeams,
    getProjects: vi.fn(() => null),
    isProjectsStale: vi.fn(() => true),
    setProjects,
    invalidateTeams,
    invalidateProjects,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import api from "@/lib/api";

const mockedApi = api as unknown as {
  get: ReturnType<typeof vi.fn>;
  post: ReturnType<typeof vi.fn>;
  delete: ReturnType<typeof vi.fn>;
};

describe("HomePage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
  });

  it("shows the team provider step when opening create-project for a team that needs setup", async () => {
    mockedApi.get.mockImplementation((url: string) => {
      if (url === "/teams") {
        return Promise.resolve({ data: [{ id: "team-1", name: "Alpha", invite_code: "CODE" }] });
      }
      if (url === "/projects") {
        return Promise.resolve({ data: [] });
      }
      if (url === "/auth/provider/team/team-1") {
        return Promise.resolve({ data: { provider_type: "local_file", is_default: true } });
      }
      return Promise.resolve({ data: [] });
    });

    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: /创建项目/ }));

    await waitFor(() =>
      expect(screen.getByText("团队文档后端")).toBeInTheDocument()
    );
    expect(
      screen.getByText("该设置是团队级的，后续同团队项目会复用当前选择。")
    ).toBeInTheDocument();
  });

  it("stores pending project creation and starts notion auth flow when notion is selected", async () => {
    mockedApi.get.mockImplementation((url: string) => {
      if (url === "/teams") {
        return Promise.resolve({ data: [{ id: "team-1", name: "Alpha", invite_code: "CODE" }] });
      }
      if (url === "/projects") {
        return Promise.resolve({ data: [] });
      }
      if (url === "/auth/provider/team/team-1") {
        return Promise.resolve({ data: { provider_type: "local_file", is_default: true } });
      }
      if (url === "/auth/provider/url") {
        return Promise.resolve({ data: { auth_url: "https://notion.example.com/oauth" } });
      }
      return Promise.resolve({ data: [] });
    });

    mockedApi.post.mockImplementation((url: string) => {
      if (url === "/auth/provider/switch") {
        return Promise.resolve({ data: { status: "switched", provider_type: "notion" } });
      }
      return Promise.resolve({ data: {} });
    });

    const locationSpy = vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      href: "http://localhost/home",
    } as Location);
    const hrefSetter = vi.fn();
    vi.spyOn(window, "location", "set").mockImplementation((value: string | Location) => {
      hrefSetter(typeof value === "string" ? value : value.href);
    });

    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: /创建项目/ }));
    await waitFor(() => expect(screen.getByText("团队文档后端")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("combobox", { name: "团队文档后端" }));
    await waitFor(() => expect(screen.getAllByText("Notion").length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByText("Notion").at(-1)!);
    fireEvent.change(screen.getByLabelText("项目名称"), { target: { value: "Notion Project" } });
    fireEvent.click(screen.getAllByRole("button", { name: /创建项目/ }).at(-1)!);

    await waitFor(() => {
      expect(mockedApi.post).toHaveBeenCalledWith("/auth/provider/switch", {
        provider_type: "notion",
        team_id: "team-1",
      });
      expect(mockedApi.get).toHaveBeenCalledWith("/auth/provider/url");
    });

    expect(JSON.parse(sessionStorage.getItem("pending_project_creation") || "{}")).toMatchObject({
      teamId: "team-1",
      providerType: "notion",
      projectName: "Notion Project",
    });

    locationSpy.mockRestore();
  });

  it("selects the newly created project after successful creation", async () => {
    mockedApi.get.mockImplementation((url: string, config?: any) => {
      if (url === "/teams") {
        return Promise.resolve({ data: [{ id: "team-1", name: "Alpha", invite_code: "CODE" }] });
      }
      if (url === "/projects") {
        return Promise.resolve({
          data: config?.params?.team_id === "team-1"
            ? [{ id: "project-1", name: "Project One", description: null, git_remote_url: null }]
            : [],
        });
      }
      if (url === "/auth/provider/team/team-1") {
        return Promise.resolve({ data: { provider_type: "local_file", is_default: false } });
      }
      if (url === "/home/project-1/todos") {
        return Promise.resolve({ data: [] });
      }
      if (url === "/home/project-1/progress") {
        return Promise.resolve({
          data: {
            total_todos: 0,
            completed_todos: 0,
            completion_rate: 0,
            team: { total: 0, completed: 0, rate: 0 },
            personal: { total: 0, completed: 0, rate: 0 },
          },
        });
      }
      if (url === "/home/project-1/problems") {
        return Promise.resolve({ data: [] });
      }
      return Promise.resolve({ data: [] });
    });

    mockedApi.post.mockImplementation((url: string) => {
      if (url === "/projects") {
        return Promise.resolve({
          data: { id: "project-1", name: "Project One", description: null, git_remote_url: null },
        });
      }
      return Promise.resolve({ data: {} });
    });

    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: /创建项目/ }));
    await waitFor(() => expect(screen.getByLabelText("项目名称")).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText("项目名称"), { target: { value: "Project One" } });
    fireEvent.click(screen.getAllByRole("button", { name: /创建项目/ }).at(-1)!);

    await waitFor(() => {
      expect(mockedApi.get).toHaveBeenCalledWith("/home/project-1/todos");
      expect(mockedApi.get).toHaveBeenCalledWith("/home/project-1/progress");
      expect(mockedApi.get).toHaveBeenCalledWith("/home/project-1/problems");
    });
  });

  it("shows inline upload error text when upload fails", async () => {
    mockedApi.get.mockImplementation((url: string, config?: any) => {
      if (url === "/teams") {
        return Promise.resolve({ data: [{ id: "team-1", name: "Alpha", invite_code: "CODE" }] });
      }
      if (url === "/projects") {
        return Promise.resolve({
          data: config?.params?.team_id === "team-1"
            ? [{ id: "project-1", name: "Project One", description: null, git_remote_url: null }]
            : [],
        });
      }
      if (url === "/auth/provider/team/team-1") {
        return Promise.resolve({ data: { provider_type: "local_file", is_default: false } });
      }
      if (url === "/home/project-1/todos") {
        return Promise.resolve({ data: [] });
      }
      if (url === "/home/project-1/progress") {
        return Promise.resolve({
          data: {
            total_todos: 0,
            completed_todos: 0,
            completion_rate: 0,
            team: { total: 0, completed: 0, rate: 0 },
            personal: { total: 0, completed: 0, rate: 0 },
          },
        });
      }
      if (url === "/home/project-1/problems") {
        return Promise.resolve({ data: [] });
      }
      return Promise.resolve({ data: [] });
    });

    mockedApi.post.mockRejectedValue({
      response: { data: { detail: "Only PDF and TXT files are supported" } },
    });

    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement | null;
    expect(fileInput).not.toBeNull();
    const file = new File(["a,b\n1,2"], "bad.csv", { type: "text/csv" });
    fireEvent.change(fileInput!, { target: { files: [file] } });

    await waitFor(() =>
      expect(screen.getByText("上传失败：Only PDF and TXT files are supported")).toBeInTheDocument()
    );
  });
});
