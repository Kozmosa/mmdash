import { describe, expect, it } from "vitest";

import {
  assignOverlapLanes,
  snapDate,
} from "@/features/progress/calendar-time";

describe("Progress calendar time rules", () => {
  it("snaps task interactions to 15-minute boundaries", () => {
    expect(snapDate(new Date(2026, 7, 11, 9, 7)).getMinutes()).toBe(0);
    expect(snapDate(new Date(2026, 7, 11, 9, 8)).getMinutes()).toBe(15);
    expect(snapDate(new Date(2026, 7, 11, 9, 53)).getHours()).toBe(10);
  });

  it("assigns overlapping tasks to separate lanes and reuses a free lane", () => {
    const lanes = assignOverlapLanes([
      { end: 120, id: "a", start: 60 },
      { end: 100, id: "b", start: 80 },
      { end: 150, id: "c", start: 120 },
    ]);
    expect(lanes.get("a")).toEqual({ lane: 0, lanes: 2 });
    expect(lanes.get("b")).toEqual({ lane: 1, lanes: 2 });
    expect(lanes.get("c")).toEqual({ lane: 0, lanes: 1 });
  });
});
