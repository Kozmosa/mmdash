import asyncio
import json
from typing import Any

import pytest

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError
from mmdash_worker.progress_tracking.handler import ProgressEvaluationHandler


class FakeClient:
    def __init__(self) -> None:
        self.input: dict[str, Any] = {
            "input_snapshot": {
                "progress": {
                    "milestones": [{"milestone_id": "m-1", "title": "Ship"}],
                    "tasks": [
                        {"task_id": "t-1", "title": "Done", "status": "done"},
                        {"task_id": "t-2", "title": "Blocked", "status": "blocked"},
                    ],
                }
            }
        }
        self.execution: dict[str, Any] = {
            "output": json.dumps(
                {
                    "stage": "execution",
                    "summary": "One blocker",
                    "changes_since_last": [],
                    "completed_items": ["Done"],
                    "in_progress_items": [],
                    "blockers": ["Blocked"],
                    "risks": [],
                    "work_state_updates": [],
                    "suggestions": [],
                    "pending_questions": [],
                }
            ),
            "agent_instance_id": "instance-1",
            "agent_session_id": "session-1",
            "agent_run_id": "run-1",
        }

    def get_progress_evaluation_input(self, job_id: str) -> dict[str, Any]:
        assert job_id == "job-1"
        return self.input

    def execute_progress_evaluation(self, job_id: str) -> dict[str, Any]:
        assert job_id == "job-1"
        return self.execution


def test_mock_evaluator_is_deterministic_and_reports_blockers() -> None:
    result = asyncio.run(
        ProgressEvaluationHandler(FakeClient(), "mock")(
            HandlerContext(job_id="job-1", worker_id="worker-1"), {}
        )
    )
    assert result["evaluator_mode"] == "mock"
    assert result["output"]["stage"] == "execution"
    assert result["output"]["completed_items"] == ["Done"]
    assert result["output"]["risks"][0]["key"] == "blocked-task:t-2"


def test_mock_evaluator_exercises_task_and_milestone_boundaries() -> None:
    client = FakeClient()
    client.input = {"input_snapshot": {"progress": {"milestones": [], "tasks": []}}}
    result = asyncio.run(
        ProgressEvaluationHandler(client, "mock")(
            HandlerContext(job_id="job-1", worker_id="worker-1"), {}
        )
    )
    assert result["output"]["stage"] == "planning"
    assert [item["proposal_type"] for item in result["output"]["suggestions"]] == [
        "milestone.create",
        "task.create",
    ]


def test_core_agent_output_preserves_agent_provenance() -> None:
    result = asyncio.run(
        ProgressEvaluationHandler(FakeClient())(
            HandlerContext(job_id="job-1", worker_id="worker-1"), {}
        )
    )
    assert result["output"]["summary"] == "One blocker"
    assert result["agent_session_id"] == "session-1"
    assert result["agent_run_id"] == "run-1"


def test_core_agent_accepts_one_trailing_json_object_after_progress_notes() -> None:
    client = FakeClient()
    client.execution["output"] = (
        "已完成项目与进度核验。下面给出最终结果。\n\n" + client.execution["output"]
    )
    result = asyncio.run(
        ProgressEvaluationHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )
    assert result["output"]["summary"] == "One blocker"


def test_core_agent_rejects_commentary_after_the_json_object() -> None:
    client = FakeClient()
    client.execution["output"] += "\n评估结束。"
    with pytest.raises(HandlerError) as caught:
        asyncio.run(
            ProgressEvaluationHandler(client)(
                HandlerContext(job_id="job-1", worker_id="worker-1"), {}
            )
        )
    assert caught.value.code == "PROGRESS_INVALID_OUTPUT"


def test_invalid_agent_json_is_safe_non_retryable_failure() -> None:
    client = FakeClient()
    client.execution["output"] = "not json"
    with pytest.raises(HandlerError) as caught:
        asyncio.run(
            ProgressEvaluationHandler(client)(
                HandlerContext(job_id="job-1", worker_id="worker-1"), {}
            )
        )
    assert caught.value.code == "PROGRESS_INVALID_OUTPUT"
    assert caught.value.retryable is False
