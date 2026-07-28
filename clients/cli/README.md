# mmdash CLI

Installable local command shell for mmdash:

```bash
pnpm --filter @mmdash/cli build
pnpm --filter @mmdash/cli dev -- help
pnpm --filter @mmdash/cli pack:check
```

After npm/pnpm installation, the package exposes the `mmdash` executable:

```bash
npm install --global @mmdash/cli
# or: pnpm add --global @mmdash/cli

mmdash --version
mmdash help
mmdash doctor
```

Stage 3.6 intentionally contains only the engineering and publication shell.
Login, project binding, secure credential storage, and the stdio-to-remote MCP
bridge are introduced in the later P0 CLI/MCP vertical.

See [the CLI development guide](../../docs/development/cli.md).
