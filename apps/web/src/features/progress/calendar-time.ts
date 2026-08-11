export const MINUTES_PER_DAY = 24 * 60;
export const SNAP_MINUTES = 15;
export const PERIODS = [
  { key: "morning", label: "上午", start: 8 * 60, end: 12 * 60 },
  { key: "afternoon", label: "下午", start: 12 * 60, end: 18 * 60 },
  { key: "evening", label: "夜晚", start: 18 * 60, end: 23 * 60 },
  { key: "night", label: "半夜", start: 23 * 60, end: 32 * 60 },
] as const;

export function startOfLocalDay(value: Date): Date {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate());
}

export function addLocalDays(value: Date, amount: number): Date {
  const result = new Date(value);
  result.setDate(result.getDate() + amount);
  return result;
}

export function localDayKey(value: Date): string {
  const year = value.getFullYear();
  const month = String(value.getMonth() + 1).padStart(2, "0");
  const day = String(value.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function snapDate(value: Date): Date {
  const snapped = new Date(value);
  snapped.setSeconds(0, 0);
  snapped.setMinutes(Math.round(snapped.getMinutes() / SNAP_MINUTES) * SNAP_MINUTES);
  return snapped;
}

export function minutesFromDayStart(value: Date): number {
  return value.getHours() * 60 + value.getMinutes();
}

export function dateAtMinutes(day: Date, minutes: number): Date {
  const value = startOfLocalDay(day);
  value.setMinutes(minutes);
  return value;
}

export function formatShortDay(value: Date): string {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    weekday: "short",
  }).format(value);
}

export function formatTime(value: Date): string {
  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(value);
}

export function periodForDate(value: Date) {
  const minutes = minutesFromDayStart(value);
  if (minutes < 8 * 60) return PERIODS[3];
  return PERIODS.find((period) => minutes >= period.start && minutes < Math.min(period.end, MINUTES_PER_DAY)) ?? PERIODS[3];
}

export type LaneItem = { id: string; start: number; end: number };

export function assignOverlapLanes(items: LaneItem[]): Map<string, { lane: number; lanes: number }> {
  const sorted = [...items].sort((left, right) => left.start - right.start || left.end - right.end);
  const result = new Map<string, { lane: number; lanes: number }>();
  let group: LaneItem[] = [];
  let groupEnd = -1;
  const flush = () => {
    if (!group.length) return;
    const laneEnds: number[] = [];
    const assignments = new Map<string, number>();
    for (const item of group) {
      let lane = laneEnds.findIndex((end) => end <= item.start);
      if (lane === -1) lane = laneEnds.length;
      laneEnds[lane] = item.end;
      assignments.set(item.id, lane);
    }
    for (const item of group) result.set(item.id, { lane: assignments.get(item.id) ?? 0, lanes: laneEnds.length });
    group = [];
    groupEnd = -1;
  };
  for (const item of sorted) {
    if (group.length && item.start >= groupEnd) flush();
    group.push(item);
    groupEnd = Math.max(groupEnd, item.end);
  }
  flush();
  return result;
}
