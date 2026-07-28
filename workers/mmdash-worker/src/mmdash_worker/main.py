"""Worker process entry point."""

from __future__ import annotations

import json

from mmdash_worker import __version__


def status() -> dict[str, str]:
    """Return the process identity used by baseline checks."""
    return {"service": "mmdash-worker", "version": __version__, "status": "ready"}


def main() -> None:
    """Print a machine-readable status until the job runtime is introduced."""
    print(json.dumps(status(), separators=(",", ":")))


if __name__ == "__main__":
    main()
