"""Async Worker loop built exclusively on the Core Job API."""

from __future__ import annotations

import asyncio
from collections.abc import Mapping, Sequence
from typing import Any, Protocol

from mmdash_worker.jobs.client import JobAPIError
from mmdash_worker.jobs.handlers import HandlerContext, HandlerError, HandlerRegistry


class JobClient(Protocol):
    """Synchronous client surface executed on asyncio's thread pool."""

    def heartbeat_worker(
        self,
        worker_id: str,
        version: str,
        capabilities: Sequence[str],
        metadata: Mapping[str, Any] | None = None,
    ) -> None: ...

    def claim(
        self,
        worker_id: str,
        job_types: Sequence[str],
        lease_seconds: int,
    ) -> dict[str, Any] | None: ...

    def renew(self, job_id: str, worker_id: str, lease_seconds: int) -> dict[str, Any]: ...

    def append_log(
        self,
        job_id: str,
        worker_id: str,
        level: str,
        message: str,
        fields: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]: ...

    def complete(
        self,
        job_id: str,
        worker_id: str,
        result: Mapping[str, Any],
    ) -> dict[str, Any]: ...

    def fail(
        self,
        job_id: str,
        worker_id: str,
        *,
        code: str,
        message: str,
        retryable: bool,
        retry_delay_seconds: int = 0,
    ) -> dict[str, Any]: ...


class WorkerRuntime:
    """Claims one job at a time and keeps its lease alive during dispatch."""

    def __init__(
        self,
        client: JobClient,
        registry: HandlerRegistry,
        *,
        worker_id: str,
        version: str,
        lease_seconds: int = 60,
        poll_seconds: float = 2.0,
        renew_interval_seconds: float | None = None,
    ) -> None:
        if lease_seconds < 10:
            raise ValueError("lease_seconds must be at least 10")
        if poll_seconds < 0:
            raise ValueError("poll_seconds cannot be negative")
        if renew_interval_seconds is not None and renew_interval_seconds < 0:
            raise ValueError("renew_interval_seconds cannot be negative")
        self.client = client
        self.registry = registry
        self.worker_id = worker_id
        self.version = version
        self.lease_seconds = lease_seconds
        self.poll_seconds = poll_seconds
        self.renew_interval_seconds = renew_interval_seconds

    async def run_once(self) -> bool:
        """Heartbeat, claim, and process at most one job."""

        capabilities = self.registry.names()
        await asyncio.to_thread(
            self.client.heartbeat_worker,
            self.worker_id,
            self.version,
            capabilities,
            {"runtime": "python"},
        )
        job = await asyncio.to_thread(
            self.client.claim,
            self.worker_id,
            capabilities,
            self.lease_seconds,
        )
        if job is None:
            return False
        await self._process(job)
        return True

    async def run_forever(self) -> None:
        """Poll until cancelled by process shutdown."""

        while True:
            handled = await self.run_once()
            if not handled:
                await asyncio.sleep(self.poll_seconds)

    async def _process(self, job: Mapping[str, Any]) -> None:
        job_id = str(job.get("id", ""))
        job_type = str(job.get("job_type", ""))
        payload = job.get("payload")
        if not job_id or not isinstance(payload, Mapping):
            raise RuntimeError("Core returned an invalid claimed job")
        context = HandlerContext(job_id=job_id, worker_id=self.worker_id)
        stop_renewal = asyncio.Event()
        renewal = asyncio.create_task(self._renew_loop(context, stop_renewal))
        try:
            await self._safe_log(job_id, "info", "job.started", {"job_type": job_type})
            result = await self.registry.dispatch(job_type, context, payload)
            if context.cancellation_requested:
                raise HandlerError(
                    "JOB_CANCELLED",
                    "Cancellation was requested while the handler was running",
                )
            if context.lease_renewal_failed:
                raise HandlerError(
                    "LEASE_RENEWAL_FAILED",
                    "The worker could not renew the job lease",
                    retryable=True,
                )
            await asyncio.to_thread(
                self.client.complete,
                job_id,
                self.worker_id,
                result,
            )
        except HandlerError as error:
            await self._submit_failure(job_id, error)
        except JobAPIError as error:
            await self._submit_failure(
                job_id,
                HandlerError(
                    error.code,
                    str(error),
                    retryable=error.status == 0 or error.status >= 500,
                ),
            )
        except Exception:  # noqa: BLE001 - handler boundary must isolate plugin errors
            await self._submit_failure(
                job_id,
                HandlerError(
                    "HANDLER_FAILED",
                    "Unhandled handler error",
                    retryable=True,
                ),
            )
        finally:
            stop_renewal.set()
            await renewal

    async def _renew_loop(
        self,
        context: HandlerContext,
        stop: asyncio.Event,
    ) -> None:
        interval = self.renew_interval_seconds
        if interval is None:
            interval = max(1.0, self.lease_seconds / 2)
        while True:
            try:
                await asyncio.wait_for(stop.wait(), timeout=interval)
                return
            except TimeoutError:
                try:
                    job = await asyncio.to_thread(
                        self.client.renew,
                        context.job_id,
                        self.worker_id,
                        self.lease_seconds,
                    )
                except Exception:  # noqa: BLE001 - lease loss must fail the attempt safely
                    context.lease_renewal_failed = True
                    return
                if job.get("cancel_requested_at"):
                    context.cancellation_requested = True

    async def _submit_failure(self, job_id: str, error: HandlerError) -> None:
        await self._safe_log(
            job_id,
            "error",
            "job.failed",
            {"code": error.code, "retryable": error.retryable},
        )
        await asyncio.to_thread(
            self.client.fail,
            job_id,
            self.worker_id,
            code=error.code,
            message=str(error),
            retryable=error.retryable,
            retry_delay_seconds=error.retry_delay_seconds,
        )

    async def _safe_log(
        self,
        job_id: str,
        level: str,
        message: str,
        fields: Mapping[str, Any],
    ) -> None:
        try:
            await asyncio.to_thread(
                self.client.append_log,
                job_id,
                self.worker_id,
                level,
                message,
                fields,
            )
        except Exception:  # noqa: BLE001 - logging must not hide a handler result
            return
