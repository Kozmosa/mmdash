# mmdash Box

`mmdash-box` is the outbound Box Gateway. The same binary exposes the `mbox`
administrative commands used to install and operate it on Windows and Linux.

## First setup

Run these commands from a terminal in the directory containing the binary:

```text
mbox setup
mbox account login
mbox service init
mbox service status
```

`mbox setup` asks for the public mmdash Box Control address, Box name, Local
Docker image, and Runtime adapter settings. Both `http://` and `https://` are
valid; Core itself is never configured as a public endpoint. Use
`mbox setup --root PATH` to select a different persistent directory.

The default persistent directory is `%LocalAppData%\\MMDash Box` on Windows and
`$XDG_DATA_HOME/mmdash-box` (falling back to `~/.local/share/mmdash-box`) on
Linux. The directory contains `config.json`, `state.json`, `logs/`, task
spools, outputs, and transferred sources.

## Administration

```text
mbox gateway --root PATH
mbox account status
mbox account logout
mbox config show
mbox config set control-url=http://localhost:3001
mbox config set local-docker.enabled=true local-docker.image=mmdash/sandbox:latest
mbox config set e2b.enabled=true e2b.api-key=... e2b.api-url=https://api.example.test
mbox service start
mbox service stop
mbox service status
mbox service logs
mbox service remove
mbox uninstall --yes
```

`service init` registers Windows auto-start through the Service Control
Manager or a Linux systemd unit. `service remove` unregisters the service while
keeping Box configuration and data. `uninstall` stops and unregisters that
service before deleting the selected Box root; it does not modify project
repositories.

On Windows, run `service init`, `service start`, `service stop`, `service
status`, and `service remove` from an elevated PowerShell or Command Prompt.
After replacing `mbox.exe`, run `mbox service remove` and then `mbox service
init` once to register the new executable with the Service Control Manager.
Windows service diagnostics are appended to `logs/service.log`; use `mbox
service logs` to print them. For development, use `mbox gateway --root PATH`
to run the Gateway in the foreground with live terminal output.

The E2B adapter accepts a hosted E2B account or a self-deployed E2B API and
Sandbox URL. Provider secrets are kept on the Box host and are never included
in task callbacks.

Local Docker does not build or publish the configured image. The image named by
`local-docker.image` must already be available to Docker Desktop; verify it
with `docker image inspect IMAGE`. If the default image is not available, set
an image that contains the entrypoint runtimes you use, for example:
`mbox config set local-docker.image=python:3.12-slim`.

This repository includes a development image definition at
`box/runtimes/local-docker/sandbox.Dockerfile`. Build it locally with
`docker build -t mmdash/sandbox:latest -f box/runtimes/local-docker/sandbox.Dockerfile box/runtimes/local-docker`.
