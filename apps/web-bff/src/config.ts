import { z } from "zod";

const configSchema = z
  .object({
    BFF_COOKIE_SECRET: z.string().min(32),
    BFF_HOST: z.string().min(1).default("0.0.0.0"),
    BFF_PORT: z.coerce.number().int().positive().max(65_535).default(3001),
    CORE_BASE_URL: z.string().url().default("http://localhost:8080"),
    NODE_ENV: z
      .enum(["development", "production", "test"])
      .default("development"),
  })
  .superRefine((config, context) => {
    if (
      config.NODE_ENV === "production" &&
      config.BFF_COOKIE_SECRET === defaultDevelopmentSecret
    ) {
      context.addIssue({
        code: "custom",
        message: "BFF_COOKIE_SECRET must be changed in production",
        path: ["BFF_COOKIE_SECRET"],
      });
    }
  });

const defaultDevelopmentSecret =
  "development-only-cookie-secret-change-me";

export type BffConfig = {
  cookieSecret: string;
  coreBaseUrl: string;
  host: string;
  nodeEnv: "development" | "production" | "test";
  port: number;
};

export function loadConfig(
  environment: NodeJS.ProcessEnv = process.env,
): BffConfig {
  const parsed = configSchema.parse({
    ...environment,
    BFF_COOKIE_SECRET:
      environment.BFF_COOKIE_SECRET ?? defaultDevelopmentSecret,
  });
  return {
    cookieSecret: parsed.BFF_COOKIE_SECRET,
    coreBaseUrl: parsed.CORE_BASE_URL,
    host: parsed.BFF_HOST,
    nodeEnv: parsed.NODE_ENV,
    port: parsed.BFF_PORT,
  };
}
