import { describe, expect, it } from "vitest";

import { hashFile, IncrementalSha256 } from "@/features/artifact/sha256";

describe("incremental Artifact SHA-256", () => {
  it("matches standard vectors across arbitrary update boundaries", () => {
    expect(new IncrementalSha256().digestHex()).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    );

    const hash = new IncrementalSha256();
    hash.update(new TextEncoder().encode("a"));
    hash.update(new TextEncoder().encode("b"));
    hash.update(new TextEncoder().encode("c"));
    expect(hash.digestHex()).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
  });

  it("hashes a Blob slice by slice and reports byte progress", async () => {
    const progress: number[] = [];
    const digest = await hashFile(new Blob(["abc"]), {
      chunkBytes: 1,
      onProgress: (completed) => progress.push(completed),
    });

    expect(digest).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    expect(progress).toEqual([1, 2, 3, 3]);
  });
});
