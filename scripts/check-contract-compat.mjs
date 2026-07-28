import { readdir, readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import yaml from "js-yaml";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const baselinePath = path.join(root, "contracts/compatibility/baseline.json");
const current = await createSnapshot();

if (process.argv.includes("--write")) {
  await mkdir(path.dirname(baselinePath), { recursive: true });
  await writeFile(
    baselinePath,
    `${JSON.stringify(current, null, 2)}\n`,
    "utf8",
  );
  console.log("updated contracts/compatibility/baseline.json");
  process.exit(0);
}

const baseline = JSON.parse(await readFile(baselinePath, "utf8"));
const errors = compareSnapshots(baseline, current);
if (errors.length > 0) {
  console.error("Breaking contract changes detected:");
  for (const error of errors) {
    console.error(`- ${error}`);
  }
  process.exit(1);
}
console.log("Contract compatibility baseline passed.");

async function createSnapshot() {
  const catalog = await readJSON("contracts/openapi/catalog.json");
  const openapi = {};
  for (const entry of catalog.contracts) {
    const document = yaml.load(
      await readFile(
        path.join(root, "contracts/openapi", entry.source),
        "utf8",
      ),
    );
    openapi[entry.id] = snapshotOpenAPI(document);
  }
  const jsonSchema = {};
  for (const directory of ["contracts/events", "contracts/json-schema"]) {
    for (const file of await jsonFiles(path.join(root, directory))) {
      const relative = path.relative(root, file).replaceAll("\\", "/");
      jsonSchema[relative] = schemaSignature(
        JSON.parse(await readFile(file, "utf8")),
      );
    }
  }
  return { jsonSchema, openapi, version: 1 };
}

function snapshotOpenAPI(document) {
  const operations = {};
  for (const [route, pathItem] of Object.entries(document.paths ?? {})) {
    for (const method of ["get", "post", "put", "patch", "delete", "head"]) {
      const operation = pathItem[method];
      if (!operation) continue;
      const parameters = [
        ...(pathItem.parameters ?? []),
        ...(operation.parameters ?? []),
      ]
        .filter((parameter) => !parameter.$ref)
        .map((parameter) => ({
          in: parameter.in,
          name: parameter.name,
          required: Boolean(parameter.required),
        }))
        .sort((left, right) =>
          `${left.in}:${left.name}`.localeCompare(`${right.in}:${right.name}`),
        );
      operations[operation.operationId] = {
        method: method.toUpperCase(),
        parameters,
        path: route,
        requestRequired: Boolean(operation.requestBody?.required),
        responses: Object.keys(operation.responses ?? {}).sort(),
      };
    }
  }
  const schemas = Object.fromEntries(
    Object.entries(document.components?.schemas ?? {})
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([name, schema]) => [name, schemaSignature(schema)]),
  );
  return { operations, schemas };
}

function schemaSignature(schema) {
  if (schema === true || schema === false) {
    return schema;
  }
  if (!schema || typeof schema !== "object") {
    return {};
  }
  const signature = {};
  for (const key of [
    "$ref",
    "type",
    "format",
    "pattern",
    "minimum",
    "maximum",
    "minLength",
    "maxLength",
    "minItems",
    "maxItems",
    "additionalProperties",
  ]) {
    if (schema[key] !== undefined) {
      signature[key] =
        key === "additionalProperties" && typeof schema[key] === "object"
          ? schemaSignature(schema[key])
          : schema[key];
    }
  }
  if (Array.isArray(schema.enum)) {
    signature.enum = [...schema.enum].sort();
  }
  if (Array.isArray(schema.required)) {
    signature.required = [...schema.required].sort();
  }
  if (schema.properties) {
    signature.properties = Object.fromEntries(
      Object.entries(schema.properties)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([name, property]) => [name, schemaSignature(property)]),
    );
  }
  if (schema.$defs) {
    signature.$defs = Object.fromEntries(
      Object.entries(schema.$defs)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([name, definition]) => [name, schemaSignature(definition)]),
    );
  }
  for (const key of ["items", "anyOf", "oneOf", "allOf"]) {
    if (schema[key]) {
      signature[key] = Array.isArray(schema[key])
        ? schema[key].map(schemaSignature)
        : schemaSignature(schema[key]);
    }
  }
  return signature;
}

function compareSnapshots(baseline, current) {
  const errors = [];
  for (const [documentId, oldDocument] of Object.entries(
    baseline.openapi ?? {},
  )) {
    const newDocument = current.openapi?.[documentId];
    if (!newDocument) {
      errors.push(`OpenAPI document removed: ${documentId}`);
      continue;
    }
    for (const [operationId, oldOperation] of Object.entries(
      oldDocument.operations ?? {},
    )) {
      const next = newDocument.operations?.[operationId];
      if (!next) {
        errors.push(`${documentId}: operation removed: ${operationId}`);
        continue;
      }
      if (
        next.method !== oldOperation.method ||
        next.path !== oldOperation.path
      ) {
        errors.push(`${documentId}: operation moved: ${operationId}`);
      }
      if (!oldOperation.requestRequired && next.requestRequired) {
        errors.push(`${documentId}: request became required: ${operationId}`);
      }
      for (const status of oldOperation.responses ?? []) {
        if (!next.responses.includes(status)) {
          errors.push(
            `${documentId}: response ${status} removed from ${operationId}`,
          );
        }
      }
      const oldParameters = new Map(
        (oldOperation.parameters ?? []).map((parameter) => [
          `${parameter.in}:${parameter.name}`,
          parameter,
        ]),
      );
      for (const parameter of next.parameters ?? []) {
        const old = oldParameters.get(`${parameter.in}:${parameter.name}`);
        if ((!old || !old.required) && parameter.required) {
          errors.push(
            `${documentId}: required parameter added to ${operationId}: ${parameter.in}.${parameter.name}`,
          );
        }
      }
    }
    compareSchemaMaps(
      `${documentId} component`,
      oldDocument.schemas ?? {},
      newDocument.schemas ?? {},
      errors,
    );
  }
  compareSchemaMaps(
    "JSON Schema",
    baseline.jsonSchema ?? {},
    current.jsonSchema ?? {},
    errors,
  );
  return errors;
}

function compareSchemaMaps(label, oldSchemas, newSchemas, errors) {
  for (const [name, oldSchema] of Object.entries(oldSchemas)) {
    const next = newSchemas[name];
    if (next === undefined) {
      errors.push(`${label} removed: ${name}`);
      continue;
    }
    compareSchema(`${label} ${name}`, oldSchema, next, errors);
  }
}

function compareSchema(label, oldSchema, next, errors) {
  if (
    typeof oldSchema !== "object" ||
    oldSchema === null ||
    typeof next !== "object" ||
    next === null
  ) {
    if (JSON.stringify(oldSchema) !== JSON.stringify(next)) {
      errors.push(`${label} changed incompatibly`);
    }
    return;
  }
  for (const key of ["$ref", "type", "format"]) {
    if (
      oldSchema[key] !== undefined &&
      JSON.stringify(oldSchema[key]) !== JSON.stringify(next[key])
    ) {
      errors.push(`${label}: ${key} changed`);
    }
  }
  for (const value of oldSchema.enum ?? []) {
    if (!(next.enum ?? []).includes(value)) {
      errors.push(`${label}: enum value removed: ${value}`);
    }
  }
  const oldRequired = new Set(oldSchema.required ?? []);
  for (const field of next.required ?? []) {
    if (!oldRequired.has(field)) {
      errors.push(`${label}: required field added: ${field}`);
    }
  }
  for (const [field, oldProperty] of Object.entries(
    oldSchema.properties ?? {},
  )) {
    if (next.properties?.[field] === undefined) {
      errors.push(`${label}: property removed: ${field}`);
    } else {
      compareSchema(
        `${label}.${field}`,
        oldProperty,
        next.properties[field],
        errors,
      );
    }
  }
  for (const [definition, oldValue] of Object.entries(oldSchema.$defs ?? {})) {
    if (next.$defs?.[definition] === undefined) {
      errors.push(`${label}: definition removed: ${definition}`);
    } else {
      compareSchema(
        `${label}#${definition}`,
        oldValue,
        next.$defs[definition],
        errors,
      );
    }
  }
}

async function jsonFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await jsonFiles(absolute)));
    } else if (entry.name.endsWith(".json")) {
      files.push(absolute);
    }
  }
  return files.sort();
}

async function readJSON(relativePath) {
  return JSON.parse(await readFile(path.join(root, relativePath), "utf8"));
}
