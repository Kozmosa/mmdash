import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

import { resolveComposeCommand } from "./compose-command.mjs";

const coreUrl = trim(
  process.env.MMDASH_SMOKE_CORE_URL ?? "http://localhost:8080",
);
const email = process.env.MMDASH_SMOKE_EMAIL ?? "admin@mmdash.local";
const password = process.env.MMDASH_SMOKE_PASSWORD ?? "mmdash-local-admin";
const runId = `${Date.now()}-${process.pid}`;
const source = await readFile(
  new URL("../docs/screenshots/repo-browser.png", import.meta.url),
);
const sha256 = createHash("sha256").update(source).digest("hex");

let artifactId;
let projectId;
let sessionHeaders;
let tokenId;

try {
  const login = await jsonChecked(`${coreUrl}/v1/auth/login`, {
    body: { email, password },
    method: "POST",
  });
  assert(login.body.access_token, "Core login did not return an access token.");
  sessionHeaders = {
    authorization: `Bearer ${login.body.access_token}`,
  };
  const priorTokens = await jsonChecked(`${coreUrl}/v1/auth/tokens`, {
    headers: sessionHeaders,
  });
  for (const credential of priorTokens.body.items ?? []) {
    if (
      credential.name?.startsWith("artifact-preview-smoke-") &&
      !credential.revoked_at
    ) {
      await safeRequest(`${coreUrl}/v1/auth/tokens/${credential.id}`, {
        headers: sessionHeaders,
        method: "DELETE",
      });
    }
  }

  const project = await jsonChecked(`${coreUrl}/v1/projects`, {
    body: {
      name: `Artifact preview smoke ${runId}`,
      problem_summary: "Disposable real MinIO and Worker preview verification",
      problem_title: "Stage 2 Artifact",
    },
    headers: sessionHeaders,
    method: "POST",
  });
  projectId = project.body.id;
  assert(projectId, "Project creation did not return an ID.");

  const issued = await jsonChecked(`${coreUrl}/v1/auth/tokens`, {
    body: {
      kind: "api",
      name: `artifact-preview-smoke-${runId}`,
      project_id: projectId,
    },
    headers: sessionHeaders,
    method: "POST",
  });
  tokenId = issued.body.credential?.id;
  const workerToken = issued.body.token;
  assert(tokenId && workerToken, "Core did not issue a Worker token.");

  const upload = await jsonChecked(
    `${coreUrl}/v1/projects/${projectId}/artifacts/uploads`,
    {
      body: {
        filename: "repo-browser.png",
        idempotency_key: `artifact-preview-smoke-${runId}`,
        kind: "attachment",
        mime_type: "image/png",
        name: "Artifact preview smoke image",
        sha256,
        size_bytes: source.byteLength,
        tags: ["smoke", "preview"],
      },
      headers: sessionHeaders,
      method: "POST",
    },
  );
  artifactId = upload.body.artifact_id;
  assert(
    upload.body.transfer_mode === "direct",
    `Expected direct MinIO transfer, received ${upload.body.transfer_mode}.`,
  );

  const signed = await jsonChecked(
    `${coreUrl}/v1/projects/${projectId}/artifacts/uploads/${upload.body.upload_id}/parts/sign`,
    {
      body: { part_numbers: [1] },
      headers: sessionHeaders,
      method: "POST",
    },
  );
  const part = signed.body.items?.[0];
  assert(part?.transfer?.url, "Core did not sign the MinIO part.");
  const putResponse = await fetch(part.transfer.url, {
    body: source,
    headers: part.transfer.headers,
    method: part.transfer.method,
    signal: AbortSignal.timeout(30_000),
  });
  assert(putResponse.ok, `MinIO part PUT returned HTTP ${putResponse.status}.`);
  const etag = putResponse.headers.get("etag");
  assert(etag, "MinIO part PUT did not return an ETag.");

  const recovered = await jsonChecked(
    `${coreUrl}/v1/projects/${projectId}/artifacts/uploads/${upload.body.upload_id}`,
    { headers: sessionHeaders },
  );
  assert(
    recovered.body.completed_parts?.length === 1,
    "Refresh-safe recovery did not discover the uploaded MinIO part.",
  );

  const confirmationUrl = `${coreUrl}/v1/projects/${projectId}/artifacts/uploads/${upload.body.upload_id}/confirm`;
  const confirmed = await jsonChecked(confirmationUrl, {
    body: { parts: [{ etag, part_number: 1 }] },
    headers: sessionHeaders,
    method: "POST",
  });
  const confirmedArtifactId = confirmed.body.artifact?.artifact_id;
  const versionId = confirmed.body.current_version?.version_id;
  assert(
    confirmedArtifactId === artifactId && versionId,
    "Upload confirmation did not return the initialized Artifact Version.",
  );

  const repeated = await jsonChecked(confirmationUrl, {
    body: { parts: [{ etag, part_number: 1 }] },
    headers: sessionHeaders,
    method: "POST",
  });
  assert(
    repeated.body.current_version?.version_id === versionId,
    "Repeated confirmation was not idempotent.",
  );

  const compose = resolveComposeCommand();
  const worker = spawnSync(
    compose.command,
    [
      ...compose.args,
      "-f",
      "deploy/compose/compose.yaml",
      "run",
      "--rm",
      "--no-deps",
      "-e",
      `MMDASH_WORKER_API_TOKEN=${workerToken}`,
      "-e",
      `MMDASH_WORKER_ID=artifact-preview-smoke-${runId}`,
      "worker",
      "--job-type",
      "artifact.preview",
      "--once",
    ],
    {
      encoding: "utf8",
      env: process.env,
      timeout: 120_000,
    },
  );
  assert(
    worker.status === 0,
    `Artifact preview Worker failed:\n${worker.stdout}\n${worker.stderr}`,
  );

  const previewUrl = `${coreUrl}/v1/projects/${projectId}/artifacts/${artifactId}/versions/${versionId}/previews`;
  const previews = await poll(
    async () =>
      (await jsonChecked(previewUrl, { headers: sessionHeaders })).body,
    (body) =>
      body.items?.some(
        (item) => item.preview_type === "image" && item.status === "available",
      ) &&
      body.items?.some(
        (item) =>
          item.preview_type === "thumbnail" &&
          item.status === "available" &&
          item.transfer?.url,
      ),
    "Worker did not publish the image summary and thumbnail.",
  );
  const image = previews.items.find((item) => item.preview_type === "image");
  assert(
    image.structural_summary?.width > 0 && image.structural_summary?.height > 0,
    "Image preview omitted bounded structural metadata.",
  );
  const thumbnail = previews.items.find(
    (item) => item.preview_type === "thumbnail",
  );
  const thumbnailResponse = await fetch(thumbnail.transfer.url, {
    headers: thumbnail.transfer.headers,
    method: thumbnail.transfer.method,
    signal: AbortSignal.timeout(20_000),
  });
  assert(
    thumbnailResponse.ok,
    `Thumbnail download returned HTTP ${thumbnailResponse.status}.`,
  );
  const thumbnailBytes = new Uint8Array(await thumbnailResponse.arrayBuffer());
  assert(
    thumbnailBytes.byteLength > 0 &&
      thumbnailBytes.byteLength <= 4 * 1024 * 1024,
    "Thumbnail output is empty or exceeded the configured bound.",
  );
  assert(
    isPng(thumbnailBytes) || isJpeg(thumbnailBytes),
    "Thumbnail output is not PNG or JPEG.",
  );

  console.log(
    JSON.stringify({
      artifact_id: artifactId,
      backend: "minio",
      preview_types: previews.items.map((item) => item.preview_type).sort(),
      project_id: projectId,
      status: "ok",
      version_id: versionId,
    }),
  );
} finally {
  if (artifactId && projectId && sessionHeaders) {
    await safeRequest(
      `${coreUrl}/v1/projects/${projectId}/artifacts/${artifactId}`,
      { headers: sessionHeaders, method: "DELETE" },
    );
    await safeRequest(
      `${coreUrl}/v1/projects/${projectId}/artifacts/${artifactId}/purge`,
      { headers: sessionHeaders, method: "DELETE" },
    );
  }
  if (tokenId && sessionHeaders) {
    await safeRequest(`${coreUrl}/v1/auth/tokens/${tokenId}`, {
      headers: sessionHeaders,
      method: "DELETE",
    });
  }
  if (projectId && sessionHeaders) {
    await safeRequest(`${coreUrl}/v1/projects/${projectId}`, {
      headers: sessionHeaders,
      method: "DELETE",
    });
  }
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
    signal: options.signal ?? AbortSignal.timeout(20_000),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(
      `${options.method ?? "GET"} ${url}: HTTP ${response.status} ${text}`,
    );
  }
  return response;
}

async function safeRequest(url, options) {
  try {
    const response = await fetch(url, {
      ...options,
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok && response.status !== 404 && response.status !== 409) {
      console.warn(`Cleanup ${options.method} ${url}: HTTP ${response.status}`);
    }
  } catch (error) {
    console.warn(`Cleanup ${options.method} ${url}: ${error.message}`);
  }
}

async function poll(load, ready, message) {
  let last;
  for (let attempt = 0; attempt < 30; attempt += 1) {
    last = await load();
    if (ready(last)) return last;
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error(`${message} Last state: ${JSON.stringify(last)}`);
}

function isPng(bytes) {
  return (
    bytes.byteLength >= 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47
  );
}

function isJpeg(bytes) {
  return bytes.byteLength >= 2 && bytes[0] === 0xff && bytes[1] === 0xd8;
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function trim(value) {
  return value.replace(/\/$/, "");
}
