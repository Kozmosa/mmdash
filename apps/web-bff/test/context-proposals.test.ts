import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";
const proposalId = "00000000-0000-4000-8000-000000000002";

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Context Proposal BFF routes", () => {
  it("lists proposals and proxies both human review decisions with provenance intact", async () => {
    const pending = proposalFixture();
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ items: [pending] }))
      .mockResolvedValueOnce(
        Response.json({
          ...pending,
          promoted_context_id: "00000000-0000-4000-8000-000000000005",
          reviewed_at: "2026-08-09T01:00:00Z",
          reviewed_by: "00000000-0000-4000-8000-000000000006",
          status: "accepted",
        }),
      )
      .mockResolvedValueOnce(
        Response.json({
          ...pending,
          review_note: "证据不足",
          reviewed_at: "2026-08-09T01:01:00Z",
          reviewed_by: "00000000-0000-4000-8000-000000000006",
          status: "rejected",
        }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const listed = await app.inject({
      headers: { cookie, "x-request-id": "context-list-request" },
      method: "GET",
      url: `/api/projects/${projectId}/context/proposals`,
    });

    expect(listed.statusCode).toBe(200);
    expect(listed.json().items[0]).toMatchObject({
      agent_run_id: pending.agent_run_id,
      agent_session_id: pending.agent_session_id,
      proposed_by_actor_id: pending.proposed_by_actor_id,
      proposed_by_actor_kind: "agent",
    });

    const accepted = await app.inject({
      headers: { cookie, "x-request-id": "context-accept-request" },
      method: "POST",
      payload: { decision: "accepted", review_note: "结论与证据一致" },
      url: `/api/projects/${projectId}/context/proposals/${proposalId}/review`,
    });
    expect(accepted.statusCode).toBe(200);
    expect(accepted.json()).toMatchObject({
      agent_run_id: pending.agent_run_id,
      agent_session_id: pending.agent_session_id,
      status: "accepted",
    });

    const rejected = await app.inject({
      headers: { cookie, "x-request-id": "context-reject-request" },
      method: "POST",
      payload: { decision: "rejected", review_note: "证据不足" },
      url: `/api/projects/${projectId}/context/proposals/${proposalId}/review`,
    });
    expect(rejected.statusCode).toBe(200);
    expect(rejected.json()).toMatchObject({
      agent_run_id: pending.agent_run_id,
      agent_session_id: pending.agent_session_id,
      status: "rejected",
    });

    const [listUrl, listOptions] = fetchImplementation.mock.calls[0]!;
    expect(listUrl).toBe(
      `http://core.test/v1/data/projects/${projectId}/context/proposals`,
    );
    expect(listOptions?.method).toBe("GET");
    expect(new Headers(listOptions?.headers).get("authorization")).toBe(
      "Bearer test-access-token",
    );

    const [acceptUrl, acceptOptions] = fetchImplementation.mock.calls[1]!;
    expect(acceptUrl).toBe(
      `http://core.test/v1/data/projects/${projectId}/context/proposals/${proposalId}/review`,
    );
    expect(JSON.parse(String(acceptOptions?.body))).toEqual({
      decision: "accepted",
      review_note: "结论与证据一致",
    });
    expect(new Headers(acceptOptions?.headers).get("x-request-id")).toBe(
      "context-accept-request",
    );

    const [, rejectOptions] = fetchImplementation.mock.calls[2]!;
    expect(JSON.parse(String(rejectOptions?.body))).toEqual({
      decision: "rejected",
      review_note: "证据不足",
    });
  });

  it("requires a browser session and rejects malformed review commands before Core", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const unauthenticated = await app.inject({
      method: "GET",
      url: `/api/projects/${projectId}/context/proposals`,
    });
    expect(unauthenticated.statusCode).toBe(401);

    const cookie = await signedSessionCookie(app);
    const malformed = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: { decision: "approved", unexpected: true },
      url: `/api/projects/${projectId}/context/proposals/${proposalId}/review`,
    });
    expect(malformed.statusCode).toBe(400);
    expect(fetchImplementation).not.toHaveBeenCalled();
  });
});

function proposalFixture() {
  return {
    agent_run_id: "00000000-0000-4000-8000-000000000004",
    agent_session_id: "00000000-0000-4000-8000-000000000003",
    content: "校准误差来自边界条件。",
    context_type: "finding",
    created_at: "2026-08-09T00:00:00Z",
    project_id: projectId,
    proposal_id: proposalId,
    proposed_by: "00000000-0000-4000-8000-000000000007",
    proposed_by_actor_id: "00000000-0000-4000-8000-000000000007",
    proposed_by_actor_kind: "agent",
    rationale: "Run 汇总了验证结果。",
    review_note: "",
    source_object_ids: ["00000000-0000-4000-8000-000000000008"],
    status: "pending",
    title: "校准误差结论",
    updated_at: "2026-08-09T00:00:00Z",
  };
}
