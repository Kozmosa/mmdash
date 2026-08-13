import { spawnSync } from "node:child_process";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import yaml from "js-yaml";

import {
  createContractMockServer,
  loadMockFixtures,
} from "./contract-mock.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const catalog = await readJSON("contracts/openapi/catalog.json");
const documents = new Map();

for (const entry of catalog.contracts) {
  const document = await readYAML(`contracts/openapi/${entry.source}`);
  validateOpenAPIRoot(entry.id, document);
  documents.set(entry.id, document);
}

await validateJSONSchemas();
validateMockFixtures(documents.get("core"), await loadMockFixtures());
await validateMockServer();
await checkGeneratedFiles();

console.log(
  `Contract checks passed for ${documents.size} OpenAPI documents and the shared JSON Schemas.`,
);

function validateOpenAPIRoot(id, document) {
  if (document.openapi !== "3.1.0") {
    fail(`${id}: OpenAPI version must be 3.1.0`);
  }
  if (!document.info?.title || !document.info?.version) {
    fail(`${id}: info.title and info.version are required`);
  }
  const operationIds = new Set();
  for (const [route, pathItem] of Object.entries(document.paths ?? {})) {
    for (const method of [
      "get",
      "post",
      "put",
      "patch",
      "delete",
      "head",
      "options",
    ]) {
      const operation = pathItem[method];
      if (!operation) continue;
      if (!operation.operationId) {
        fail(`${id}: ${method.toUpperCase()} ${route} has no operationId`);
      }
      if (operationIds.has(operation.operationId)) {
        fail(`${id}: duplicate operationId ${operation.operationId}`);
      }
      operationIds.add(operation.operationId);
      if (Object.keys(operation.responses ?? {}).length === 0) {
        fail(`${id}: ${operation.operationId} has no responses`);
      }
    }
  }
  if (operationIds.size === 0) {
    fail(`${id}: no operations found`);
  }
}

async function validateJSONSchemas() {
  const [common, envelope, articleTemplate, articleTemplateExample, errorExample, eventExample] = await Promise.all([
    readJSON("contracts/json-schema/common/dtos.schema.json"),
    readJSON("contracts/events/event-envelope.schema.json"),
    readJSON("contracts/json-schema/article-template.schema.json"),
    readJSON("contracts/json-schema/examples/article-template.valid.json"),
    readJSON("contracts/examples/error.example.json"),
    readJSON("contracts/examples/event-envelope.example.json"),
  ]);
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  addFormats(ajv);
  ajv.addSchema(common);
  const validateArticleTemplate = ajv.compile(articleTemplate);
  if (!validateArticleTemplate(articleTemplateExample)) {
    fail(`article template example: ${ajv.errorsText(validateArticleTemplate.errors)}`);
  }
  const eventFiles = (
    await readdir(path.join(root, "contracts/events"))
  ).filter(
    (file) =>
      file.endsWith(".schema.json") && file !== "event-envelope.schema.json",
  );
  for (const eventFile of eventFiles.sort()) {
    const eventPayload = await readJSON(`contracts/events/${eventFile}`);
    try {
      ajv.compile(eventPayload);
    } catch (error) {
      fail(`${eventFile}: ${error}`);
    }
  }
  const validateEnvelope = ajv.compile(envelope);
  if (!validateEnvelope(eventExample)) {
    fail(`event-envelope example: ${ajv.errorsText(validateEnvelope.errors)}`);
  }
  const validateError = ajv.getSchema(`${common.$id}#/$defs/Error`);
  if (!validateError || !validateError(errorExample)) {
    fail(`error example: ${ajv.errorsText(validateError?.errors)}`);
  }
}

function validateMockFixtures(document, fixtures) {
  const operations = operationIndex(document);
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  addFormats(ajv);
  for (const [operationId, fixture] of Object.entries(fixtures)) {
    const operation = operations.get(operationId);
    if (!operation) {
      fail(`mock fixture references unknown operationId ${operationId}`);
    }
    if (
      operation.method !== fixture.method ||
      operation.path !== fixture.path
    ) {
      fail(`mock fixture ${operationId} method/path differs from OpenAPI`);
    }
    const response = operation.operation.responses?.[String(fixture.status)];
    const schema = response?.content?.["application/json"]?.schema;
    if (!schema) {
      fail(`mock fixture ${operationId} has no matching JSON response schema`);
    }
    const resolved = dereference(schema, document);
    const validate = ajv.compile(resolved);
    if (!validate(fixture.body)) {
      fail(
        `mock fixture ${operationId}: ${ajv.errorsText(validate.errors, {
          separator: "; ",
        })}`,
      );
    }
  }
}

async function validateMockServer() {
  const server = await createContractMockServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  try {
    const address = server.address();
    const response = await fetch(
      `http://127.0.0.1:${address.port}/v1/projects`,
    );
    const body = await response.json();
    if (
      response.status !== 200 ||
      response.headers.get("x-mmdash-operation-id") !== "projects.list" ||
      !Array.isArray(body.items)
    ) {
      fail("contract mock did not serve the projects.list fixture");
    }
  } finally {
    await new Promise((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
}

async function checkGeneratedFiles() {
  const generator = spawnSync(
    process.execPath,
    [path.join(root, "scripts/generate-contracts.mjs"), "--check"],
    { cwd: root, encoding: "utf8" },
  );
  if (generator.status !== 0) {
    fail(generator.stderr || generator.stdout);
  }

  const generated = spawnSync(
    process.execPath,
    [path.join(root, "scripts/generate-core-client.mjs"), "--check"],
    { cwd: root, encoding: "utf8" },
  );
  if (generated.status !== 0) {
    fail(generated.stderr || generated.stdout);
  }
}

function operationIndex(document) {
  const index = new Map();
  for (const [route, pathItem] of Object.entries(document.paths ?? {})) {
    for (const method of ["get", "post", "put", "patch", "delete", "head"]) {
      if (pathItem[method]?.operationId) {
        index.set(pathItem[method].operationId, {
          method: method.toUpperCase(),
          operation: pathItem[method],
          path: route,
        });
      }
    }
  }
  return index;
}

function dereference(value, document, seen = new Set()) {
  if (Array.isArray(value)) {
    return value.map((item) => dereference(item, document, seen));
  }
  if (!value || typeof value !== "object") {
    return value;
  }
  if (value.$ref?.startsWith("#/")) {
    if (seen.has(value.$ref)) {
      return {};
    }
    const target = value.$ref
      .slice(2)
      .split("/")
      .reduce((current, segment) => current?.[segment], document);
    return dereference(target, document, new Set([...seen, value.$ref]));
  }
  return Object.fromEntries(
    Object.entries(value).map(([key, item]) => [
      key,
      dereference(item, document, seen),
    ]),
  );
}

async function readJSON(relativePath) {
  return JSON.parse(await readFile(path.join(root, relativePath), "utf8"));
}

async function readYAML(relativePath) {
  return yaml.load(await readFile(path.join(root, relativePath), "utf8"));
}

function fail(message) {
  console.error(String(message).trim());
  process.exit(1);
}
