import {
  Ajv2020,
  type AnySchema,
  type ErrorObject,
  type Options,
  type ValidateFunction,
} from "ajv/dist/2020.js";
import formatsPlugin from "ajv-formats";

export type ValidationIssue = {
  keyword: string;
  message: string;
  path: string;
};

export type SchemaValidator<T> = {
  validate(value: unknown): value is T;
  issues(): ValidationIssue[];
};

export function createSchemaValidator<T>(
  schema: AnySchema,
  referencedSchemas: AnySchema[] = [],
  options: Options = {},
): SchemaValidator<T> {
  const ajv = new Ajv2020({
    allErrors: true,
    strict: true,
    ...options,
  });
  const addFormats = formatsPlugin as unknown as (instance: Ajv2020) => Ajv2020;
  addFormats(ajv);
  for (const referenced of referencedSchemas) {
    ajv.addSchema(referenced);
  }
  const validate = ajv.compile<T>(schema);
  return {
    issues: () => formatValidationErrors(validate),
    validate: (value: unknown): value is T => validate(value) === true,
  };
}

export function formatValidationErrors(
  validate: Pick<ValidateFunction, "errors">,
): ValidationIssue[] {
  return (validate.errors ?? []).map((error: ErrorObject) => ({
    keyword: error.keyword,
    message: error.message ?? "is invalid",
    path: error.instancePath || "/",
  }));
}
