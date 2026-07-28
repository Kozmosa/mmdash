import { readFileSync } from "node:fs";

type PackageMetadata = {
  version: string;
};

export function readCliVersion(): string {
  const packageUrl = new URL("../package.json", import.meta.url);
  const metadata = JSON.parse(
    readFileSync(packageUrl, "utf8"),
  ) as PackageMetadata;
  return metadata.version;
}
