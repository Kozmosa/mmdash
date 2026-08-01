import { execFileSync } from "node:child_process";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";

const files = [
  ...(await goFiles("backend")),
  ...(await goFiles("clients/cli")),
].sort();
const unformatted = [];

for (const file of files) {
  const contents = (await readFile(file, "utf8")).replaceAll("\r\n", "\n");
  const formatted = execFileSync("gofmt", {
    encoding: "utf8",
    input: contents,
  });
  if (formatted !== contents) {
    unformatted.push(file);
  }
}

if (unformatted.length > 0) {
  console.error(`Go files require gofmt:\n${unformatted.join("\n")}`);
  process.exit(1);
}

async function goFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await goFiles(entryPath)));
    } else if (entry.isFile() && entry.name.endsWith(".go")) {
      files.push(entryPath);
    }
  }
  return files.sort();
}
