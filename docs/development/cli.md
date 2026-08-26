# CLI development and release

`clients/cli` is the Stage 3 native Go 1.26 CLI. It is a separate Go module in
the repository `go.work`, produces one binary, and has no TypeScript runtime or
dependency on shared TypeScript packages.

## Commands

| Command                                                                                                | Purpose                                                                               |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------- |
| `mmdash login`                                                                                         | Start browser-approved device authorization and securely save the refreshable session |
| `mmdash logout`                                                                                        | Revoke the Core session and delete the local system credential                        |
| `mmdash whoami`                                                                                        | Resolve the delegated Core identity                                                   |
| `mmdash project list`                                                                                  | List active projects visible to the identity                                          |
| `mmdash project use <project_id>`                                                                      | Validate and explicitly select the local current Project                              |
| `mmdash project current`                                                                               | Read the selected Project from Core                                                   |
| `mmdash model list`                                                                                    | List Model questions and Notion synchronization state                                 |
| `mmdash model show <question_id>`                                                                      | Show one Model question and its Snapshot timeline                                     |
| `mmdash model sync [question_id]`                                                                      | Synchronize all models or one question and reset the countdown                        |
| `mmdash experiment list`                                                                               | List frozen experiments in the current project                                        |
| `mmdash experiment create <box\|self> <name> <full_commit_sha> <entrypoint> [auto\|e2b\|local-docker]` | Create a frozen Box-managed or self-run experiment                                    |
| `mmdash experiment run <experiment_id>`                                                                | Queue one frozen experiment                                                           |
| `mmdash experiment status <experiment_id>`                                                             | Read one experiment lifecycle state                                                   |
| `mmdash config set-domain [host]`                                                                      | Set the unified hosted domain or a loopback development host                          |
| `mmdash mcp`                                                                                           | Run the stdio-to-remote-Streamable-HTTP MCP bridge                                    |
| `mmdash doctor`                                                                                        | Check configuration, identity, Project selection, and Gateway health                  |

Use `--json` for machine-readable command output. Stable failures use a code,
safe message, exit status, optional request ID, and retryability. Usage errors
exit `2`, authentication errors `3`, network errors `4`, and local
configuration errors `5`.

## Device login and credentials

`login` calls `auth.device.authorize`, opens the returned browser URL, and polls
`auth.device.token` at the server-provided interval. The authenticated browser
approves or denies the displayed short code through Web BFF. The device code is
single-use and expires after ten minutes by default.

Core returns a short-lived JWT plus a rotating refresh token. Only hashes are
stored by Core. Refresh is compare-and-swap rotation: the old access and refresh
tokens stop working as soon as the new pair is committed.

The CLI stores the pair in Windows Credential Manager, macOS Keychain, or the
Linux Secret Service. There is no plaintext credential fallback.
`MMDASH_CONFIG_DIR` changes only the non-secret configuration location. Linux
headless environments must provide an unlocked Secret Service.

## Configuration

`config.json` is versioned and written with user-only permissions. It stores
only the unified server URL, Core URL, MCP URL, and explicit current Project.

| Variable            | Default                   | Purpose                                               |
| ------------------- | ------------------------- | ----------------------------------------------------- |
| `MMDASH_URL`        | `https://mmdash.moe`      | Unified public origin                                 |
| `MMDASH_CORE_URL`   | `MMDASH_URL`              | Core origin for direct development or unified routing |
| `MMDASH_MCP_URL`    | `${MMDASH_URL}/mcp`       | Remote Streamable HTTP endpoint                       |
| `MMDASH_CONFIG_DIR` | platform config directory | Test or portable non-secret config override           |

Plain HTTP is rejected except for loopback development.
`mmdash config set-domain` updates the unified server, Core, and MCP endpoints
together. With no argument it restores the hosted default; `localhost` and
other loopback hosts automatically use HTTP for local testing. Environment
variables continue to take precedence over the saved file.

## MCP bridge

The bridge forwards MCP JSON-RPC between stdin/stdout and the Gateway without a
compiled list of remote tools. It therefore forwards future Gateway capability
discovery without a CLI release. It supports JSON and SSE HTTP responses,
Gateway logical session correlation, one refresh-and-retry after HTTP 401, and
bounded retries only for discovery/read protocol methods. Tool calls are never
blindly replayed.

For every tool except `project.list`, the bridge injects the explicitly selected
`project_id` when absent. It rejects missing selection and rejects a conflicting
Project supplied by the local Agent. This prevents implicit or guessed write
targets when later stages add mutating tools.

`mmdash mcp` reserves stdout exclusively for MCP frames. Diagnostics, browser
instructions, and safe errors go to stderr; tokens never appear in either.

## Extending the CLI

Features register commands and doctor checks through small compile-time Go
interfaces. The CLI currently registers the Core/auth/MCP, Project, Model, and
Experiment features. Later Artifact, Progress, and Article stages own their
human commands. Go runtime plugins are not supported.

## Build, test, and release

```powershell
go test ./clients/cli/...
go build ./clients/cli/cmd/mmdash
```

Tests cover platform paths, refresh rotation, Project injection,
project-ambiguity refusal, retry boundaries, and protocol-only stdout. CI tag
`cli-v*` cross-compiles release archives for Windows, macOS, and Linux on amd64
and arm64, then publishes `SHA256SUMS`. Installing a newer release archive
upgrades the single binary without migrating secret storage or Project
selection.

For installation, download the archive for the target OS and architecture,
verify it against the release's `SHA256SUMS`, extract it, and copy the binary to
a directory on `PATH`. Upgrade by replacing that binary with a verified newer
release. Confirm both operations with `mmdash --version` and `mmdash doctor`;
the config schema handles non-secret configuration compatibility while system
credential storage remains outside the archive.

Hermes is outside this CLI. In the later Agent stage it uses its own
Project-scoped Agent Token to connect directly to the Gateway.
