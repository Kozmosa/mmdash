import { describe, expect, it } from "vitest";

import {
  calculateGanttTimeline,
  parseTimelineDate,
} from "@/features/progress/gantt-timeline";

describe("calculateGanttTimeline", () => {
  it("uses one shared range to calculate offsets and widths", () => {
    const timeline = calculateGanttTimeline([
      {
        id: "milestone-1",
        kind: "milestone",
        status: "planned",
        title: "模型冻结",
        start_at: "2030-01-01T00:00:00Z",
        target_at: "2030-01-11T00:00:00Z",
      },
      {
        id: "task-1",
        kind: "task",
        status: "in_progress",
        title: "整理参数",
        start_at: "2030-01-06T00:00:00Z",
        target_at: "2030-01-21T00:00:00Z",
      },
    ]);

    expect(
      timeline.items.map(({ offset, width }) => ({ offset, width })),
    ).toEqual([
      { offset: 0, width: 50 },
      { offset: 25, width: 75 },
    ]);
    expect(timeline.ticks.map(({ offset }) => offset)).toEqual([
      0, 25, 50, 75, 100,
    ]);
  });

  it("gives a single zero-span item a centered one-day domain", () => {
    const timestamp = "2030-02-01T12:00:00Z";
    const timeline = calculateGanttTimeline([
      {
        id: "task-1",
        kind: "task",
        status: "todo",
        title: "单日任务",
        start_at: timestamp,
        target_at: timestamp,
      },
    ]);

    expect(timeline.rangeMs).toBe(24 * 60 * 60 * 1_000);
    expect(timeline.items[0]).toMatchObject({ offset: 50, width: 0 });
    expect(timeline.items[0].startAt).toBe(Date.parse(timestamp));
  });

  it("normalizes reversed dates and keeps one-sided dates safe", () => {
    const timeline = calculateGanttTimeline([
      {
        id: "reverse",
        kind: "task",
        status: "blocked",
        title: "反向任务",
        start_at: "2030-03-10T00:00:00Z",
        target_at: "2030-03-01T00:00:00Z",
      },
      {
        id: "point",
        kind: "milestone",
        status: "planned",
        title: "只有目标日期",
        target_at: "2030-03-05T00:00:00Z",
      },
      { id: "unknown", kind: "task", status: "todo", title: "没有日期" },
    ]);

    expect(timeline.items[0].startAt).toBe(Date.parse("2030-03-01T00:00:00Z"));
    expect(timeline.items[0].targetAt).toBe(Date.parse("2030-03-10T00:00:00Z"));
    expect(timeline.items[0].width).toBeGreaterThan(0);
    expect(timeline.items[1].startAt).toBe(timeline.items[1].targetAt);
    expect(timeline.unscheduled.map(({ id }) => id)).toEqual(["unknown"]);
  });

  it("calculates ISO offsets by instant and never interprets timezone-less datetimes locally", () => {
    expect(parseTimelineDate("2030-01-01T00:00:00")).toBeNull();
    expect(parseTimelineDate("2030-01-01")).toBe(
      Date.parse("2030-01-01T00:00:00.000Z"),
    );

    const timeline = calculateGanttTimeline([
      {
        id: "tz-1",
        kind: "task",
        status: "todo",
        title: "东八区",
        start_at: "2030-01-01T00:00:00+08:00",
        target_at: "2030-01-02T00:00:00+08:00",
      },
      {
        id: "tz-2",
        kind: "task",
        status: "todo",
        title: "西五区",
        start_at: "2029-12-31T19:00:00-05:00",
        target_at: "2030-01-03T00:00:00-05:00",
      },
    ]);

    expect(timeline.startAt).toBe(Date.parse("2030-01-01T00:00:00+08:00"));
    expect(timeline.targetAt).toBe(Date.parse("2030-01-03T00:00:00-05:00"));
    expect(timeline.items[0].offset).toBe(0);
    expect(timeline.items[1].offset).toBeGreaterThan(0);
  });

  it("returns an empty layout when no item has a usable date", () => {
    const timeline = calculateGanttTimeline([
      {
        id: "unknown",
        kind: "milestone",
        status: "planned",
        title: "未安排",
        start_at: "not-a-date",
      },
    ]);

    expect(timeline.items).toEqual([]);
    expect(timeline.ticks).toEqual([]);
    expect(timeline.unscheduled).toHaveLength(1);
  });
});
