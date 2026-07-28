#!/usr/bin/env node

import { runCli } from "./cli.js";
import { readCliVersion } from "./version.js";

process.exitCode = await runCli(process.argv.slice(2), {
  version: readCliVersion(),
});
