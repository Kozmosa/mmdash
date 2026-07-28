import { describe, expect, it } from "vitest";

import { resolveCliPaths } from "../src/config/paths.js";

describe("CLI configuration paths", () => {
  it("uses APPDATA and LOCALAPPDATA on Windows", () => {
    const paths = resolveCliPaths({
      environment: {
        APPDATA: "C:\\Users\\test\\AppData\\Roaming",
        LOCALAPPDATA: "C:\\Users\\test\\AppData\\Local",
      },
      homeDirectory: "C:\\Users\\test",
      platform: "win32",
    });

    expect(paths.configFile).toBe(
      "C:\\Users\\test\\AppData\\Roaming\\mmdash\\config.json",
    );
    expect(paths.stateDirectory).toBe(
      "C:\\Users\\test\\AppData\\Local\\mmdash",
    );
  });

  it("uses XDG paths on Linux", () => {
    const paths = resolveCliPaths({
      environment: {
        XDG_CONFIG_HOME: "/tmp/config",
        XDG_STATE_HOME: "/tmp/state",
      },
      homeDirectory: "/home/test",
      platform: "linux",
    });

    expect(paths.configDirectory).toBe("/tmp/config/mmdash");
    expect(paths.stateDirectory).toBe("/tmp/state/mmdash");
  });

  it("uses Application Support on macOS", () => {
    const paths = resolveCliPaths({
      environment: {},
      homeDirectory: "/Users/test",
      platform: "darwin",
    });

    expect(paths.configDirectory).toBe(
      "/Users/test/Library/Application Support/mmdash",
    );
  });
});
