# Sandbox capability

The Sandbox capability maps a reviewed fixed entrypoint to an argv vector,
executes a `SandboxRuntime`, and packages only manifest-declared regular files.
It rejects shell syntax, absolute/traversal paths, symlinks, duplicate manifest
entries, zip-slip, and hash/size mismatches. It never edits source, parameters,
Git state, or long-lived workspace data.
