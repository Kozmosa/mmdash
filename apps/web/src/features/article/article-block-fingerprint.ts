const goJSONEscapes: Record<string, string> = {
  "&": "\\u0026",
  "<": "\\u003c",
  ">": "\\u003e",
  "\u2028": "\\u2028",
  "\u2029": "\\u2029",
};

export async function articleBlockContentFingerprint(
  node: Record<string, unknown>,
): Promise<string> {
  const clean = canonicalValue(node) as Record<string, unknown>;
  sanitizeNode(clean);
  const attrs = canonicalValue(clean.attrs) as Record<string, unknown>;
  delete attrs.tag;
  delete attrs.provenance;
  clean.attrs = attrs;
  const encoded = new TextEncoder().encode(
    JSON.stringify(clean).replace(/[<>&\u2028\u2029]/g, (value) => goJSONEscapes[value]),
  );
  const digest = await crypto.subtle.digest("SHA-256", encoded);
  return [...new Uint8Array(digest)]
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}

function canonicalValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalValue);
  if (typeof value !== "object" || value === null) return value;
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
      .map(([key, item]) => [key, canonicalValue(item)]),
  );
}

function sanitizeNode(node: Record<string, unknown>): void {
  const attrs = node.attrs;
  if (attrs && typeof attrs === "object" && !Array.isArray(attrs)) {
    const cleanAttrs = attrs as Record<string, unknown>;
    if (node.type === "artifactReference") {
      for (const key of ["previewUrl", "preview_url", "expiresAt", "expires_at"])
        delete cleanAttrs[key];
    }
    if (node.type === "articleImage" && hasSignedImageSource(cleanAttrs.src))
      delete cleanAttrs.src;
  }
  if (Array.isArray(node.content)) {
    for (const child of node.content) {
      if (child && typeof child === "object" && !Array.isArray(child))
        sanitizeNode(child as Record<string, unknown>);
    }
  }
}

function hasSignedImageSource(value: unknown): boolean {
  if (typeof value !== "string") return false;
  try {
    const url = new URL(value);
    return [...url.searchParams.keys()].some((key) =>
      [
        "x-amz-algorithm",
        "x-amz-credential",
        "x-amz-signature",
        "x-amz-security-token",
        "signature",
        "token",
      ].includes(key.toLowerCase()),
    );
  } catch {
    return true;
  }
}
