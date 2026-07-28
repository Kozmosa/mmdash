import asyncio

import pytest

from mmdash_worker.jobs.handlers import (
    HandlerContext,
    HandlerError,
    HandlerRegistry,
    baseline_registry,
)


def test_registry_rejects_duplicate_handlers() -> None:
    registry = HandlerRegistry()
    registry.register("system.test", lambda _context, _payload: {})
    with pytest.raises(ValueError, match="already registered"):
        registry.register("system.test", lambda _context, _payload: {})


def test_registry_rejects_non_canonical_job_type() -> None:
    with pytest.raises(ValueError, match="stable dotted name"):
        HandlerRegistry().register("System_Test", lambda _context, _payload: {})


def test_baseline_registry_contains_only_test_handler() -> None:
    registry = baseline_registry()
    assert registry.names() == ("system.test",)
    result = asyncio.run(
        registry.dispatch(
            "system.test",
            HandlerContext(job_id="job-1", worker_id="worker-1"),
            {"value": 42},
        )
    )
    assert result == {
        "echo": {"value": 42},
        "handled_by": "worker-1",
        "handler": "system.test",
    }


def test_missing_handler_is_a_non_retryable_safe_failure() -> None:
    with pytest.raises(HandlerError) as caught:
        asyncio.run(
            HandlerRegistry().dispatch(
                "article.build",
                HandlerContext(job_id="job-1", worker_id="worker-1"),
                {},
            )
        )
    assert caught.value.code == "HANDLER_NOT_REGISTERED"
    assert caught.value.retryable is False
