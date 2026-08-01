import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";

const contents = await readFile("Caddyfile", "utf8");
const requiredFragments = [
  "mmdash.moe {",
  "path /api/*",
  "reverse_proxy web-bff:3001",
  "path /mcp /mcp/*",
  "reverse_proxy mcp-gateway:3002",
  "path /v1/*",
  "path /box /box/*",
  "reverse_proxy core:8080",
  "reverse_proxy web:3000",
  "request_body {",
  "flush_interval -1",
  "health_uri",
  "format json",
  "Strict-Transport-Security",
  "X-Content-Type-Options",
  "X-Frame-Options",
];
const missing = requiredFragments.filter(
  (fragment) => !contents.includes(fragment),
);

if (missing.length > 0) {
  console.error(`Caddyfile is missing: ${missing.join(", ")}`);
  process.exit(1);
}

const caddy = spawnSync(
  "caddy",
  ["validate", "--config", "Caddyfile", "--adapter", "caddyfile"],
  { encoding: "utf8" },
);

if (!caddy.error) {
  if (caddy.status !== 0) {
    process.stderr.write(caddy.stderr);
    process.exit(caddy.status ?? 1);
  }
  process.stdout.write(caddy.stdout);
} else if (caddy.error.code === "ENOENT") {
  const docker = spawnSync(
    "docker",
    [
      "run",
      "--rm",
      "--volume",
      `${process.cwd()}:/work:ro`,
      "--workdir",
      "/work",
      "caddy:2.10-alpine",
      "caddy",
      "validate",
      "--config",
      "Caddyfile",
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
  if (docker.status !== 0) {
    process.stderr.write(docker.stderr);
    process.exit(docker.status ?? 1);
  }
  process.stdout.write(docker.stdout);
} else {
  throw caddy.error;
}
