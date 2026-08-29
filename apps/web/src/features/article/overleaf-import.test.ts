// @vitest-environment node

import { strFromU8, unzipSync } from "fflate";
import { describe, expect, it } from "vitest";

import { convertOverleafBytes, inspectOverleafBytes } from "./overleaf-import";

const valid = bytes(
  "UEsDBBQAAAAIACJmDV1yvkr5OAAAAEYAAAAIAAAAbWFpbi50ZXiLiUnJTy7NTc0rSc5JLC6uTiwqyUzOSa3liolJSk3PzKuGSddyBSfmFuSkKiTlp1QCZVPzUhByAFBLAwQUAAAACAAiZg1dBIPa8gkAAAAHAAAAEAAAAHN0eWxlL2N1c3RvbS5zdHlTVSguqcxJBQBQSwECFAAUAAAACAAiZg1dcr5K+TgAAABGAAAACAAAAAAAAAAAAAAAAAAAAAAAbWFpbi50ZXhQSwECFAAUAAAACAAiZg1dBIPa8gkAAAAHAAAAEAAAAAAAAAAAAAAAAABeAAAAc3R5bGUvY3VzdG9tLnN0eVBLBQYAAAAAAgACAHQAAACVAAAAAAA=",
);
const traversal = bytes(
  "UEsDBBQAAAAIACJmDV31H35WGgAAACEAAAALAAAALi4vbWFpbi50ZXiLiUlKTc/Mq07JTy7NTc0rqa2IiUnNS0HwAVBLAQIUABQAAAAIACJmDV31H35WGgAAACEAAAALAAAAAAAAAAAAAAAAAAAAAAAuLi9tYWluLnRleFBLBQYAAAAAAQABADkAAABDAAAAAAA=",
);
const script = bytes(
  "UEsDBBQAAAAIACJmDV31H35WGgAAACEAAAAIAAAAbWFpbi50ZXiLiUlKTc/Mq07JTy7NTc0rqa2IiUnNS0HwAVBLAwQUAAAACAAiZg1dDoQJlAYAAAAEAAAACAAAAGJ1aWxkLnNoS63ILAEAUEsBAhQAFAAAAAgAImYNXfUfflYaAAAAIQAAAAgAAAAAAAAAAAAAAAAAAAAAAG1haW4udGV4UEsBAhQAFAAAAAgAImYNXQ6ECZQGAAAABAAAAAgAAAAAAAAAAAAAAAAAQAAAAGJ1aWxkLnNoUEsFBgAAAAACAAIAbAAAAGwAAAAAAA==",
);

describe("Overleaf template import", () => {
  it("discovers a main TeX file and converts its body to generated targets", async () => {
    const source = valid;
    expect(strFromU8(unzipSync(source)["main.tex"]!)).toContain(
      "\\begin{document}",
    );
    expect(inspectOverleafBytes("overleaf.zip", source)).toMatchObject({
      candidates: ["main.tex"],
      fileCount: 2,
    });
    const converted = convertOverleafBytes("overleaf.zip", source, "main.tex", {
      name: "Contest",
      version: "1.0.0",
      engine: "xelatex",
      bibliography_tool: "biber",
    });
    const entries = unzipSync(converted.bytes);
    expect(strFromU8(entries["main.tex"]!)).toContain(
      "\\input{.mmdash/generated-content}",
    );
    expect(strFromU8(entries["main.tex"]!)).not.toContain("Sample body");
    expect(
      JSON.parse(strFromU8(entries["mmdash-template.json"]!)),
    ).toMatchObject({
      entrypoint: "main.tex",
      engine: "xelatex",
      bibliography_tool: "biber",
    });
  });

  it("rejects path traversal and executable template content", async () => {
    expect(() => inspectOverleafBytes("overleaf.zip", traversal)).toThrow(
      "不安全",
    );
    expect(() => inspectOverleafBytes("overleaf.zip", script)).toThrow(
      "不安全",
    );
  });
});

function bytes(value: string) {
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}
