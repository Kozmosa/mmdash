import { ApiError, type ApiClient } from "@/lib/api-client";

export async function optionalRequest<T>(
  client: ApiClient,
  path: string,
): Promise<T | null> {
  try {
    return await client.request<T>(path);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return null;
    }
    throw error;
  }
}
