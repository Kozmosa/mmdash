import "fastify";

import type { BrowserIdentity } from "../auth/browser-auth.js";

declare module "fastify" {
  interface FastifyContextConfig {
    auth?: "public" | "required";
    project?: "none" | "optional" | "required";
  }

  interface FastifyRequest {
    browserIdentity?: BrowserIdentity;
    currentProjectId?: string;
  }
}
