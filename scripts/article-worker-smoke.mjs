import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

const source = resolve("scripts/article-worker-smoke.py");
const result = spawnSync(
  "docker",
  [
    "run",
    "--rm",
    "--network",
    "none",
    "--mount",
    `type=bind,source=${source},target=/smoke/article-worker-smoke.py,readonly`,
    "--entrypoint",
    "/app/.venv/bin/python",
    "mmdash-worker",
    "/smoke/article-worker-smoke.py",
  ],
  { encoding: "utf8", stdio: "inherit" },
);

if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);
