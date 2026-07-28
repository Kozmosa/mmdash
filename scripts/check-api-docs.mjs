import { readFile } from "node:fs/promises";

const openapi = await readFile("contracts/openapi/core.yaml", "utf8");
const catalog = await readFile("docs/api/endpoints.md", "utf8");
const operationIds = [
  ...openapi.matchAll(/^\s+operationId:\s+([A-Za-z0-9_.-]+)\s*$/gm),
].map((match) => match[1]);

if (operationIds.length === 0) {
  console.error("Core OpenAPI contract has no operationId entries.");
  process.exit(1);
}

const missing = operationIds.filter(
  (operationId) => !catalog.includes(`\`${operationId}\``),
);

if (missing.length > 0) {
  console.error(
    `docs/api/endpoints.md is missing operationIds: ${missing.join(", ")}`,
  );
  process.exit(1);
}

console.log(`API catalog covers ${operationIds.length} operation(s).`);
