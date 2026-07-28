# CLI development and packaging

`clients/cli` is an installable Node.js command-line package, separate from
long-running applications. Stage 3.6 supplies the command and release
foundation only; it does not connect to Core, storage, Git, or the MCP Gateway.

## Commands

| Command                               | Purpose                                                         | Exit status                       |
| ------------------------------------- | --------------------------------------------------------------- | --------------------------------- |
| `mmdash --version` / `mmdash version` | Print the package version                                       | `0`                               |
| `mmdash help [command]`               | Show global or command-specific help                            | `0`, or `2` for an unknown target |
| `mmdash doctor [--json]`              | Check Node, platform, config path, and server URL configuration | `0` when all checks pass          |

Unknown commands return exit status `2` with the stable code
`UNKNOWN_COMMAND`. Unexpected errors use a safe `INTERNAL_ERROR`; stack traces
and secrets are not written to normal output.

Set `MMDASH_LOG_LEVEL` to `debug`, `info`, `warn`, `error`, or `silent`.
Structured logs go to stderr so stdout remains safe for command results and
shell scripts.

## Configuration paths

The resolver is pure and does not create files during `help`, `version`, or
`doctor`.

| Platform   | Config directory                                | State directory                                     |
| ---------- | ----------------------------------------------- | --------------------------------------------------- |
| Windows    | `%APPDATA%\mmdash`                              | `%LOCALAPPDATA%\mmdash`                             |
| macOS      | `~/Library/Application Support/mmdash`          | Same foundation directory                           |
| Linux/Unix | `$XDG_CONFIG_HOME/mmdash` or `~/.config/mmdash` | `$XDG_STATE_HOME/mmdash` or `~/.local/state/mmdash` |

Reserved files are `config.json` and `credentials.json`. The later P0 auth
stage must replace the credentials placeholder with the operating system's
secure credential store; plaintext token persistence is not implemented here.

`MMDASH_URL` defaults to `https://mmdash.com`. Doctor rejects non-HTTPS remote
URLs while allowing HTTP for localhost development.

## Build and publication

```bash
pnpm --filter @mmdash/cli test
pnpm --filter @mmdash/cli build
pnpm --filter @mmdash/cli pack:check
```

TypeScript emits portable ESM for Node.js 20.9 or newer. `package.json`
declares the `mmdash` bin entry and limits the npm tarball to `dist`, the
package manifest, and README. `pack:check` builds and runs `pnpm pack --dry-run`
without publishing.

Once a release is published, npm and pnpm install the same package:

```bash
npm install --global @mmdash/cli
pnpm add --global @mmdash/cli
```

Local development:

```bash
pnpm --filter @mmdash/cli dev -- doctor --json
```

For a new command, implement `CliCommand`, register it in
`createDefaultRegistry`, and test stdout, stderr, and exit status separately.
