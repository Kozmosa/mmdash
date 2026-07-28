import { readdir, readFile } from "node:fs/promises";

const catalog = await readFile("docs/api/endpoints.md", "utf8");
const contractDirectory = "contracts/openapi";
const contractFiles = (await readdir(contractDirectory))
  .filter((file) => file.endsWith(".yaml") || file.endsWith(".yml"))
  .sort();
const operationIds = [];

for (const contractFile of contractFiles) {
  const openapi = await readFile(`${contractDirectory}/${contractFile}`, "utf8");
  operationIds.push(
    ...[...openapi.matchAll(/^\s+operationId:\s+([A-Za-z0-9_.-]+)\s*$/gm)].map(
      (match) => match[1],
    ),
  );
}

if (operationIds.length === 0) {
  console.error("OpenAPI contracts have no operationId entries.");
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

console.log(
  `API catalog covers ${operationIds.length} operation(s) across ${contractFiles.length} contract(s).`,
);
