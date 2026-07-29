import type { components } from "./generated/core.js";

export type CoreRequestContext = {
  accessToken?: string;
  projectId?: string;
  requestId: string;
  userId?: string;
};

export type CoreRequestOptions = Omit<RequestInit, "body"> & {
  body?: BodyInit | Record<string, unknown> | null;
  duplex?: "half";
};

export type CoreErrorBody = {
  code?: string;
  message?: string;
  request_id?: string;
};

export class CoreClientError extends Error {
  readonly body: CoreErrorBody;
  readonly status: number;

  constructor(status: number, body: CoreErrorBody) {
    super(body.message ?? `Core request failed with HTTP ${status}`);
    this.name = "CoreClientError";
    this.status = status;
    this.body = body;
  }
}

export class CoreClient {
  readonly baseUrl: string;

  constructor(
    baseUrl: string,
    private readonly fetchImplementation: typeof fetch = fetch,
  ) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  async checkExample(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ExampleCheck"]> {
    return this.request<components["schemas"]["ExampleCheck"]>(
      "/v1/example",
      { method: "GET" },
      context,
    );
  }

  async login(
    credentials: components["schemas"]["LoginRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["LoginResult"]> {
    return this.request(
      "/v1/auth/login",
      {
        body: credentials,
        method: "POST",
      },
      context,
    );
  }

  async register(
    input: components["schemas"]["RegisterRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["LoginResult"]> {
    return this.request(
      "/v1/auth/register",
      { body: input, method: "POST" },
      context,
    );
  }

  async updateProfile(
    input: components["schemas"]["UpdateProfileRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["User"]> {
    return this.request(
      "/v1/auth/me",
      { body: input, method: "PATCH" },
      context,
    );
  }

  async changePassword(
    input: components["schemas"]["ChangePasswordRequest"],
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      "/v1/auth/me/password",
      { body: input, method: "POST" },
      context,
    );
  }

  async previewInvitation(
    token: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectInvitation"]> {
    return this.request(
      "/v1/auth/invitations/preview",
      { body: { token }, method: "POST" },
      context,
    );
  }

  async acceptInvitation(
    token: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectMember"]> {
    return this.request(
      "/v1/auth/invitations/accept",
      { body: { token }, method: "POST" },
      context,
    );
  }

  async rejectInvitation(
    token: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      "/v1/auth/invitations/reject",
      { body: { token }, method: "POST" },
      context,
    );
  }

  async logout(context: CoreRequestContext): Promise<void> {
    return this.request("/v1/auth/logout", { method: "POST" }, context);
  }

  async currentIdentity(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Identity"]> {
    return this.request("/v1/auth/me", { method: "GET" }, context);
  }

  async listProjects(
    context: CoreRequestContext,
    includeArchived = false,
  ): Promise<components["schemas"]["ProjectList"]> {
    return this.request(
      `/v1/projects?include_archived=${includeArchived}`,
      { method: "GET" },
      context,
    );
  }

  async createProject(
    input: components["schemas"]["CreateProjectRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Project"]> {
    return this.request(
      "/v1/projects",
      {
        body: input,
        method: "POST",
      },
      context,
    );
  }

  async listProjectTrash(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectList"]> {
    return this.request("/v1/projects/trash", { method: "GET" }, context);
  }

  async getProject(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Project"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateProject(
    projectId: string,
    input: components["schemas"]["UpdateProjectRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Project"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async trashProject(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async restoreProject(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Project"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/restore`,
      { method: "POST" },
      context,
    );
  }

  async listProjectMembers(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["MemberList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/members`,
      { method: "GET" },
      context,
    );
  }

  async updateProjectMember(
    projectId: string,
    userId: string,
    role: components["schemas"]["ProjectRole"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectMember"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`,
      { body: { role }, method: "PUT" },
      context,
    );
  }

  async listProjectInvitations(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["InvitationList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/invitations`,
      { method: "GET" },
      context,
    );
  }

  async createProjectInvitation(
    projectId: string,
    input: components["schemas"]["CreateInvitationRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["IssuedInvitation"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/invitations`,
      { body: input, method: "POST" },
      context,
    );
  }

  async revokeProjectInvitation(
    projectId: string,
    invitationId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/invitations/${encodeURIComponent(invitationId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async removeProjectMember(
    projectId: string,
    userId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async getProjectPermissions(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectPermissions"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/permissions`,
      { method: "GET" },
      context,
    );
  }

  async listSettingTypes(
    scope: components["schemas"]["SettingScope"],
    context: CoreRequestContext,
    projectId?: string,
  ): Promise<components["schemas"]["SettingTypeList"]> {
    const query = new URLSearchParams({ scope });
    if (projectId) {
      query.set("project_id", projectId);
    }
    return this.request(
      `/v1/settings/types?${query.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async getSystemSetting(
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Setting"]> {
    return this.request(
      `/v1/settings/system/${encodeURIComponent(typeKey)}`,
      { method: "GET" },
      context,
    );
  }

  async updateSystemSetting(
    typeKey: string,
    input: components["schemas"]["UpdateSettingRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Setting"]> {
    return this.request(
      `/v1/settings/system/${encodeURIComponent(typeKey)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteSystemSetting(
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/settings/system/${encodeURIComponent(typeKey)}`,
      { method: "DELETE" },
      context,
    );
  }

  async testSystemSetting(
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ConnectionTestResult"]> {
    return this.request(
      `/v1/settings/system/${encodeURIComponent(typeKey)}/test`,
      { method: "POST" },
      context,
    );
  }

  async getProjectSetting(
    projectId: string,
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Setting"]> {
    return this.request(
      `/v1/settings/projects/${encodeURIComponent(projectId)}/${encodeURIComponent(typeKey)}`,
      { method: "GET" },
      context,
    );
  }

  async updateProjectSetting(
    projectId: string,
    typeKey: string,
    input: components["schemas"]["UpdateSettingRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Setting"]> {
    return this.request(
      `/v1/settings/projects/${encodeURIComponent(projectId)}/${encodeURIComponent(typeKey)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteProjectSetting(
    projectId: string,
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/settings/projects/${encodeURIComponent(projectId)}/${encodeURIComponent(typeKey)}`,
      { method: "DELETE" },
      context,
    );
  }

  async testProjectSetting(
    projectId: string,
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ConnectionTestResult"]> {
    return this.request(
      `/v1/settings/projects/${encodeURIComponent(projectId)}/${encodeURIComponent(typeKey)}/test`,
      { method: "POST" },
      context,
    );
  }

  async listDataObjects(
    projectId: string,
    context: CoreRequestContext,
    options: { cursor?: string; limit?: number; type?: string } = {},
  ): Promise<components["schemas"]["DataObjectPage"]> {
    const query = new URLSearchParams();
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.limit) query.set("limit", String(options.limit));
    if (options.type) query.set("type", options.type);
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/objects${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async readDataObject(
    projectId: string,
    objectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["DataObjectRead"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/objects/${encodeURIComponent(objectId)}`,
      { method: "GET" },
      context,
    );
  }

  async listDataActivity(
    projectId: string,
    context: CoreRequestContext,
    options: { cursor?: string; limit?: number } = {},
  ): Promise<components["schemas"]["DataActivityPage"]> {
    const query = new URLSearchParams();
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.limit) query.set("limit", String(options.limit));
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/activity${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async listProjectContext(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectContextList"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/context`,
      { method: "GET" },
      context,
    );
  }

  async listContextProposals(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ContextProposalList"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/context/proposals`,
      { method: "GET" },
      context,
    );
  }

  async createContextProposal(
    projectId: string,
    input: components["schemas"]["CreateContextProposalRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ContextProposal"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/context/proposals`,
      { body: input, method: "POST" },
      context,
    );
  }

  async reviewContextProposal(
    projectId: string,
    proposalId: string,
    input: components["schemas"]["ReviewContextProposalRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ContextProposal"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/context/proposals/${encodeURIComponent(proposalId)}/review`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getProjectHome(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["HomeAggregate"]> {
    return this.request(
      `/v1/data/projects/${encodeURIComponent(projectId)}/home`,
      { method: "GET" },
      context,
    );
  }

  async recordAuditEvent(
    input: components["schemas"]["RecordAuditEventRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["AuditEvent"]> {
    return this.request(
      "/v1/audit/events",
      { body: input, method: "POST" },
      context,
    );
  }

  async listAuditEvents(
    context: CoreRequestContext,
    options: {
      action?: string;
      actorId?: string;
      category?: string;
      cursor?: string;
      limit?: number;
      outcome?: "denied" | "error" | "success";
      projectId?: string;
      requestId?: string;
      source?: string;
    } = {},
  ): Promise<components["schemas"]["AuditEventPage"]> {
    const query = new URLSearchParams();
    if (options.action) query.set("action", options.action);
    if (options.actorId) query.set("actor_id", options.actorId);
    if (options.category) query.set("category", options.category);
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.limit) query.set("limit", String(options.limit));
    if (options.outcome) query.set("outcome", options.outcome);
    if (options.projectId) query.set("project_id", options.projectId);
    if (options.requestId) query.set("request_id", options.requestId);
    if (options.source) query.set("source", options.source);
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/audit/events${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async request<T>(
    path: string,
    options: CoreRequestOptions,
    context: CoreRequestContext,
  ): Promise<T> {
    const response = await this.fetch(path, options, context);
    if (!response.ok) {
      throw new CoreClientError(response.status, await parseError(response));
    }
    if (response.status === 204) {
      return undefined as T;
    }
    return (await response.json()) as T;
  }

  async fetch(
    path: string,
    options: CoreRequestOptions,
    context: CoreRequestContext,
  ): Promise<Response> {
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    const headers = new Headers(options.headers);
    headers.set("accept", headers.get("accept") ?? "application/json");
    headers.set("x-request-id", context.requestId);
    if (context.accessToken) {
      headers.set("authorization", `Bearer ${context.accessToken}`);
    }
    if (context.projectId) {
      headers.set("x-mmdash-project-id", context.projectId);
    }
    if (context.userId) {
      headers.set("x-mmdash-user-id", context.userId);
    }

    let body = options.body;
    if (isJsonBody(body)) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(body);
    }

    return this.fetchImplementation(`${this.baseUrl}${normalizedPath}`, {
      ...options,
      body: body as BodyInit | null | undefined,
      headers,
    });
  }
}

function isJsonBody(
  body: CoreRequestOptions["body"],
): body is Record<string, unknown> {
  return (
    body !== undefined &&
    body !== null &&
    typeof body === "object" &&
    !(body instanceof ArrayBuffer) &&
    !(body instanceof Blob) &&
    !(body instanceof FormData) &&
    !(body instanceof URLSearchParams)
  );
}

async function parseError(response: Response): Promise<CoreErrorBody> {
  if (!response.headers.get("content-type")?.includes("application/json")) {
    return {};
  }
  try {
    return (await response.json()) as CoreErrorBody;
  } catch {
    return {};
  }
}
