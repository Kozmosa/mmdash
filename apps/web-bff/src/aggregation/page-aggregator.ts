import type { CoreClient } from "@mmdash/core-client";

import type { BrowserIdentity } from "../auth/browser-auth.js";
import { BffError } from "../errors/bff-error.js";

export type PageAggregationContext = {
  coreClient: CoreClient;
  identity: BrowserIdentity;
  projectId: string;
  requestId: string;
};

export type PageContributor = {
  id: string;
  load: (context: PageAggregationContext) => Promise<unknown>;
};

export type PageAggregation = {
  fragments: Record<string, unknown>;
  page_id: string;
  project_id: string;
  request_id: string;
};

export class PageAggregatorRegistry {
  private readonly pages = new Map<string, readonly PageContributor[]>();

  register(pageId: string, contributors: readonly PageContributor[]): void {
    if (this.pages.has(pageId)) {
      throw new Error(`Page aggregator "${pageId}" is already registered`);
    }
    const contributorIds = contributors.map((contributor) => contributor.id);
    if (new Set(contributorIds).size !== contributorIds.length) {
      throw new Error(`Page aggregator "${pageId}" has duplicate fragments`);
    }
    this.pages.set(pageId, [...contributors]);
  }

  async aggregate(
    pageId: string,
    context: PageAggregationContext,
  ): Promise<PageAggregation> {
    const contributors = this.pages.get(pageId);
    if (!contributors) {
      throw new BffError({
        code: "PAGE_AGGREGATOR_NOT_FOUND",
        message: "The requested page aggregation is not registered",
        status: 404,
      });
    }

    const values = await Promise.all(
      contributors.map((contributor) => contributor.load(context)),
    );
    return {
      fragments: Object.fromEntries(
        contributors.map((contributor, index) => [
          contributor.id,
          values[index],
        ]),
      ),
      page_id: pageId,
      project_id: context.projectId,
      request_id: context.requestId,
    };
  }
}

export function createDefaultPageRegistry(): PageAggregatorRegistry {
  const registry = new PageAggregatorRegistry();
  registry.register("workspace-shell", [
    {
      id: "context",
      load: async ({ identity, projectId }) => ({
        project: { id: projectId },
        user: {
          display_name: identity.displayName,
          email: identity.email,
          id: identity.userId,
        },
      }),
    },
  ]);
  registry.register("home", [
    {
      id: "home",
      load: async ({ coreClient, identity, projectId, requestId }) =>
        coreClient.getProjectHome(projectId, {
          accessToken: identity.accessToken,
          projectId,
          requestId,
          userId: identity.userId,
        }),
    },
    {
      id: "project",
      load: async ({ coreClient, identity, projectId, requestId }) =>
        coreClient.getProject(projectId, {
          accessToken: identity.accessToken,
          projectId,
          requestId,
          userId: identity.userId,
        }),
    },
  ]);
  return registry;
}
