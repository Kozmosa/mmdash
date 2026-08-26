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

  async refreshSession(
    refreshToken: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["LoginResult"]> {
    return this.request(
      "/v1/auth/refresh",
      { body: { refresh_token: refreshToken }, method: "POST" },
      context,
    );
  }

  async startDeviceAuthorization(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["DeviceAuthorization"]> {
    return this.request(
      "/v1/auth/device/authorize",
      { method: "POST" },
      context,
    );
  }

  async verifyDeviceAuthorization(
    input: components["schemas"]["DeviceVerificationRequest"],
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      "/v1/auth/device/verify",
      { body: input, method: "POST" },
      context,
    );
  }

  async exchangeDeviceAuthorization(
    deviceCode: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["LoginResult"]> {
    return this.request(
      "/v1/auth/device/token",
      { body: { device_code: deviceCode }, method: "POST" },
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

  async recordAgentTokenVerification(
    tokenId: string,
    input: components["schemas"]["RecordAgentTokenVerificationRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["AgentTokenVerificationEvidence"]> {
    return this.request(
      `/v1/auth/agent-tokens/${encodeURIComponent(tokenId)}/verification`,
      { body: input, method: "POST" },
      context,
    );
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

  async getRepository(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Repository"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository`,
      { method: "GET" },
      context,
    );
  }

  async connectRepository(
    projectId: string,
    input: components["schemas"]["RepoConnectRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Repository"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository`,
      { body: input, method: "PUT" },
      context,
    );
  }

  async disconnectRepository(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository`,
      { method: "DELETE" },
      context,
    );
  }

  async testRepositoryConnection(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RepoConnectionTestResult"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/test`,
      { method: "POST" },
      context,
    );
  }

  async requestRepositorySync(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Repository"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/sync`,
      { method: "POST" },
      context,
    );
  }

  async rotateRepositoryWebhookSecret(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Repository"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/webhook-secret`,
      { method: "POST" },
      context,
    );
  }

  async updateRepositoryWorkspaces(
    projectId: string,
    input: components["schemas"]["RepoUpdateWorkspacesRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Repository"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/workspaces`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async listRepositoryBranches(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RepoBranchList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/branches`,
      { method: "GET" },
      context,
    );
  }

  async listRepositoryCommits(
    projectId: string,
    workspace: components["schemas"]["RepoWorkspaceKind"],
    context: CoreRequestContext,
    options: { cursor?: string; limit?: number } = {},
  ): Promise<components["schemas"]["RepoCommitPage"]> {
    const query = new URLSearchParams({ workspace });
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.limit) query.set("limit", String(options.limit));
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/commits?${query.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async getRepositoryCommit(
    projectId: string,
    commitSha: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RepoCommit"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/commits/${encodeURIComponent(commitSha)}`,
      { method: "GET" },
      context,
    );
  }

  async listRepositoryTree(
    projectId: string,
    input: {
      cursor?: string;
      limit?: number;
      path?: string;
      revision: string;
      workspace: components["schemas"]["RepoWorkspaceKind"];
    },
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RepoTreePage"]> {
    const query = new URLSearchParams({
      revision: input.revision,
      workspace: input.workspace,
    });
    if (input.cursor) query.set("cursor", input.cursor);
    if (input.limit) query.set("limit", String(input.limit));
    if (input.path) query.set("path", input.path);
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/tree?${query.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async getRepositoryContent(
    projectId: string,
    input: {
      path: string;
      revision: string;
      workspace: components["schemas"]["RepoWorkspaceKind"];
    },
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RepoFileContent"]> {
    const query = new URLSearchParams({
      path: input.path,
      revision: input.revision,
      workspace: input.workspace,
    });
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/repository/content?${query.toString()}`,
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

  async getModels(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelOverview"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models`,
      { method: "GET" },
      context,
    );
  }

  async listArticleChapterTags(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArticleChapterTagList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags`,
      { method: "GET" },
      context,
    );
  }

  async createArticleChapterTag(
    projectId: string,
    input: components["schemas"]["CreateArticleChapterTagRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArticleChapterTag"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getArticleChapterTag(
    projectId: string,
    chapterTagId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArticleChapterTag"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags/${encodeURIComponent(chapterTagId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateArticleChapterTag(
    projectId: string,
    chapterTagId: string,
    input: components["schemas"]["UpdateArticleChapterTagRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArticleChapterTag"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags/${encodeURIComponent(chapterTagId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteArticleChapterTag(
    projectId: string,
    chapterTagId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags/${encodeURIComponent(chapterTagId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async reviewArticleChapterTag(
    projectId: string,
    chapterTagId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArticleChapterTag"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/article/chapter-tags/${encodeURIComponent(chapterTagId)}/review`,
      { method: "POST" },
      context,
    );
  }

  async getModelSource(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSource"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/source`,
      { method: "GET" },
      context,
    );
  }

  async syncModels(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSync"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/source/sync`,
      { method: "POST" },
      context,
    );
  }

  async getNotionOAuthConnection(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotionOAuthConnection"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/notion/oauth`,
      { method: "GET" },
      context,
    );
  }

  async startNotionOAuth(
    projectId: string,
    input: components["schemas"]["StartNotionOAuthRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotionOAuthAuthorization"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/notion/oauth/authorizations`,
      { body: input, method: "POST" },
      context,
    );
  }

  async completeNotionOAuth(
    input: components["schemas"]["CompleteNotionOAuthRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotionOAuthCallbackResult"]> {
    return this.request(
      "/v1/model-notion/oauth/callback",
      { body: input, method: "POST" },
      context,
    );
  }

  async disconnectNotionOAuth(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/notion/oauth/connection`,
      { method: "DELETE" },
      context,
    );
  }

  async listModelQuestions(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelQuestionList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions`,
      { method: "GET" },
      context,
    );
  }

  async createModelQuestion(
    projectId: string,
    input: components["schemas"]["CreateModelQuestionRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelQuestion"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getModelQuestion(
    projectId: string,
    questionId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelQuestionDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateModelQuestion(
    projectId: string,
    questionId: string,
    input: components["schemas"]["UpdateModelQuestionRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelQuestion"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteModelQuestion(
    projectId: string,
    questionId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async syncModelQuestion(
    projectId: string,
    questionId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSync"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/sync`,
      { method: "POST" },
      context,
    );
  }

  async listModelSnapshots(
    projectId: string,
    questionId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSnapshotList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/snapshots`,
      { method: "GET" },
      context,
    );
  }

  async getModelSnapshot(
    projectId: string,
    questionId: string,
    snapshotId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSnapshot"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/snapshots/${encodeURIComponent(snapshotId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateModelSnapshot(
    projectId: string,
    questionId: string,
    snapshotId: string,
    input: components["schemas"]["UpdateModelSnapshotRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelSnapshot"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/snapshots/${encodeURIComponent(snapshotId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async diffModelSnapshots(
    projectId: string,
    questionId: string,
    fromSnapshotId: string,
    toSnapshotId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ModelDiff"]> {
    const query = new URLSearchParams({
      from_snapshot_id: fromSnapshotId,
      to_snapshot_id: toSnapshotId,
    });
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/diff?${query.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async initializeArtifactUpload(
    projectId: string,
    input: components["schemas"]["ArtifactInitializeUploadRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactUploadSession"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/uploads`,
      { body: input, method: "POST" },
      context,
    );
  }

  async initializeAgentArtifactUpload(
    projectId: string,
    input: components["schemas"]["AgentArtifactInitializeUploadRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactUploadSession"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/agent-uploads`,
      { body: input, method: "POST" },
      context,
    );
  }

  async initializeArtifactVersionUpload(
    projectId: string,
    artifactId: string,
    input: components["schemas"]["ArtifactInitializeVersionUploadRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactUploadSession"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/versions/uploads`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getArtifactUpload(
    projectId: string,
    uploadId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactUploadSession"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/uploads/${encodeURIComponent(uploadId)}`,
      { method: "GET" },
      context,
    );
  }

  async signArtifactUploadParts(
    projectId: string,
    uploadId: string,
    input: components["schemas"]["ArtifactSignPartsRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactPartGrantList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/uploads/${encodeURIComponent(uploadId)}/parts/sign`,
      { body: input, method: "POST" },
      context,
    );
  }

  async confirmArtifactUpload(
    projectId: string,
    uploadId: string,
    input: components["schemas"]["ArtifactConfirmUploadRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/uploads/${encodeURIComponent(uploadId)}/confirm`,
      { body: input, method: "POST" },
      context,
    );
  }

  async abortArtifactUpload(
    projectId: string,
    uploadId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/uploads/${encodeURIComponent(uploadId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async listArtifacts(
    projectId: string,
    context: CoreRequestContext,
    options: {
      cursor?: string;
      kind?: components["schemas"]["ArtifactKind"];
      limit?: number;
      source?: components["schemas"]["ArtifactSource"];
      status?: components["schemas"]["ArtifactStatus"];
      tag?: string;
    } = {},
  ): Promise<components["schemas"]["ArtifactPage"]> {
    const query = new URLSearchParams();
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.kind) query.set("kind", options.kind);
    if (options.limit) query.set("limit", String(options.limit));
    if (options.source) query.set("source", options.source);
    if (options.status) query.set("status", options.status);
    if (options.tag) query.set("tag", options.tag);
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async listArtifactTrash(
    projectId: string,
    context: CoreRequestContext,
    options: {
      cursor?: string;
      kind?: components["schemas"]["ArtifactKind"];
      limit?: number;
      tag?: string;
    } = {},
  ): Promise<components["schemas"]["ArtifactPage"]> {
    const query = new URLSearchParams();
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.kind) query.set("kind", options.kind);
    if (options.limit) query.set("limit", String(options.limit));
    if (options.tag) query.set("tag", options.tag);
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/trash${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async getArtifact(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateArtifact(
    projectId: string,
    artifactId: string,
    input: components["schemas"]["ArtifactUpdateRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async requestArtifactSemanticDescription(
    projectId: string,
    artifactId: string,
    input: components["schemas"]["ArtifactSemanticDescriptionInput"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactSemanticDescriptionJob"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/description`,
      { body: input, method: "POST" },
      context,
    );
  }

  async listArtifactVersions(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactVersionList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/versions`,
      { method: "GET" },
      context,
    );
  }

  async restoreArtifactVersion(
    projectId: string,
    artifactId: string,
    versionId: string,
    input: components["schemas"]["ArtifactRestoreVersionRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/versions/${encodeURIComponent(versionId)}/restore`,
      { body: input, method: "POST" },
      context,
    );
  }

  async downloadArtifact(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
    versionId?: string,
  ): Promise<components["schemas"]["ArtifactDownloadGrant"]> {
    const versionSegment = versionId
      ? `/versions/${encodeURIComponent(versionId)}`
      : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}${versionSegment}/download`,
      { method: "POST" },
      context,
    );
  }

  async listArtifactPreviews(
    projectId: string,
    artifactId: string,
    versionId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactPreviewList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/versions/${encodeURIComponent(versionId)}/previews`,
      { method: "GET" },
      context,
    );
  }

  async trashArtifact(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async restoreArtifact(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ArtifactDetail"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/restore`,
      { method: "POST" },
      context,
    );
  }

  async purgeArtifact(
    projectId: string,
    artifactId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/artifacts/${encodeURIComponent(artifactId)}/purge`,
      { method: "DELETE" },
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

  async getProgress(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Progress"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress`,
      { method: "GET" },
      context,
    );
  }

  async listProgressMilestones(projectId: string, context: CoreRequestContext) {
    return this.request<components["schemas"]["MilestoneList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/milestones`,
      { method: "GET" },
      context,
    );
  }

  async createProgressMilestone(
    projectId: string,
    input: components["schemas"]["CreateMilestoneRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Milestone"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/milestones`,
      { body: input, method: "POST" },
      context,
    );
  }

  async updateProgressMilestone(
    projectId: string,
    milestoneId: string,
    input: components["schemas"]["UpdateMilestoneRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Milestone"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/milestones/${encodeURIComponent(milestoneId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteProgressMilestone(
    projectId: string,
    milestoneId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/milestones/${encodeURIComponent(milestoneId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async listProgressTasks(projectId: string, context: CoreRequestContext) {
    return this.request<components["schemas"]["TaskList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/tasks`,
      { method: "GET" },
      context,
    );
  }

  async createProgressTask(
    projectId: string,
    input: components["schemas"]["CreateTaskRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Task"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/tasks`,
      { body: input, method: "POST" },
      context,
    );
  }

  async updateProgressTask(
    projectId: string,
    taskId: string,
    input: components["schemas"]["UpdateTaskRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Task"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/tasks/${encodeURIComponent(taskId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async deleteProgressTask(
    projectId: string,
    taskId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/tasks/${encodeURIComponent(taskId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async listProgressDependencies(
    projectId: string,
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["DependencyList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/dependencies`,
      { method: "GET" },
      context,
    );
  }

  async createProgressDependency(
    projectId: string,
    input: components["schemas"]["CreateDependencyRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Dependency"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/dependencies`,
      { body: input, method: "POST" },
      context,
    );
  }

  async deleteProgressDependency(
    projectId: string,
    dependencyId: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/dependencies/${encodeURIComponent(dependencyId)}`,
      { method: "DELETE" },
      context,
    );
  }

  async listProgressReminders(projectId: string, context: CoreRequestContext) {
    return this.request<components["schemas"]["ReminderList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/reminders`,
      { method: "GET" },
      context,
    );
  }

  async createProgressReminder(
    projectId: string,
    input: components["schemas"]["CreateReminderRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Reminder"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/reminders`,
      { body: input, method: "POST" },
      context,
    );
  }

  async triggerProgressReminder(
    projectId: string,
    reminderId: string,
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["Reminder"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/reminders/${encodeURIComponent(reminderId)}/trigger`,
      { method: "POST" },
      context,
    );
  }

  async listProgressProposals(projectId: string, context: CoreRequestContext) {
    return this.request<components["schemas"]["ProgressProposalList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/proposals`,
      { method: "GET" },
      context,
    );
  }

  async createProgressProposal(
    projectId: string,
    input: components["schemas"]["CreateProgressProposalRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["ProgressProposal"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/proposals`,
      { body: input, method: "POST" },
      context,
    );
  }

  async reviewProgressProposal(
    projectId: string,
    proposalId: string,
    input: components["schemas"]["ReviewProgressProposalRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["ProgressProposal"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/proposals/${encodeURIComponent(proposalId)}/review`,
      { body: input, method: "POST" },
      context,
    );
  }

  async batchReviewProgressProposals(
    projectId: string,
    input: components["schemas"]["BatchReviewProgressProposalsRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["ProgressProposalList"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/proposals/batch-review`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getProgressSettings(projectId: string, context: CoreRequestContext) {
    return this.request<components["schemas"]["ProgressSettings"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/settings`,
      { method: "GET" },
      context,
    );
  }

  async listInbox(
    query: Record<string, string | number | undefined>,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["InboxPage"]> {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query))
      if (value !== undefined) params.set(key, String(value));
    return this.request(
      `/v1/inbox?${params.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async acceptInvitationById(
    invitationId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectMember"]> {
    return this.request(
      `/v1/projects/invitations/${encodeURIComponent(invitationId)}/accept`,
      { method: "POST" },
      context,
    );
  }

  async getInbox(
    inboxItemId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["InboxItem"]> {
    return this.request(
      `/v1/inbox/${encodeURIComponent(inboxItemId)}`,
      { method: "GET" },
      context,
    );
  }

  async updateInbox(
    inboxItemId: string,
    input: components["schemas"]["UpdateInboxRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["InboxItem"]> {
    return this.request(
      `/v1/inbox/${encodeURIComponent(inboxItemId)}`,
      { method: "PATCH", body: input },
      context,
    );
  }

  async unreadInboxCount(
    projectId: string | undefined,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["UnreadCount"]> {
    const suffix = projectId
      ? `?project_id=${encodeURIComponent(projectId)}`
      : "";
    return this.request(
      `/v1/inbox/unread-count${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async markAllInboxRead(
    input: components["schemas"]["MarkAllInboxReadRequest"],
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      "/v1/inbox/mark-all-read",
      { method: "POST", body: input },
      context,
    );
  }

  async getNotificationChannel(
    projectId: string,
    channelKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationChannel"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(channelKey)}`,
      { method: "GET" },
      context,
    );
  }
  async updateNotificationChannel(
    projectId: string,
    channelKey: string,
    input: components["schemas"]["UpdateNotificationChannelRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationChannel"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(channelKey)}`,
      { method: "PATCH", body: input },
      context,
    );
  }
  async deleteNotificationChannel(
    projectId: string,
    channelKey: string,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(channelKey)}`,
      { method: "DELETE" },
      context,
    );
  }
  async testNotificationChannel(
    projectId: string,
    channelKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ConnectionTestResult"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-channels/${encodeURIComponent(channelKey)}/test`,
      { method: "POST" },
      context,
    );
  }
  async getNotificationRule(
    projectId: string,
    typeKey: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationRule"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-rules/${encodeURIComponent(typeKey)}`,
      { method: "GET" },
      context,
    );
  }
  async updateNotificationRule(
    projectId: string,
    typeKey: string,
    input: components["schemas"]["UpdateNotificationRuleRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationRule"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-rules/${encodeURIComponent(typeKey)}`,
      { method: "PUT", body: input },
      context,
    );
  }
  async listNotificationDeliveries(
    projectId: string,
    query: Record<string, string | number | undefined>,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationDeliveryPage"]> {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query))
      if (value !== undefined) params.set(key, String(value));
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-deliveries?${params.toString()}`,
      { method: "GET" },
      context,
    );
  }
  async retryNotificationDelivery(
    projectId: string,
    deliveryId: string,
    input: components["schemas"]["RetryNotificationDeliveryRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["NotificationDelivery"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/notification-deliveries/${encodeURIComponent(deliveryId)}/retry`,
      { method: "POST", body: input },
      context,
    );
  }

  async updateProgressSettings(
    projectId: string,
    input: components["schemas"]["UpdateProgressSettingsRequest"],
    context: CoreRequestContext,
  ) {
    return this.request<components["schemas"]["ProgressSettings"]>(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/settings`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async recalculateProgress(
    projectId: string,
    input: components["schemas"]["RecalculateProgressRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RecalculateProgressResult"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/recalculate`,
      { body: input, method: "POST" },
      context,
    );
  }

  async listProgressEvaluations(
    projectId: string,
    query: { cursor?: string; limit?: number },
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProgressEvaluationPage"]> {
    const params = new URLSearchParams();
    if (query.cursor) params.set("cursor", query.cursor);
    if (query.limit !== undefined) params.set("limit", String(query.limit));
    const suffix = params.size > 0 ? `?${params.toString()}` : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/evaluations${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async getProgressEvaluation(
    projectId: string,
    evaluationId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProgressEvaluation"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/evaluations/${encodeURIComponent(evaluationId)}`,
      { method: "GET" },
      context,
    );
  }

  async retryProgressEvaluation(
    projectId: string,
    evaluationId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["RecalculateProgressResult"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/evaluations/${encodeURIComponent(evaluationId)}/retry`,
      { method: "POST" },
      context,
    );
  }

  async setProgressStageOverride(
    projectId: string,
    input: components["schemas"]["SetProgressStageOverrideRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProgressStageOverride"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/stage-override`,
      { body: input, method: "POST" },
      context,
    );
  }

  async clearProgressStageOverride(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProgressStageOverride"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/progress/stage-override`,
      { method: "DELETE" },
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

  async listExperiments(
    projectId: string,
    context: CoreRequestContext,
    options: { cursor?: string; limit?: number; status?: string } = {},
  ): Promise<components["schemas"]["ExperimentPage"]> {
    const query = new URLSearchParams();
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.limit) query.set("limit", String(options.limit));
    if (options.status) query.set("status", options.status);
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async createExperiment(
    projectId: string,
    input: components["schemas"]["CreateExperimentRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments`,
      { body: input, method: "POST" },
      context,
    );
  }

  async getExperimentSettings(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ExperimentSettings"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/settings`,
      { method: "GET" },
      context,
    );
  }

  async updateExperimentSettings(
    projectId: string,
    input: components["schemas"]["UpdateExperimentSettingsRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ExperimentSettings"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/settings`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async getExperiment(
    projectId: string,
    experimentId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}`,
      { method: "GET" },
      context,
    );
  }

  async compareExperiments(
    projectId: string,
    experimentIds: string[],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ExperimentComparison"]> {
    const query = new URLSearchParams();
    query.set("experiment_id", experimentIds.join(","));
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/compare?${query.toString()}`,
      { method: "GET" },
      context,
    );
  }

  async runExperiment(
    projectId: string,
    experimentId: string,
    input: components["schemas"]["RunExperimentRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/run`,
      { body: input, method: "POST" },
      context,
    );
  }

  async rerunExperiment(
    projectId: string,
    experimentId: string,
    input: components["schemas"]["RerunExperimentRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/rerun`,
      { body: input, method: "POST" },
      context,
    );
  }

  async bindExperimentResult(
    projectId: string,
    experimentId: string,
    input: components["schemas"]["BindExperimentResultRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/result/bind`,
      { body: input, method: "POST" },
      context,
    );
  }

  async cancelExperiment(
    projectId: string,
    experimentId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/cancel`,
      { method: "POST" },
      context,
    );
  }

  async archiveExperiment(
    projectId: string,
    experimentId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Experiment"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/archive`,
      { method: "POST" },
      context,
    );
  }

  async listExperimentLogs(
    projectId: string,
    experimentId: string,
    context: CoreRequestContext,
    options: { cursor?: string | number; limit?: number; tail?: boolean } = {},
  ): Promise<components["schemas"]["ExperimentLogPage"]> {
    const query = new URLSearchParams();
    if (options.cursor !== undefined)
      query.set("cursor", String(options.cursor));
    if (options.limit !== undefined) query.set("limit", String(options.limit));
    if (options.tail !== undefined) query.set("tail", String(options.tail));
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/logs${suffix}`,
      { method: "GET" },
      context,
    );
  }

  async getExperimentResult(
    projectId: string,
    experimentId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ResultBundle"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/experiments/${encodeURIComponent(experimentId)}/result`,
      { method: "GET" },
      context,
    );
  }

  async listPersonalBoxes(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["BoxList"]> {
    return this.request("/v1/users/me/boxes", { method: "GET" }, context);
  }

  async listBoxReleases(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["BoxReleaseList"]> {
    return this.request("/v1/box/releases", { method: "GET" }, context);
  }

  async getPersonalBox(
    boxId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Box"]> {
    return this.request(
      `/v1/users/me/boxes/${encodeURIComponent(boxId)}`,
      { method: "GET" },
      context,
    );
  }

  async updatePersonalBox(
    boxId: string,
    input: components["schemas"]["UpdateBoxRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["Box"]> {
    return this.request(
      `/v1/users/me/boxes/${encodeURIComponent(boxId)}`,
      { body: input, method: "PATCH" },
      context,
    );
  }

  async revokePersonalBox(
    boxId: string,
    input: components["schemas"]["RevokeBoxRequest"],
    context: CoreRequestContext,
  ): Promise<components["schemas"]["BoxRevocation"]> {
    return this.request(
      `/v1/users/me/boxes/${encodeURIComponent(boxId)}/revoke`,
      { body: input, method: "POST" },
      context,
    );
  }

  async listProjectBoxes(
    projectId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectBoxList"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/boxes`,
      { method: "GET" },
      context,
    );
  }

  async assignProjectBox(
    projectId: string,
    boxId: string,
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ProjectBoxBinding"]> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/boxes/${encodeURIComponent(boxId)}`,
      { method: "PUT" },
      context,
    );
  }

  async removeProjectBox(
    projectId: string,
    boxId: string,
    force: boolean,
    context: CoreRequestContext,
  ): Promise<void> {
    return this.request(
      `/v1/projects/${encodeURIComponent(projectId)}/boxes/${encodeURIComponent(boxId)}?force=${String(force)}`,
      { method: "DELETE" },
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
    !ArrayBuffer.isView(body) &&
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
