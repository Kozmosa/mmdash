import { describe, expect, it } from "vitest";

import { runCli } from "../src/cli.js";

describe("CLI shell", () => {
  it("prints version through the command and global alias", async () => {
    const command = captureIo();
    const commandExit = await runCli(["version"], {
      io: command.io,
      version: "0.1.0-test",
    });
    const alias = captureIo();
    const aliasExit = await runCli(["--version"], {
      io: alias.io,
      version: "0.1.0-test",
    });

    expect(commandExit).toBe(0);
    expect(aliasExit).toBe(0);
    expect(command.stdout).toEqual(["0.1.0-test"]);
    expect(alias.stdout).toEqual(["0.1.0-test"]);
  });

  it("renders searchable help and command-specific help", async () => {
    const all = captureIo();
    await runCli(["help"], { io: all.io, version: "0.1.0" });
    const doctor = captureIo();
    await runCli(["doctor", "--help"], {
      io: doctor.io,
      version: "0.1.0",
    });

    expect(all.stdout.join("\n")).toContain("doctor");
    expect(all.stdout.join("\n")).toContain("version");
    expect(doctor.stdout.join("\n")).toContain("mmdash doctor [--json]");
  });

  it("returns a stable error for unknown commands", async () => {
    const output = captureIo();
    const exitCode = await runCli(["missing"], {
      environment: { MMDASH_LOG_LEVEL: "silent" },
      io: output.io,
      version: "0.1.0",
    });

    expect(exitCode).toBe(2);
    expect(output.stderr).toEqual([
      "[UNKNOWN_COMMAND] Unknown command: missing",
      'Hint: Run "mmdash help" to list commands.',
    ]);
  });

  it("reports doctor results as text and JSON", async () => {
    const text = captureIo();
    const textExit = await runCli(["doctor"], {
      environment: { MMDASH_URL: "https://mmdash.com" },
      io: text.io,
      nodeVersion: "v20.9.0",
      platform: "linux",
      version: "0.1.0",
    });
    const json = captureIo();
    const jsonExit = await runCli(["doctor", "--json"], {
      environment: { MMDASH_URL: "https://mmdash.com" },
      io: json.io,
      nodeVersion: "v20.9.0",
      platform: "linux",
      version: "0.1.0",
    });

    expect(textExit).toBe(0);
    expect(text.stdout.join("\n")).toContain("OK  node");
    expect(jsonExit).toBe(0);
    expect(JSON.parse(json.stdout[0]!)).toMatchObject({ ok: true });
  });
});

function captureIo(): {
  io: {
    stderr(line: string): void;
    stdout(line: string): void;
  };
  stderr: string[];
  stdout: string[];
} {
  const stderr: string[] = [];
  const stdout: string[] = [];
  return {
    io: {
      stderr: (line) => stderr.push(line),
      stdout: (line) => stdout.push(line),
    },
    stderr,
    stdout,
  };
}
