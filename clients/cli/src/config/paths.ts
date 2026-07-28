import os from "node:os";
import path from "node:path";

export type CliPaths = {
  configDirectory: string;
  configFile: string;
  credentialsFile: string;
  stateDirectory: string;
};

export type PathEnvironment = {
  APPDATA?: string;
  HOME?: string;
  LOCALAPPDATA?: string;
  XDG_CONFIG_HOME?: string;
  XDG_STATE_HOME?: string;
};

export function resolveCliPaths(
  options: {
    environment?: PathEnvironment;
    homeDirectory?: string;
    platform?: NodeJS.Platform;
  } = {},
): CliPaths {
  const environment = options.environment ?? process.env;
  const platform = options.platform ?? process.platform;
  const homeDirectory = options.homeDirectory ?? os.homedir();
  const paths = platform === "win32" ? path.win32 : path.posix;

  const configDirectory =
    platform === "win32"
      ? paths.join(
          environment.APPDATA ??
            paths.join(homeDirectory, "AppData", "Roaming"),
          "mmdash",
        )
      : platform === "darwin"
        ? paths.join(homeDirectory, "Library", "Application Support", "mmdash")
        : paths.join(
            environment.XDG_CONFIG_HOME ?? paths.join(homeDirectory, ".config"),
            "mmdash",
          );
  const stateDirectory =
    platform === "win32"
      ? paths.join(
          environment.LOCALAPPDATA ??
            paths.join(homeDirectory, "AppData", "Local"),
          "mmdash",
        )
      : platform === "darwin"
        ? paths.join(homeDirectory, "Library", "Application Support", "mmdash")
        : paths.join(
            environment.XDG_STATE_HOME ??
              paths.join(homeDirectory, ".local", "state"),
            "mmdash",
          );

  return {
    configDirectory,
    configFile: paths.join(configDirectory, "config.json"),
    credentialsFile: paths.join(configDirectory, "credentials.json"),
    stateDirectory,
  };
}
