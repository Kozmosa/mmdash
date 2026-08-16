"""Worker handler registration and dispatch."""

from __future__ import annotations

import inspect
import re
from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass, field
from typing import Any

JobHandler = Callable[
    ["HandlerContext", Mapping[str, Any]],
    Mapping[str, Any] | Awaitable[Mapping[str, Any]],
]
JOB_TYPE_PATTERN = re.compile(r"^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$")


class HandlerError(RuntimeError):
    """A safe handler failure that controls Core retry policy."""

    def __init__(
        self,
        code: str,
        message: str,
        *,
        retryable: bool = False,
        retry_delay_seconds: int = 0,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.retryable = retryable
        self.retry_delay_seconds = retry_delay_seconds


@dataclass
class HandlerContext:
    """Per-attempt context with cooperative cancellation."""

    job_id: str
    worker_id: str
    cancellation_requested: bool = False
    lease_renewal_failed: bool = False
    metadata: dict[str, Any] = field(default_factory=dict)


class HandlerRegistry:
    """Maps stable job type names to runtime handlers."""

    def __init__(self) -> None:
        self._handlers: dict[str, JobHandler] = {}

    def register(self, job_type: str, handler: JobHandler) -> None:
        if not JOB_TYPE_PATTERN.fullmatch(job_type):
            raise ValueError("job type must be a stable dotted name")
        if job_type in self._handlers:
            raise ValueError(f"handler already registered: {job_type}")
        self._handlers[job_type] = handler

    def handler(self, job_type: str) -> JobHandler:
        try:
            return self._handlers[job_type]
        except KeyError as error:
            raise HandlerError(
                "HANDLER_NOT_REGISTERED",
                f"No Worker handler is registered for {job_type}",
            ) from error

    def names(self) -> tuple[str, ...]:
        return tuple(sorted(self._handlers))

    async def dispatch(
        self,
        job_type: str,
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> dict[str, Any]:
        result = self.handler(job_type)(context, payload)
        if inspect.isawaitable(result):
            result = await result
        if not isinstance(result, Mapping):
            raise HandlerError(
                "INVALID_HANDLER_RESULT",
                f"Handler {job_type} returned a non-object result",
            )
        return dict(result)


def baseline_registry() -> HandlerRegistry:
    """Return only the stage-3.11 test handler, never product handlers."""

    registry = HandlerRegistry()

    def system_test(
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        return {
            "echo": dict(payload),
            "handled_by": context.worker_id,
            "handler": "system.test",
        }

    registry.register("system.test", system_test)
    return registry


def worker_registry(artifact_client: Any) -> HandlerRegistry:
    """Return the production registry with Artifact and Model handlers."""

    from mmdash_worker.article import ArticleBuildHandler
    from mmdash_worker.experiment import ExperimentResultHandler, compare_results
    from mmdash_worker.model_sync import ModelNotionHandler
    from mmdash_worker.preview import ArtifactPreviewHandler, PreviewConfig, PreviewProcessor
    from mmdash_worker.progress_tracking import ProgressEvaluationHandler

    registry = baseline_registry()
    registry.register(
        "artifact.preview",
        ArtifactPreviewHandler(
            artifact_client,
            PreviewProcessor(PreviewConfig.from_environment()),
        ),
    )
    model_handler = ModelNotionHandler(artifact_client)
    registry.register("model.notion.discover", model_handler)
    registry.register("model.notion.snapshot", model_handler)
    registry.register(
        "progress.evaluate",
        ProgressEvaluationHandler.from_environment(artifact_client),
    )
    registry.register(
        "artifact.semantic.describe",
        lambda context, payload: artifact_client.execute_artifact_semantic_description(
            context.job_id
        ),
    )
    registry.register("experiment.result.process", ExperimentResultHandler(artifact_client))
    registry.register("experiment.result.compare", compare_results)
    registry.register("article.build", ArticleBuildHandler(artifact_client))
    return registry
