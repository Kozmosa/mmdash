import type { components } from "./generated/core.js";

export type CoreRequestContext = {
  projectId?: string;
  requestId: string;
  userId?: string;
};

export type CoreRequestOptions = Omit<RequestInit, "body"> & {
  body?: BodyInit | Record<string, unknown> | null;
  duplex?: "half";
};

export type CoreErrorBody = {
  code?: string;
  message?: string;
  request_id?: string;
};

export class CoreClientError extends Error {
  readonly body: CoreErrorBody;
  readonly status: number;

  constructor(status: number, body: CoreErrorBody) {
    super(body.message ?? `Core request failed with HTTP ${status}`);
    this.name = "CoreClientError";
    this.status = status;
    this.body = body;
  }
}

export class CoreClient {
  readonly baseUrl: string;

  constructor(
    baseUrl: string,
    private readonly fetchImplementation: typeof fetch = fetch,
  ) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  async checkExample(
    context: CoreRequestContext,
  ): Promise<components["schemas"]["ExampleCheck"]> {
    return this.request<components["schemas"]["ExampleCheck"]>(
      "/v1/example",
      { method: "GET" },
      context,
    );
  }

  async request<T>(
    path: string,
    options: CoreRequestOptions,
    context: CoreRequestContext,
  ): Promise<T> {
    const response = await this.fetch(path, options, context);
    if (!response.ok) {
      throw new CoreClientError(response.status, await parseError(response));
    }
    if (response.status === 204) {
      return undefined as T;
    }
    return (await response.json()) as T;
  }

  async fetch(
    path: string,
    options: CoreRequestOptions,
    context: CoreRequestContext,
  ): Promise<Response> {
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    const headers = new Headers(options.headers);
    headers.set("accept", headers.get("accept") ?? "application/json");
    headers.set("x-request-id", context.requestId);
    if (context.projectId) {
      headers.set("x-mmdash-project-id", context.projectId);
    }
    if (context.userId) {
      headers.set("x-mmdash-user-id", context.userId);
    }

    let body = options.body;
    if (isJsonBody(body)) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(body);
    }

    return this.fetchImplementation(`${this.baseUrl}${normalizedPath}`, {
      ...options,
      body: body as BodyInit | null | undefined,
      headers,
    });
  }
}

function isJsonBody(
  body: CoreRequestOptions["body"],
): body is Record<string, unknown> {
  return (
    body !== undefined &&
    body !== null &&
    typeof body === "object" &&
    !(body instanceof ArrayBuffer) &&
    !(body instanceof Blob) &&
    !(body instanceof FormData) &&
    !(body instanceof URLSearchParams)
  );
}

async function parseError(response: Response): Promise<CoreErrorBody> {
  if (!response.headers.get("content-type")?.includes("application/json")) {
    return {};
  }
  try {
    return (await response.json()) as CoreErrorBody;
  } catch {
    return {};
  }
}
