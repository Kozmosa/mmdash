import asyncio
from collections.abc import Mapping, Sequence
from typing import Any

from mmdash_worker.jobs.handlers import HandlerError, HandlerRegistry, baseline_registry
from mmdash_worker.jobs.runtime import WorkerRuntime


class FakeClient:
    def __init__(self, job: dict[str, Any] | None) -> None:
        self.job = job
        self.calls: list[tuple[Any, ...]] = []

    def heartbeat_worker(
        self,
        worker_id: str,
        version: str,
        capabilities: Sequence[str],
        metadata: Mapping[str, Any] | None = None,
    ) -> None:
        self.calls.append(("heartbeat", worker_id, version, tuple(capabilities), metadata))

    def claim(
        self,
        worker_id: str,
        job_types: Sequence[str],
        lease_seconds: int,
    ) -> dict[str, Any] | None:
        self.calls.append(("claim", worker_id, tuple(job_types), lease_seconds))
        return self.job

    def renew(self, job_id: str, worker_id: str, lease_seconds: int) -> dict[str, Any]:
        self.calls.append(("renew", job_id, worker_id, lease_seconds))
        return dict(self.job or {})

    def append_log(
        self,
        job_id: str,
        worker_id: str,
        level: str,
        message: str,
        fields: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]:
        self.calls.append(("log", job_id, worker_id, level, message, fields))
        return {"id": "log-1"}

    def complete(
        self,
        job_id: str,
        worker_id: str,
        result: Mapping[str, Any],
    ) -> dict[str, Any]:
        self.calls.append(("complete", job_id, worker_id, result))
        return {"id": job_id, "status": "succeeded"}

    def fail(
        self,
        job_id: str,
        worker_id: str,
        *,
        code: str,
        message: str,
        retryable: bool,
        retry_delay_seconds: int = 0,
    ) -> dict[str, Any]:
        self.calls.append(
            ("fail", job_id, worker_id, code, message, retryable, retry_delay_seconds)
        )
        return {"id": job_id, "status": "queued" if retryable else "failed"}


def test_run_once_heartbeats_claims_dispatches_and_completes() -> None:
    client = FakeClient(
        {"id": "job-1", "job_type": "system.test", "payload": {"value": 42}}
    )
    runtime = WorkerRuntime(
        client,
        baseline_registry(),
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=10,
        poll_seconds=0,
    )
    assert asyncio.run(runtime.run_once()) is True
    assert client.calls[0][:4] == (
        "heartbeat",
        "worker-1",
        "0.1.0",
        ("system.test",),
    )
    assert client.calls[1] == ("claim", "worker-1", ("system.test",), 10)
    assert client.calls[-1] == (
        "complete",
        "job-1",
        "worker-1",
        {
            "echo": {"value": 42},
            "handled_by": "worker-1",
            "handler": "system.test",
        },
    )


def test_safe_handler_failure_is_submitted_with_retry_policy() -> None:
    registry = HandlerRegistry()

    def fail_handler(_context: object, _payload: object) -> dict[str, Any]:
        raise HandlerError(
            "TEMPORARY_FAILURE",
            "retry later",
            retryable=True,
            retry_delay_seconds=7,
        )

    registry.register("system.retry", fail_handler)
    client = FakeClient(
        {"id": "job-2", "job_type": "system.retry", "payload": {}}
    )
    runtime = WorkerRuntime(
        client,
        registry,
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=10,
        poll_seconds=0,
    )
    assert asyncio.run(runtime.run_once()) is True
    assert client.calls[-1] == (
        "fail",
        "job-2",
        "worker-1",
        "TEMPORARY_FAILURE",
        "retry later",
        True,
        7,
    )


def test_empty_poll_reports_no_work_without_dispatch() -> None:
    client = FakeClient(None)
    runtime = WorkerRuntime(
        client,
        baseline_registry(),
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=10,
        poll_seconds=0,
    )
    assert asyncio.run(runtime.run_once()) is False
    assert [call[0] for call in client.calls] == ["heartbeat", "claim"]
