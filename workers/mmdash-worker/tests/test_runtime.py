import asyncio
from collections.abc import Mapping, Sequence
from typing import Any

from mmdash_worker.jobs.handlers import (
    HandlerContext,
    HandlerError,
    HandlerRegistry,
    baseline_registry,
)
from mmdash_worker.jobs.runtime import WorkerRuntime


class FakeClient:
    def __init__(
        self,
        job: dict[str, Any] | None,
        *,
        renew_error: Exception | None = None,
        renew_result: dict[str, Any] | None = None,
    ) -> None:
        self.job = job
        self.calls: list[tuple[Any, ...]] = []
        self.renew_error = renew_error
        self.renew_result = renew_result

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
        if self.renew_error is not None:
            raise self.renew_error
        return dict(self.renew_result if self.renew_result is not None else self.job or {})

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
    client = FakeClient({"id": "job-1", "job_type": "system.test", "payload": {"value": 42}})
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
    client = FakeClient({"id": "job-2", "job_type": "system.retry", "payload": {}})
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


def test_long_running_handler_renews_its_lease_before_completion() -> None:
    registry = HandlerRegistry()
    client = FakeClient({"id": "job-3", "job_type": "system.long", "payload": {}})

    async def long_handler(context: HandlerContext, _payload: object) -> dict[str, Any]:
        await asyncio.wait_for(context.lease_renewed_event.wait(), timeout=1.0)
        return {"status": "renewed"}

    registry.register("system.long", long_handler)
    runtime = WorkerRuntime(
        client,
        registry,
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=15,
        poll_seconds=0,
        renew_interval_seconds=0.001,
    )

    assert asyncio.run(runtime.run_once()) is True
    assert ("renew", "job-3", "worker-1", 15) in client.calls
    assert ("complete", "job-3", "worker-1", {"status": "renewed"}) in client.calls


def test_renewal_cancellation_submits_a_stable_failure_without_completion() -> None:
    registry = HandlerRegistry()
    client = FakeClient(
        {"id": "job-4", "job_type": "system.cancel", "payload": {}},
        renew_result={"cancel_requested_at": "2026-07-28T00:00:00Z"},
    )

    async def cancellation_aware_handler(
        context: HandlerContext, _payload: object
    ) -> dict[str, Any]:
        await asyncio.wait_for(context.cancellation_requested_event.wait(), timeout=1.0)
        assert context.cancellation_requested
        return {"status": "cancelled"}

    registry.register("system.cancel", cancellation_aware_handler)
    runtime = WorkerRuntime(
        client,
        registry,
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=12,
        poll_seconds=0,
        renew_interval_seconds=0.001,
    )

    assert asyncio.run(runtime.run_once()) is True
    assert not any(call[0] == "complete" for call in client.calls)
    assert (
        "fail",
        "job-4",
        "worker-1",
        "JOB_CANCELLED",
        "Cancellation was requested while the handler was running",
        False,
        0,
    ) in client.calls


def test_renewal_failure_submits_a_retryable_failure_without_completion() -> None:
    registry = HandlerRegistry()
    client = FakeClient(
        {"id": "job-5", "job_type": "system.renew", "payload": {}},
        renew_error=RuntimeError("Core unavailable"),
    )

    async def lease_aware_handler(context: HandlerContext, _payload: object) -> dict[str, Any]:
        await asyncio.wait_for(context.lease_renewal_failed_event.wait(), timeout=1.0)
        assert context.lease_renewal_failed
        return {"status": "lease-lost"}

    registry.register("system.renew", lease_aware_handler)
    runtime = WorkerRuntime(
        client,
        registry,
        worker_id="worker-1",
        version="0.1.0",
        lease_seconds=12,
        poll_seconds=0,
        renew_interval_seconds=0.001,
    )

    assert asyncio.run(runtime.run_once()) is True
    assert not any(call[0] == "complete" for call in client.calls)
    assert (
        "fail",
        "job-5",
        "worker-1",
        "LEASE_RENEWAL_FAILED",
        "The worker could not renew the job lease",
        True,
        0,
    ) in client.calls
