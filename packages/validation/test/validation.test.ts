import { readFile } from "node:fs/promises";

import { describe, expect, it } from "vitest";

import { createSchemaValidator } from "../src/index.js";

describe("JSON Schema validation", () => {
  it("validates the stable event envelope and returns normalized issues", async () => {
    const [eventSchema, commonSchema, example] = await Promise.all([
      readJSON("../../../contracts/events/event-envelope.schema.json"),
      readJSON("../../../contracts/json-schema/common/dtos.schema.json"),
      readJSON("../../../contracts/examples/event-envelope.example.json"),
    ]);
    const validator = createSchemaValidator(eventSchema, [commonSchema]);

    expect(validator.validate(example)).toBe(true);
    expect(validator.issues()).toEqual([]);
    expect(
      validator.validate({
        ...example,
        event_type: "invalid",
        payload: "not-an-object",
      }),
    ).toBe(false);
    expect(validator.issues()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "/event_type" }),
        expect.objectContaining({ path: "/payload" }),
      ]),
    );
  });
});

async function readJSON(relativePath: string): Promise<object> {
  return JSON.parse(
    await readFile(new URL(relativePath, import.meta.url), "utf8"),
  ) as object;
}
