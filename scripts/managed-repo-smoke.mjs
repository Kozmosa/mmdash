import { runManagedRepoSmoke } from "./repo-smoke.mjs";

const webUrl = trim(process.env.MMDASH_SMOKE_URL ?? "http://127.0.0.1:3001");
const coreUrl = trim(
  process.env.MMDASH_SMOKE_CORE_URL ?? "http://127.0.0.1:8080",
);
const email = trim(
  process.env.MMDASH_SMOKE_EMAIL ??
    process.env.AUTH_BOOTSTRAP_EMAIL ??
    "admin@mmdash.local",
);
const password =
  process.env.MMDASH_SMOKE_PASSWORD ??
  process.env.AUTH_BOOTSTRAP_PASSWORD ??
  "mmdash-local-admin";
const runId = `${Date.now()}-${Math.random().toString(16).slice(2)}`;

const result = await runManagedRepoSmoke({
  coreUrl,
  email,
  expectServerExistingDisabled:
    process.env.MMDASH_SMOKE_EXPECT_SERVER_REPO_DISABLED === "1",
  password,
  runId,
  webUrl,
});

console.log(
  JSON.stringify({
    managed_repository: result,
    status: "ok",
  }),
);

function trim(value) {
  return value.trim().replace(/\/$/, "");
}
