// @vitest-environment node

import { strFromU8, strToU8, unzipSync, zipSync } from "fflate";
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

  it("infers XeLaTeX with biber for a ctex + biblatex template", () => {
    const source = templateZip({
      "main.tex": latexDocument(
        "\\documentclass{ctexart}\n\\usepackage[backend=biber]{biblatex}\n\\addbibresource{refs.bib}",
        "\\section{标题}\n正文\n\\printbibliography",
      ),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({
      engine: "xelatex",
      bibliography_tool: "biber",
    });
  });

  it("infers LuaLaTeX when the template loads luatexja", () => {
    const source = templateZip({
      "main.tex": latexDocument(
        "\\documentclass{article}\n\\usepackage{luatexja}",
        "Body",
      ),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "lualatex", bibliography_tool: "none" });
  });

  it("infers pdfLaTeX with bibtex for a plain article with a bst style", () => {
    const source = templateZip({
      "main.tex": latexDocument(
        "\\documentclass[12pt]{article}\n\\usepackage{graphicx}",
        "Body\n\\bibliographystyle{plain}\n\\bibliography{refs}",
      ),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "pdflatex", bibliography_tool: "bibtex" });
  });

  it("prefers bibtex when biblatex declares backend=bibtex", () => {
    const source = templateZip({
      "main.tex": latexDocument(
        "\\documentclass{article}\n\\usepackage[backend=bibtex]{biblatex}",
        "Body",
      ),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "pdflatex", bibliography_tool: "bibtex" });
  });

  it("falls back to pdfLaTeX with no bibliography tool", () => {
    const source = templateZip({
      "main.tex": latexDocument("\\documentclass{article}", "Body"),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "pdflatex", bibliography_tool: "none" });
  });

  it("ignores commented-out packages when inferring", () => {
    const source = templateZip({
      "main.tex": latexDocument(
        "% \\usepackage{fontspec}\n\\documentclass{article}",
        "Body",
      ),
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "pdflatex", bibliography_tool: "none" });
  });

  it("scans cls and sty files for engine hints", () => {
    const source = templateZip({
      "main.tex": latexDocument("\\documentclass{myschool}", "Body"),
      "myschool.cls": "\\RequirePackage{xeCJK}\n",
      "extra.sty": "\\RequirePackage{tcolorbox}\n",
    });
    expect(
      inspectOverleafBytes("overleaf.zip", source).profiles["main.tex"],
    ).toEqual({ engine: "xelatex", bibliography_tool: "none" });
  });
});

function templateZip(files: Record<string, string>): Uint8Array {
  return zipSync(
    Object.fromEntries(
      Object.entries(files).map(([path, contents]) => [
        path,
        strToU8(contents),
      ]),
    ),
  );
}

function latexDocument(preamble: string, body: string): string {
  return `${preamble}\n\\begin{document}\n${body}\n\\end{document}\n`;
}

function bytes(value: string) {
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}
