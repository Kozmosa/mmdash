export type GanttTimelineInput = {
  id: string;
  kind: string;
  title: string;
  status: string;
  start_at?: string | null;
  target_at?: string | null;
};

export type GanttTimelineTick = {
  offset: number;
  label: string;
};

export type PositionedGanttItem = GanttTimelineInput & {
  startAt: number;
  targetAt: number;
  offset: number;
  width: number;
};

export type GanttTimeline = {
  startAt: number | null;
  targetAt: number | null;
  rangeMs: number;
  items: PositionedGanttItem[];
  unscheduled: GanttTimelineInput[];
  ticks: GanttTimelineTick[];
};

const DAY_MS = 24 * 60 * 60 * 1_000;
const TICK_OFFSETS = [0, 0.25, 0.5, 0.75, 1];

/**
 * Parse the date forms accepted by the API without falling back to the
 * browser/server local timezone. Date-only values are deliberately treated as
 * UTC midnight; date-times must carry an explicit Z or numeric offset.
 */
export function parseTimelineDate(
  value: string | null | undefined,
): number | null {
  if (!value || typeof value !== "string") return null;
  const trimmed = value.trim();
  if (!trimmed) return null;

  const dateOnly = /^\d{4}-\d{2}-\d{2}$/.test(trimmed);
  const hasTimezone = /(?:Z|[+-]\d{2}:?\d{2})$/i.test(trimmed);
  if (!dateOnly && !hasTimezone) return null;

  const timestamp = Date.parse(dateOnly ? `${trimmed}T00:00:00.000Z` : trimmed);
  return Number.isFinite(timestamp) ? timestamp : null;
}

export function formatTimelineDate(timestamp: number): string {
  return new Date(timestamp).toISOString().slice(0, 10);
}

/**
 * Build one shared, UTC-based timeline for milestones and tasks. An item with
 * only one usable endpoint is represented as a point at that date. Reversed
 * endpoints are normalized so stale data cannot create negative CSS widths.
 */
export function calculateGanttTimeline(
  items: GanttTimelineInput[],
): GanttTimeline {
  const scheduled: Array<{
    item: GanttTimelineInput;
    startAt: number;
    targetAt: number;
  }> = [];
  const unscheduled: GanttTimelineInput[] = [];

  for (const item of items) {
    const start = parseTimelineDate(item.start_at);
    const target = parseTimelineDate(item.target_at);
    if (start === null && target === null) {
      unscheduled.push(item);
      continue;
    }
    const first = start ?? target!;
    const last = target ?? start!;
    scheduled.push({
      item,
      startAt: Math.min(first, last),
      targetAt: Math.max(first, last),
    });
  }

  if (!scheduled.length) {
    return {
      startAt: null,
      targetAt: null,
      rangeMs: 0,
      items: [],
      unscheduled,
      ticks: [],
    };
  }

  const firstAt = Math.min(...scheduled.map(({ startAt }) => startAt));
  const lastAt = Math.max(...scheduled.map(({ targetAt }) => targetAt));
  const originalRange = lastAt - firstAt;
  // A point needs a domain to sit inside. Keep the point centered so it remains
  // visible and the axis still communicates its date.
  const rangeMs = originalRange > 0 ? originalRange : DAY_MS;
  const domainStart = originalRange > 0 ? firstAt : firstAt - DAY_MS / 2;
  const domainEnd = domainStart + rangeMs;

  const positioned = scheduled.map(({ item, startAt, targetAt }) => ({
    ...item,
    startAt,
    targetAt,
    offset: roundPercent(((startAt - domainStart) / rangeMs) * 100),
    width: roundPercent(((targetAt - startAt) / rangeMs) * 100),
  }));
  const ticks = TICK_OFFSETS.map((offset) => ({
    offset: roundPercent(offset * 100),
    label: formatTimelineDate(domainStart + rangeMs * offset),
  }));

  return {
    startAt: domainStart,
    targetAt: domainEnd,
    rangeMs,
    items: positioned,
    unscheduled,
    ticks,
  };
}

function roundPercent(value: number): number {
  return Math.round(Math.min(100, Math.max(0, value)) * 100) / 100;
}
