import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const source = resolve("scripts/article-worker-smoke.py");
const result = spawnSync(
  "docker",
  [
    "run",
    "--rm",
    "-i",
    "--network",
    "none",
    "--entrypoint",
    "/app/.venv/bin/python",
    "mmdash-worker",
    "-",
  ],
  {
    encoding: "utf8",
    input: readFileSync(source),
    stdio: ["pipe", "inherit", "inherit"],
  },
);

if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);
