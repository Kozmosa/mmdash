export function normalizeAlignment(value: unknown) {
  return value === "left" || value === "right" ? value : "center";
}

export function normalizeImageWidth(value: unknown): number {
  const width = Number(value);
  if (!Number.isFinite(width)) return 100;
  return Math.min(100, Math.max(20, Math.round(width)));
}

export function isTransientImageURL(value: string): boolean {
  try {
    const url = new URL(value, "http://mmdash.local");
    const parameters = new Set(
      [...url.searchParams.keys()].map((key) => key.toLowerCase()),
    );
    return [
      "x-amz-algorithm",
      "x-amz-credential",
      "x-amz-signature",
      "x-amz-security-token",
      "signature",
      "token",
    ].some((key) => parameters.has(key));
  } catch {
    return true;
  }
}

export function availableThumbnailURL(
  previews: ReadonlyArray<{
    preview_type: string;
    status: string;
    transfer: { url: string } | null;
  }>,
): string | undefined {
  return previews.find(
    (item) =>
      item.preview_type === "thumbnail" &&
      item.status === "available" &&
      item.transfer?.url,
  )?.transfer?.url;
}

export function imageAlignmentStyle(value: unknown) {
  const alignment = normalizeAlignment(value);
  return {
    marginLeft:
      alignment === "right" ? "auto" : alignment === "center" ? "auto" : "0",
    marginRight:
      alignment === "left" ? "auto" : alignment === "center" ? "auto" : "0",
  };
}

export function isImageArtifact(payload: {
  filename?: string;
  mimeType?: string;
  title?: string;
}): boolean {
  if (payload.mimeType?.startsWith("image/")) return true;
  const name = (payload.filename || payload.title || "").toLowerCase();
  return /\.(png|jpe?g|gif|webp|svg|bmp|tiff?|ico)$/i.test(name);
}
