import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ExperimentCard } from "@/features/experiment/experiment-ui";
import type { Experiment } from "@/features/experiment/types";

const base: Experiment = {
  experiment_id: "00000000-0000-4000-8000-0000000000e1",
  project_id: "00000000-0000-4000-8000-000000000001",
  name: "Q3 求解实验",
  experiment_type: "box",
  execution_status: "running",
  created_by: "00000000-0000-4000-8000-000000000009",
  source_commit: "a".repeat(40),
  entrypoint: "python:run.py",
  parameters: {},
  environment: {},
  inputs: {},
  requested_runtime_policy: "auto",
  limits: {
    cpu_millis: 1000,
    memory_bytes: 1 << 30,
    timeout_seconds: 3600,
    disk_bytes: 1 << 30,
    pids: 128,
    network: "enabled",
  },
  project_timezone: "Asia/Shanghai",
  result_directory: "experiments/demo/",
  retry: {
    retry_of_experiment_id: undefined,
    root_experiment_id: "00000000-0000-4000-8000-0000000000e0",
    superseded_by_experiment_id: undefined,
    latest_experiment_id: "00000000-0000-4000-8000-0000000000e1",
    retry_sequence: 0,
  },
  logs_truncated: false,
  progress: 40,
  created_at: "2026-09-08T00:00:00Z",
  updated_at: "2026-09-08T00:10:00Z",
};

function renderCard(item: Experiment) {
  const noop = () => undefined;
  return render(
    <ExperimentCard
      checked={false}
      compareMode={false}
      item={item}
      onCancel={noop}
      onCompare={noop}
      onRerun={noop}
      onRun={noop}
      onSelect={noop}
    />,
  );
}

afterEach(cleanup);

describe("ExperimentCard", () => {
  it("renders one segment per pipeline stage with the current stage label", () => {
    renderCard(base);
    const strip = screen.getByLabelText("阶段进度：运行中");
    expect(strip).toBeInTheDocument();
    // created..succeeded = 8 segments
    expect(strip.childElementCount).toBe(8);
  });

  it("labels the strip with the terminal failure state", () => {
    renderCard({ ...base, execution_status: "failed" });
    expect(screen.getByLabelText("阶段进度：失败")).toBeInTheDocument();
  });

  it("shows the retry chain with links once the experiment is a rerun", () => {
    renderCard({
      ...base,
      retry: {
        retry_of_experiment_id: "00000000-0000-4000-8000-0000000000e0",
        root_experiment_id: "00000000-0000-4000-8000-0000000000e0",
        superseded_by_experiment_id: "00000000-0000-4000-8000-0000000000e2",
        latest_experiment_id: "00000000-0000-4000-8000-0000000000e2",
        retry_sequence: 2,
      },
    });
    expect(screen.getByText(/第 2 次/)).toBeInTheDocument();
    // root and "based on" share the 8-char prefix in this fixture
    // every fixture UUID shares the 8-char display prefix; assert by href
    const chainLinks = screen
      .getAllByRole("link", { name: "00000000" })
      .map((link) => link.getAttribute("href") ?? "");
    expect(chainLinks).toHaveLength(3);
    expect(
      chainLinks.filter((href) => href.includes("0000000000e0")),
    ).toHaveLength(2);
    expect(
      chainLinks.filter((href) => href.includes("0000000000e2")),
    ).toHaveLength(1);
    expect(screen.getByText(/已被.*取代/)).toBeInTheDocument();
  });

  it("omits the retry chain for original experiments", () => {
    renderCard(base);
    expect(screen.queryByText(/重跑链/)).not.toBeInTheDocument();
  });
});
