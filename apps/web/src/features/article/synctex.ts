export type SyncTexPoint = {
  file: string;
  line: number;
  page: number;
  y: number;
};

export function parseSyncTex(source: string, sourceFiles: string[]): SyncTexPoint[] {
  const inputs = new Map<number, string>();
  const points: SyncTexPoint[] = [];
  let page = 0;
  for (const rawLine of source.split(/\r?\n/u)) {
    const input = /^Input:(\d+):(.+)$/u.exec(rawLine);
    if (input) {
      const tag = Number(input[1]);
      const file = matchSourceFile(input[2]!, sourceFiles);
      if (file) inputs.set(tag, file);
      continue;
    }
    const pageStart = /^\{(\d+)$/u.exec(rawLine);
    if (pageStart) {
      page = Number(pageStart[1]);
      continue;
    }
    if (page <= 0) continue;
    // A SyncTeX box/point record starts with its input tag and source line,
    // followed by the horizontal and vertical positions in scaled points.
    const record = /^[[(vhxkg$]?\s*(\d+),(\d+):(?:-?\d+,)?(-?\d+),(-?\d+)/u.exec(rawLine);
    if (!record) continue;
    const file = inputs.get(Number(record[1]));
    if (!file) continue;
    points.push({ file, line: Number(record[2]), page, y: Number(record[4]) });
  }
  const unique = new Map<string, SyncTexPoint>();
  for (const point of points) unique.set(`${point.file}:${point.line}:${point.page}:${point.y}`, point);
  return [...unique.values()].sort((left, right) =>
    left.page - right.page || left.y - right.y || left.file.localeCompare(right.file) || left.line - right.line,
  );
}

export function forwardSyncPoint(
  points: SyncTexPoint[],
  file: string,
  line: number,
): SyncTexPoint | undefined {
  return points
    .filter((point) => point.file === file)
    .reduce<SyncTexPoint | undefined>((best, point) =>
      !best || Math.abs(point.line - line) < Math.abs(best.line - line) ? point : best, undefined);
}

export function reverseSyncPoint(
  points: SyncTexPoint[],
  page: number,
  verticalPercent: number,
): SyncTexPoint | undefined {
  const candidates = points.filter((point) => point.page === page);
  if (!candidates.length) return undefined;
  const min = Math.min(...candidates.map((point) => point.y));
  const max = Math.max(...candidates.map((point) => point.y));
  const target = min + (max - min) * Math.min(1, Math.max(0, verticalPercent / 100));
  return candidates.reduce((best, point) =>
    Math.abs(point.y - target) < Math.abs(best.y - target) ? point : best,
  );
}

function matchSourceFile(input: string, sourceFiles: string[]): string | undefined {
  const normalized = input.replaceAll("\\", "/");
  const exact = sourceFiles.find((file) => normalized === file || normalized.endsWith(`/${file}`));
  if (exact) return exact;
  const basename = normalized.split("/").at(-1);
  const matches = sourceFiles.filter((file) => file.split("/").at(-1) === basename);
  return matches.length === 1 ? matches[0] : undefined;
}
