import { runCompose } from "./compose-command.mjs";

export async function runRepoSmoke({
  coreUrl,
  email,
  password,
  runId,
  webUrl,
}) {
  const fixtureRoot = `/tmp/mmdash-repo-smoke-${safeRunId(runId)}`;
  const remote = `${fixtureRoot}/remote.git`;
  runCompose(["exec", "-T", "core", "mkdir", "-p", fixtureRoot]);
  runCompose(["exec", "-T", "core", "git", "init", "--bare", remote]);

  const codeHead = createFixtureCommit({
    branch: "main",
    files: {
      "README.md": "Stage 1 initial code\n",
      "run.py": `import os

print("MMDASH_STAGE8_STDOUT", flush=True)
os.makedirs("/output/tables", exist_ok=True)
with open("/output/summary.md", "w", encoding="utf-8") as handle:
    handle.write("Stage 8 terminal acceptance passed\\n")
with open("/output/tables/result.csv", "w", encoding="utf-8") as handle:
    handle.write("x,y\\n1,2\\n")
`,
    },
    message: "Initial code",
    remote,
  });
  createFixtureCommit({
    branch: "article",
    content: "# Initial article\n",
    file: "article.md",
    message: "Initial article",
    remote,
  });
  createFixtureCommit({
    branch: "result",
    content: "initial result\n",
    file: "result.txt",
    message: "Initial result",
    remote,
  });
  git(remote, ["symbolic-ref", "HEAD", "refs/heads/main"]);

  const login = await jsonChecked(`${webUrl}/api/auth/login`, {
    body: { email, password },
    method: "POST",
  });
  const cookieHeader =
    login.response.headers.getSetCookie?.()[0] ??
    login.response.headers.get("set-cookie");
  assert(cookieHeader, "Repo smoke login did not set a browser cookie.");
  const cookie = cookieHeader.split(";", 1)[0];

  const project = await jsonChecked(`${webUrl}/api/projects`, {
    body: {
      name: `Repo smoke ${runId}`,
      problem_summary: "Stage 1 server-existing Git Docker acceptance",
      problem_title: "Repository integration",
    },
    headers: { cookie },
    method: "POST",
  });
  const projectId = project.body.id;
  assert(projectId, "Repo smoke project creation did not return an ID.");
  const projectPath = `${webUrl}/api/projects/${encodeURIComponent(projectId)}`;
  const setting = await jsonChecked(`${projectPath}/settings/repo.connection`, {
    body: {
      values: {
        article_branch: "article",
        code_branch: "main",
        provider: "server_existing",
        remote_url: remote,
        result_branch: "result",
      },
    },
    headers: { cookie },
    method: "PATCH",
  });
  assert(
    setting.body.values?.remote_url === "********" &&
      !JSON.stringify(setting.body).includes(remote),
    "Server-existing Repo setting exposed its mounted path.",
  );
  const tested = await jsonChecked(`${projectPath}/repository/test`, {
    headers: { cookie },
    method: "POST",
  });
  assert(
    tested.body.status === "passed" &&
      ["main", "article", "result"].every((branch) =>
        tested.body.branches.includes(branch),
      ),
    `Server-existing Repo connection test failed: ${JSON.stringify(tested.body)}`,
  );

  const accepted = await jsonChecked(`${projectPath}/repository`, {
    body: { settings_version: setting.body.version },
    headers: { cookie },
    method: "PUT",
  });
  const repositoryId = accepted.body.repository_id;
  assert(
    repositoryId && accepted.body.remote_url === null,
    "Server-existing Repo connection exposed a mounted path.",
  );
  const ready = await poll(
    async () =>
      (
        await jsonChecked(`${projectPath}/repository`, {
          headers: { cookie },
        })
      ).body,
    (repository) =>
      repository.status === "ready" &&
      repository.workspaces?.length === 3 &&
      repository.workspaces.every(
        (workspace) =>
          workspace.status === "ready" && workspace.head_commit_sha,
      ),
    "Server-existing Repo did not synchronize three logical workspaces.",
  );
  const resolvedCodeHead = ready.workspaces.find(
    (workspace) => workspace.workspace === "code",
  ).head_commit_sha;
  assert(
    resolvedCodeHead === codeHead,
    `Code workspace resolved ${resolvedCodeHead}, expected ${codeHead}.`,
  );

  const commits = await jsonChecked(
    withQuery(`${projectPath}/repository/commits`, {
      limit: "10",
      workspace: "code",
    }),
    { headers: { cookie } },
  );
  assert(
    commits.body.resolved_revision === codeHead &&
      commits.body.items?.some((commit) => commit.commit_sha === codeHead),
    "Commit list was not pinned to the code head.",
  );
  const tree = await jsonChecked(
    withQuery(`${projectPath}/repository/tree`, {
      limit: "200",
      revision: codeHead,
      workspace: "code",
    }),
    { headers: { cookie } },
  );
  assert(
    tree.body.resolved_revision === codeHead &&
      tree.body.items?.some((entry) => entry.path === "README.md"),
    "Repo tree did not expose the fixed-SHA fixture.",
  );
  const initialContent = await repoContent({
    cookie,
    projectPath,
    revision: codeHead,
  });
  assert(
    initialContent.content === "Stage 1 initial code\n" &&
      initialContent.resolved_revision === codeHead,
    "Initial content was not read from the pinned commit.",
  );

  const coreLogin = await jsonChecked(`${coreUrl}/v1/auth/login`, {
    body: { email, password },
    method: "POST",
  });
  const authorization = `Bearer ${coreLogin.body.access_token}`;
  const checkout = await jsonChecked(
    `${coreUrl}/v1/projects/${encodeURIComponent(projectId)}/repository/checkouts`,
    {
      body: {
        commit_sha: codeHead,
        purpose: "Stage 1 Docker acceptance",
        ttl_seconds: 120,
      },
      headers: { authorization },
      method: "POST",
    },
  );
  assert(
    checkout.body.status === "active" &&
      checkout.body.commit_sha === codeHead &&
      !("checkout_relpath" in checkout.body),
    "Checkout response was not fixed-SHA or leaked a server path.",
  );
  await fetchChecked(
    `${coreUrl}/v1/projects/${encodeURIComponent(projectId)}/repository/checkouts/${encodeURIComponent(checkout.body.checkout_id)}`,
    { headers: { authorization }, method: "DELETE" },
  );

  const nextCodeHead = createFixtureCommit({
    branch: "main",
    content: "Stage 1 externally updated code\n",
    file: "README.md",
    message: "External update",
    parent: codeHead,
    remote,
  });
  await jsonChecked(`${projectPath}/repository/sync`, {
    headers: { cookie },
    method: "POST",
  });
  await poll(
    async () =>
      (
        await jsonChecked(`${projectPath}/repository`, {
          headers: { cookie },
        })
      ).body,
    (repository) =>
      repository.status === "ready" &&
      repository.workspaces?.find((workspace) => workspace.workspace === "code")
        ?.head_commit_sha === nextCodeHead,
    "Manual sync did not detect the external push.",
  );
  const oldContent = await repoContent({
    cookie,
    projectPath,
    revision: codeHead,
  });
  const nextContent = await repoContent({
    cookie,
    projectPath,
    revision: nextCodeHead,
  });
  assert(
    oldContent.content === "Stage 1 initial code\n" &&
      nextContent.content === "Stage 1 externally updated code\n",
    "Branch movement changed an immutable content read.",
  );

  const projectedCommits = await poll(
    async () =>
      (
        await jsonChecked(
          withQuery(
            `${coreUrl}/v1/data/projects/${encodeURIComponent(projectId)}/objects`,
            { type: "repo_commit" },
          ),
          { headers: { authorization } },
        )
      ).body,
    (page) =>
      page.items?.some((item) => item.metadata?.commit_sha === nextCodeHead),
    "Data Hub did not project the externally detected commit.",
  );
  const projected = projectedCommits.items.find(
    (item) => item.metadata?.commit_sha === nextCodeHead,
  );
  const projectedRead = await jsonChecked(
    `${coreUrl}/v1/data/projects/${encodeURIComponent(projectId)}/objects/${encodeURIComponent(projected.object_id)}`,
    { headers: { authorization } },
  );
  assert(
    projectedRead.body.content?.commit_sha === nextCodeHead,
    "Data Hub reader did not route Repo commit reads through Core.",
  );

  const metrics = await (await fetchChecked(`${coreUrl}/metrics`)).text();
  for (const name of [
    "mmdash_repo_operations_total",
    "mmdash_repo_operation_duration_seconds",
    "mmdash_repo_sync_queue_depth",
    "mmdash_repo_checkouts_active",
    "mmdash_repo_storage_bytes",
  ]) {
    assert(metrics.includes(name), `Repo metrics omitted ${name}.`);
  }

  return {
    code_head: codeHead,
    detected_head: nextCodeHead,
    fixture_root: fixtureRoot,
    project_id: projectId,
    remote,
    repository_id: repositoryId,
  };
}

export async function runManagedRepoSmoke({
  coreUrl,
  email,
  password,
  runId,
  webUrl,
}) {
  const login = await jsonChecked(`${webUrl}/api/auth/login`, {
    body: { email, password },
    method: "POST",
  });
  const cookieHeader =
    login.response.headers.getSetCookie?.()[0] ??
    login.response.headers.get("set-cookie");
  assert(
    cookieHeader,
    "Managed Repo smoke login did not set a browser cookie.",
  );
  const cookie = cookieHeader.split(";", 1)[0];
  const project = await jsonChecked(`${webUrl}/api/projects`, {
    body: {
      name: `Managed Repo smoke ${runId}`,
      problem_summary: "mmdash managed repository acceptance",
      problem_title: "Managed repository integration",
    },
    headers: { cookie },
    method: "POST",
  });
  const projectId = project.body.id;
  assert(projectId, "Managed Repo project creation did not return an ID.");
  const projectPath = `${webUrl}/api/projects/${encodeURIComponent(projectId)}`;
  const capabilities = await jsonChecked(
    `${projectPath}/repository/capabilities`,
    { headers: { cookie } },
  );
  assert(
    capabilities.body.providers?.some(
      (item) => item.provider === "managed" && item.enabled === true,
    ),
    "Managed Repo capability is not enabled.",
  );
  const setting = await jsonChecked(`${projectPath}/settings/repo.connection`, {
    body: {
      values: {
        article_branch: "article",
        code_branch: "main",
        provider: "managed",
        result_branch: "result",
      },
    },
    headers: { cookie },
    method: "PATCH",
  });
  assert(
    !JSON.stringify(setting.body).includes("/var/lib/mmdash/repos"),
    "Managed Repo setting leaked its storage root.",
  );
  const tested = await jsonChecked(`${projectPath}/repository/test`, {
    headers: { cookie },
    method: "POST",
  });
  assert(
    tested.body.status === "passed" &&
      ["main", "article", "result"].every((branch) =>
        tested.body.branches.includes(branch),
      ),
    `Managed Repo test failed: ${JSON.stringify(tested.body)}`,
  );
  const accepted = await jsonChecked(`${projectPath}/repository`, {
    body: { settings_version: setting.body.version },
    headers: { cookie },
    method: "PUT",
  });
  assert(
    accepted.body.provider === "managed" && accepted.body.remote_url === null,
    "Managed Repo response exposed a remote or storage path.",
  );
  let ready = await poll(
    async () =>
      (
        await jsonChecked(`${projectPath}/repository`, {
          headers: { cookie },
        })
      ).body,
    (repository) =>
      repository.status === "ready" &&
      repository.provider === "managed" &&
      repository.remote_url === null &&
      repository.workspaces?.length === 3 &&
      repository.workspaces.every(
        (workspace) =>
          workspace.status === "ready" && workspace.head_commit_sha,
      ),
    "Managed Repo did not initialize three workspaces.",
  );
  const coreLogin = await jsonChecked(`${coreUrl}/v1/auth/login`, {
    body: { email, password },
    method: "POST",
  });
  const authorization = `Bearer ${coreLogin.body.access_token}`;
  for (const workspace of ["code", "article", "result"]) {
    const current = ready.workspaces.find(
      (candidate) => candidate.workspace === workspace,
    );
    assert(current?.head_commit_sha, `Managed ${workspace} head is missing.`);
    const filename = `${workspace}.txt`;
    const content = `managed ${workspace} acceptance\n`;
    const committed = await jsonChecked(
      `${coreUrl}/v1/projects/${encodeURIComponent(projectId)}/repository/commits`,
      {
        body: {
          changes: [
            {
              content_base64: Buffer.from(content).toString("base64"),
              operation: "put",
              path: filename,
            },
          ],
          expected_head_sha: current.head_commit_sha,
          idempotency_key: `managed-smoke-${runId}-${workspace}`,
          message: `test(repo): write managed ${workspace}`,
          workspace,
        },
        headers: { authorization },
        method: "POST",
      },
    );
    const contentResponse = await jsonChecked(
      withQuery(`${projectPath}/repository/content`, {
        path: filename,
        revision: committed.body.commit_sha,
        workspace,
      }),
      { headers: { cookie } },
    );
    assert(
      contentResponse.body.content === content,
      `Managed ${workspace} content did not round-trip through Repo.`,
    );
    ready = (
      await jsonChecked(`${projectPath}/repository`, { headers: { cookie } })
    ).body;
  }
  return {
    code_head: ready.workspaces.find(
      (workspace) => workspace.workspace === "code",
    )?.head_commit_sha,
    project_id: projectId,
    repository_id: accepted.body.repository_id,
  };
}

function createFixtureCommit({
  branch,
  content,
  file,
  files,
  message,
  parent,
  remote,
}) {
  const entries = files ?? { [file]: content };
  const treeInput = Object.entries(entries)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([path, value]) => {
      const blob = git(remote, ["hash-object", "-w", "--stdin"], {
        input: value,
      }).stdout.trim();
      return `100644 blob ${blob}\t${path}`;
    })
    .join("\n");
  const tree = git(remote, ["mktree"], {
    input: `${treeInput}\n`,
  }).stdout.trim();
  const args = ["commit-tree", tree];
  if (parent) args.push("-p", parent);
  const commit = git(remote, args, { input: `${message}\n` }).stdout.trim();
  git(remote, ["update-ref", `refs/heads/${branch}`, commit]);
  return commit;
}

function git(remote, args, options = {}) {
  return runCompose(
    [
      "exec",
      "-T",
      "-e",
      "GIT_AUTHOR_NAME=mmdash Smoke",
      "-e",
      "GIT_AUTHOR_EMAIL=smoke@mmdash.local",
      "-e",
      "GIT_COMMITTER_NAME=mmdash Smoke",
      "-e",
      "GIT_COMMITTER_EMAIL=smoke@mmdash.local",
      "-w",
      remote,
      "core",
      "git",
      ...args,
    ],
    options,
  );
}

async function repoContent({ cookie, projectPath, revision }) {
  return (
    await jsonChecked(
      withQuery(`${projectPath}/repository/content`, {
        path: "README.md",
        revision,
        workspace: "code",
      }),
      { headers: { cookie } },
    )
  ).body;
}

async function jsonChecked(url, options = {}) {
  const response = await fetchChecked(url, options);
  return { body: await response.json(), response };
}

async function fetchChecked(url, options = {}) {
  const headers = new Headers(options.headers);
  let body = options.body;
  if (body !== undefined) {
    headers.set("content-type", "application/json");
    body = JSON.stringify(body);
  }
  const response = await fetch(url, {
    ...options,
    body,
    headers,
    signal: AbortSignal.timeout(20_000),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(
      `${options.method ?? "GET"} ${url}: HTTP ${response.status} ${text}`,
    );
  }
  return response;
}

async function poll(load, ready, message) {
  let last;
  for (let attempt = 0; attempt < 45; attempt += 1) {
    last = await load();
    if (ready(last)) return last;
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error(`${message} Last state: ${JSON.stringify(last)}`);
}

function withQuery(url, values) {
  return `${url}?${new URLSearchParams(values)}`;
}

function safeRunId(value) {
  return value.replace(/[^A-Za-z0-9_-]/g, "-");
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
