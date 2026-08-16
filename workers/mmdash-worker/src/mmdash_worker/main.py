"""Worker process entry point."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import socket

from mmdash_worker import __version__
from mmdash_worker.jobs import CoreJobClient, WorkerRuntime, worker_registry


def status() -> dict[str, str]:
    """Return the process identity used by baseline checks."""
    return {"service": "mmdash-worker", "version": __version__, "status": "ready"}


def main() -> None:
    """Run the HTTP-only Job Worker or print a machine-readable status."""
    parser = argparse.ArgumentParser(prog="mmdash-worker")
    parser.add_argument("--once", action="store_true", help="poll and handle at most one job")
    parser.add_argument("--status", action="store_true", help="print process identity and exit")
    arguments = parser.parse_args()
    if arguments.status:
        print(json.dumps(status(), separators=(",", ":")))
        return

    token = os.environ.get("MMDASH_WORKER_API_TOKEN", "").strip()
    if not token:
        parser.error("MMDASH_WORKER_API_TOKEN is required")
    worker_id = os.environ.get(
        "MMDASH_WORKER_ID",
        f"{socket.gethostname()}-{os.getpid()}",
    )
    lease_seconds = int(os.environ.get("MMDASH_WORKER_LEASE_SECONDS", "60"))
    poll_seconds = float(os.environ.get("MMDASH_WORKER_POLL_SECONDS", "2"))
    client = CoreJobClient(
        os.environ.get("MMDASH_CORE_URL", "http://localhost:8080"),
        token,
        model_export_timeout_seconds=float(
            os.environ.get("MMDASH_WORKER_MODEL_EXPORT_TIMEOUT_SECONDS", "300")
        ),
        model_completion_timeout_seconds=float(
            os.environ.get("MMDASH_WORKER_MODEL_COMPLETION_TIMEOUT_SECONDS", "300")
        ),
        progress_evaluation_timeout_seconds=float(
            os.environ.get("MMDASH_WORKER_PROGRESS_EVALUATION_TIMEOUT_SECONDS", "900")
        ),
        experiment_result_timeout_seconds=float(
            os.environ.get("MMDASH_WORKER_EXPERIMENT_RESULT_TIMEOUT_SECONDS", "3600")
        ),
    )
    runtime = WorkerRuntime(
        client,
        worker_registry(client),
        worker_id=worker_id,
        version=__version__,
        lease_seconds=lease_seconds,
        poll_seconds=poll_seconds,
    )
    if arguments.once:
        asyncio.run(runtime.run_once())
    else:
        asyncio.run(runtime.run_forever())


if __name__ == "__main__":
    main()
