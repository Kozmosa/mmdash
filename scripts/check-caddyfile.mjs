import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";

const configurations = [
  {
    path: "Caddyfile",
    environment: {},
    requiredFragments: [
      "mmdash.moe {",
      "path /api/*",
      "reverse_proxy web-bff:3001",
      "path /mcp /mcp/*",
      "reverse_proxy mcp-gateway:3002",
      "path /v1/*",
      "reverse_proxy web:3000",
      "request_body {",
      "flush_interval -1",
      "health_uri",
      "format json",
      "Strict-Transport-Security",
      "X-Content-Type-Options",
      "X-Frame-Options",
    ],
    forbiddenFragments: ["path /box /box/*", "reverse_proxy core:8080"],
  },
  {
    path: "deploy/production/caddy/Caddyfile",
    environment: {
      CADDY_API_MAX_REQUEST_BODY: "64MB",
      CADDY_ARTIFACT_MAX_REQUEST_BODY: "64MB",
      CADDY_MCP_MAX_REQUEST_BODY: "16MB",
      MMDASH_PRODUCTION_HOST: "prod.mmdash.moe",
      OBJECT_STORAGE_BUCKET: "mmdash",
    },
    requiredFragments: [
      "auto_https off",
      "host {$MMDASH_PRODUCTION_HOST}",
      "path /api/*",
      "path /v1/*",
      "path /mcp /mcp/*",
      "path /{$OBJECT_STORAGE_BUCKET} /{$OBJECT_STORAGE_BUCKET}/*",
      "reverse_proxy web-bff:3001",
      "reverse_proxy mcp-gateway:3002",
      "reverse_proxy minio:9000",
      "reverse_proxy web:3000",
      "log_skip",
      "remote_ip 127.0.0.1 ::1",
      "header_up X-Forwarded-Proto https",
      "format json",
      "Strict-Transport-Security",
      "X-Content-Type-Options",
      "X-Frame-Options",
    ],
    forbiddenFragments: [
      "path /box /box/*",
      "reverse_proxy core:8080",
      "reverse_proxy minio:9001",
    ],
  },
];

for (const configuration of configurations) {
  await checkRequiredFragments(configuration);
  validateSyntax(configuration);
}

async function checkRequiredFragments(configuration) {
  const contents = await readFile(configuration.path, "utf8");
  const missing = configuration.requiredFragments.filter(
    (fragment) => !contents.includes(fragment),
  );

  if (missing.length > 0) {
    console.error(`${configuration.path} is missing: ${missing.join(", ")}`);
    process.exit(1);
  }

  const forbidden = configuration.forbiddenFragments.filter((fragment) =>
    contents.includes(fragment),
  );
  if (forbidden.length > 0) {
    console.error(
      `${configuration.path} exposes a forbidden boundary: ${forbidden.join(", ")}`,
    );
    process.exit(1);
  }
}

function validateSyntax(configuration) {
  const caddy = spawnSync(
    "caddy",
    ["validate", "--config", configuration.path, "--adapter", "caddyfile"],
    {
      encoding: "utf8",
      env: { ...process.env, ...configuration.environment },
    },
  );

  if (!caddy.error) {
    handleValidationResult(configuration.path, caddy);
    return;
  }
  if (caddy.error.code !== "ENOENT") {
    throw caddy.error;
  }

  const environmentArguments = Object.entries(
    configuration.environment,
  ).flatMap(([name, value]) => ["--env", `${name}=${value}`]);
  const docker = spawnSync(
    "docker",
    [
      "run",
      "--rm",
      ...environmentArguments,
      "--volume",
      `${process.cwd()}:/work:ro`,
      "--workdir",
      "/work",
      "caddy:2.10-alpine",
      "caddy",
      "validate",
      "--config",
      configuration.path,
      "--adapter",
      "caddyfile",
    ],
    { encoding: "utf8" },
  );
  if (docker.error?.code === "ENOENT") {
    console.error(
      "Caddy or Docker is required to validate the Caddyfile syntax.",
    );
    process.exit(1);
  }
  if (docker.error) {
    throw docker.error;
  }
  handleValidationResult(configuration.path, docker);
}

function handleValidationResult(path, result) {
  if (result.status !== 0) {
    process.stderr.write(result.stderr);
    process.exit(result.status ?? 1);
  }
  process.stdout.write(`${path}: ${result.stdout}`);
}
