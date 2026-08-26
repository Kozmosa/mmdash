# mmdash CLI

Native Go 1.26 CLI for users and local Coding Agents.

```bash
go build ./clients/cli/cmd/mmdash
go test ./clients/cli/...

mmdash login
mmdash whoami
mmdash project list
mmdash project use <project_id>
mmdash project current
mmdash model list
mmdash experiment list
mmdash experiment run <experiment_id>
mmdash config set-domain [domain]
mmdash mcp
mmdash doctor
mmdash logout
```

`mmdash login` uses browser-approved device authorization. Access and refresh
tokens are stored only in Windows Credential Manager, macOS Keychain, or the
Linux Secret Service. `config.json` contains non-secret endpoints and the
explicit current Project selection.

`mmdash mcp` is a transparent stdio-to-Streamable-HTTP bridge. Its stdout is
reserved for MCP JSON-RPC; diagnostics and safe errors use stderr. Hermes and
other hosted Agent runtimes connect directly to the Gateway in the later Agent
stage and never through this CLI.

See [the CLI development guide](../../docs/development/cli.md).

## Install and upgrade

Release tags named `cli-v*` publish one archive for each supported Windows,
macOS, and Linux amd64/arm64 target, together with `SHA256SUMS`. Download the
archive matching your operating system and CPU, verify its SHA-256 digest,
extract it, and place `mmdash` (or `mmdash.exe`) in a directory on `PATH`.

To upgrade, verify and extract a newer release, then replace the single binary.
The versioned non-secret config and the operating system credential entry stay
in their platform locations, so an upgrade does not require another login or
Project selection. Run `mmdash --version` and `mmdash doctor` after replacement.
