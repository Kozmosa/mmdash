import type { Principal } from "../auth/token-authenticator.js";
import { GatewayError } from "../errors/gateway-error.js";

export interface GatewayAuthorizer {
  assertProjectAccess(principal: Principal, projectId: string): void;
  assertToolAccess(principal: Principal, toolName: string): void;
  canAccessProject(principal: Principal, projectId: string): boolean;
  canAccessTool(principal: Principal, toolName: string): boolean;
}

export class PatternAuthorizer implements GatewayAuthorizer {
  assertProjectAccess(principal: Principal, projectId: string): void {
    if (!this.canAccessProject(principal, projectId)) {
      throw new GatewayError(
        "PROJECT_ACCESS_DENIED",
        "The token cannot access this project",
        403,
      );
    }
  }

  canAccessProject(principal: Principal, projectId: string): boolean {
    return matches(principal.projects, projectId);
  }

  assertToolAccess(principal: Principal, toolName: string): void {
    if (!this.canAccessTool(principal, toolName)) {
      throw new GatewayError(
        "TOOL_ACCESS_DENIED",
        "The token cannot call this tool",
        403,
      );
    }
  }

  canAccessTool(principal: Principal, toolName: string): boolean {
    return matches(principal.tools, toolName);
  }
}

function matches(patterns: readonly string[], value: string): boolean {
  return patterns.some(
    (pattern) =>
      pattern === "*" ||
      pattern === value ||
      (pattern.endsWith("*") && value.startsWith(pattern.slice(0, -1))),
  );
}
