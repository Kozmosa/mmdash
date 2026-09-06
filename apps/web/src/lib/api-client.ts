export type ApiErrorBody = {
  code?: string;
  details?: unknown;
  message?: string;
  request_id?: string;
};

export class ApiError extends Error {
  readonly code: string;
  readonly details?: unknown;
  readonly requestId?: string;
  readonly retryable: boolean;
  readonly status: number;

  constructor(options: {
    code?: string;
    details?: unknown;
    message: string;
    requestId?: string;
    status: number;
  }) {
    super(options.message);
    this.name = "ApiError";
    this.code = options.code ?? "HTTP_ERROR";
    this.details = options.details;
    this.requestId = options.requestId;
    this.retryable = retryableDetail(options.details);
    this.status = options.status;
  }
}

type QueryValue = boolean | number | string | null | undefined;

const publicPagePrefixes = ["/login", "/register", "/invite", "/downloads"];
const unauthenticatedRequestPrefixes = [
  "/auth/login",
  "/auth/register",
  "/auth/invitations/preview",
];

export function shouldRedirectToLogin(
  status: number,
  requestPath: string,
  currentPathname: string,
): boolean {
  return (
    status === 401 &&
    !unauthenticatedRequestPrefixes.some((prefix) =>
      requestPath.startsWith(prefix),
    ) &&
    !isPublicPage(currentPathname)
  );
}

function isPublicPage(pathname: string): boolean {
  return (
    pathname === "/" ||
    publicPagePrefixes.some((prefix) => pathname.startsWith(prefix))
  );
}

export type ApiRequestOptions = Omit<RequestInit, "body"> & {
  body?: BodyInit | Record<string, unknown> | null;
  query?: Record<string, QueryValue>;
};

export class ApiClient {
  constructor(
    private readonly baseUrl = "/api",
    private readonly fetchImplementation: typeof fetch = (input, init) =>
      globalThis.fetch(input, init),
  ) {}

  async request<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
    const url = this.buildUrl(path, options.query);
    const headers = new Headers(options.headers);
    let body = options.body;

    if (
      body !== undefined &&
      body !== null &&
      !(body instanceof Blob) &&
      !(body instanceof FormData) &&
      !(body instanceof URLSearchParams) &&
      typeof body !== "string"
    ) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(body);
    }

    headers.set("accept", "application/json");
    const response = await this.fetchImplementation(url, {
      ...options,
      body: body as BodyInit | null | undefined,
      credentials: options.credentials ?? "include",
      headers,
    });

    if (!response.ok) {
      const error = await parseErrorBody(response);
      if (
        typeof window !== "undefined" &&
        shouldRedirectToLogin(response.status, path, window.location.pathname)
      ) {
        const returnTo = `${window.location.pathname}${window.location.search}`;
        window.location.assign(
          `/login?returnTo=${encodeURIComponent(returnTo)}`,
        );
      }
      throw new ApiError({
        code: error.code,
        details: error.details,
        message: error.message ?? `Request failed with HTTP ${response.status}`,
        requestId:
          error.request_id ?? response.headers.get("x-request-id") ?? undefined,
        status: response.status,
      });
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  }

  private buildUrl(
    path: string,
    query: Record<string, QueryValue> | undefined,
  ): string {
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    const url = `${this.baseUrl.replace(/\/$/, "")}${normalizedPath}`;
    if (!query) {
      return url;
    }

    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== null) {
        search.set(key, String(value));
      }
    }
    const queryString = search.toString();
    return queryString ? `${url}?${queryString}` : url;
  }
}

function retryableDetail(details: unknown): boolean {
  return (
    typeof details === "object" &&
    details !== null &&
    "retryable" in details &&
    details.retryable === true
  );
}

async function parseErrorBody(response: Response): Promise<ApiErrorBody> {
  const contentType = response.headers.get("content-type");
  if (!contentType?.includes("application/json")) {
    return {};
  }

  try {
    return (await response.json()) as ApiErrorBody;
  } catch {
    return {};
  }
}

export const apiClient = new ApiClient();
