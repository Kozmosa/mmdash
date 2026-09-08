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


def test_core_agent_normalizes_known_legacy_hermes_output() -> None:
    client = FakeClient()
    client.execution["output"] = "评估完成，输出结论 JSON：\n\n" + json.dumps(
        {
            "detected_stage": "Q2建模成稿待确认",
            "summary": "Q2模型文档已经成稿。",
            "changes_since_last": ["新增Q2模型快照"],
            "completed_items": ["Q1建模"],
            "in_progress_items": ["Q2建模"],
            "blockers": [],
            "risks": ["Q1求解临近截止，存在逾期风险"],
            "pending_questions": [],
        },
        ensure_ascii=False,
    )

    result = asyncio.run(
        ProgressEvaluationHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )

    output = result["output"]
    assert output["stage"] == "Q2建模成稿待确认"
    assert output["work_state_updates"] == []
    assert output["suggestions"] == []
    assert output["risks"] == [
        {
            "key": "prose-risk:1",
            "title": "Q1求解临近截止，存在逾期风险",
            "severity": "medium",
            "detail": "Q1求解临近截止，存在逾期风险",
        }
    ]


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


def test_core_agent_defaults_missing_empty_arrays() -> None:
    client = FakeClient()
    client.execution["output"] = json.dumps(
        {
            "stage": "execution",
            "summary": "One blocker",
            "in_progress_items": ["Blocked"],
        },
        ensure_ascii=False,
    )
    result = asyncio.run(
        ProgressEvaluationHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )
    assert result["output"]["changes_since_last"] == []
    assert result["output"]["completed_items"] == []
    assert result["output"]["blockers"] == []
    assert result["output"]["risks"] == []
    assert result["output"]["work_state_updates"] == []
    assert result["output"]["suggestions"] == []
    assert result["output"]["pending_questions"] == []


def test_core_agent_drops_unknown_top_level_keys() -> None:
    client = FakeClient()
    payload = json.loads(client.execution["output"])
    payload["status"] = "ok"
    payload["notes"] = ["内部备注"]
    client.execution["output"] = json.dumps(payload, ensure_ascii=False)
    result = asyncio.run(
        ProgressEvaluationHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )
    assert result["output"]["summary"] == "One blocker"
    assert "status" not in result["output"]
    assert "notes" not in result["output"]


def test_core_agent_prefers_stage_when_detected_stage_also_present() -> None:
    client = FakeClient()
    payload = json.loads(client.execution["output"])
    payload["detected_stage"] = "legacy-stage"
    client.execution["output"] = json.dumps(payload, ensure_ascii=False)
    result = asyncio.run(
        ProgressEvaluationHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )
    assert result["output"]["stage"] == "execution"
    assert "detected_stage" not in result["output"]


def test_core_agent_rejects_duplicate_suggestion_keys_before_reporting() -> None:
    client = FakeClient()
    suggestion = {
        "key": "task.create:archive",
        "proposal_type": "task.create",
        "title": "归档实验",
        "rationale": "需要可追溯记录。",
        "changes": {"title": "归档实验", "status": "todo"},
    }
    payload = json.loads(client.execution["output"])
    payload["suggestions"] = [suggestion, dict(suggestion)]
    client.execution["output"] = json.dumps(payload, ensure_ascii=False)
    with pytest.raises(HandlerError) as caught:
        asyncio.run(
            ProgressEvaluationHandler(client)(
                HandlerContext(job_id="job-1", worker_id="worker-1"), {}
            )
        )
    assert caught.value.code == "PROGRESS_INVALID_OUTPUT"
    assert "unique" in str(caught.value)
