# ADR 0001: Managed Git CLI runtime for Stage 1 Repo

Status: accepted for Stage 1

## Context

Stage 1 needs one authoritative Git boundary that can fetch provider
repositories, keep three logical workspaces available concurrently, read
immutable Git objects, create detached checkouts, and support future Article
and Experiment writes. Browser, BFF, MCP, CLI, and Worker processes must not
access Git state or server repository paths directly.

The product names `code`, `article`, and `result` are logical workspace names,
not required remote branch names. Existing branches such as `main` may back any
workspace. A branch name moves over time, while a full commit SHA is an
immutable reference.

## Decision

- Go Core is the only Git entry point.
- Each connected repository uses a Core-managed bare repository plus linked,
  long-lived worktrees for `code`, `article`, and `result`.
- The three workspaces must map to three distinct, existing remote branches.
  Stage 1 never silently creates, renames, deletes, force-pushes, or substitutes
  a remote branch.
- Git operations use fixed `exec.CommandContext` templates and the system Git
  CLI. Shell command strings and caller-provided arbitrary arguments are not
  allowed.
- Git operations are serialized per repository, globally bounded, and
  coordinated across Core instances with PostgreSQL leases/advisory locks.
- Long-running network and Git subprocess work occurs outside database
  transactions. State, Audit metadata, and Outbox events are persisted in
  short transactions after Git succeeds.
- Browsing resolves a workspace/branch to a full commit SHA once. Tree and
  content reads then use that SHA.
- Temporary consumers receive detached checkout leases. HTTP never returns a
  server filesystem path.
- GitHub HTTPS/PAT and allowlisted Local Git are the only Stage 1 providers.
  PATs are injected with AskPass and never placed in a URL, command argument,
  ordinary database column, log, event, or response.
- Core exposes controlled file changes, Commit, and ordinary Push for trusted
  internal module interfaces. Stage 1 Web remains read-only.
- Sync coordination runs in Go Core; no Python Repo Worker is added.

## Consequences

Core deployment needs Git, CA certificates, a persistent repository volume,
and bounded runtime configuration. Multi-Core deployments require shared
storage with compatible atomic filesystem operations. Git remains the
authority for branches, commits, trees, and blobs; PostgreSQL stores
configuration, status, leases, observations, idempotency, and projections.

Repo callers depend on stable service interfaces such as `ArticleWorkspace`
instead of database tables, Git commands, or filesystem paths. Later providers
can implement the provider boundary without changing the domain contract.

## Alternatives considered

- `go-git`: rejected for Stage 1 because the system needs mature linked
  worktree, credential, ref, and Git object behavior already provided by Git.
- Python Worker synchronization: rejected because Repo lifecycle and
  authoritative state belong to Core and no queued compute step is required.
- Directly operating on Local Git sources: rejected because source paths are
  administrator-owned inputs and must not become mutable application
  workspaces.
- Branch names as durable references: rejected because branches move; formal
  cross-module references must store full commit SHAs.
