import { execFileSync } from "node:child_process";

const output = execFileSync("gofmt", ["-l", "backend"], {
  encoding: "utf8",
}).trim();

if (output) {
  console.error(`Go files require gofmt:\n${output}`);
  process.exit(1);
}
