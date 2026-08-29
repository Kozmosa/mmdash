"""Evaluate versioned Progress snapshots through Hermes or an explicit mock."""

from __future__ import annotations

import asyncio
import json
import os
from collections.abc import Mapping, Sequence
from typing import Any, Protocol

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError

MAX_AGENT_OUTPUT_BYTES = 2 * 1024 * 1024


class ProgressEvaluationClient(Protocol):
    def get_progress_evaluation_input(self, job_id: str) -> dict[str, Any]: ...

    def execute_progress_evaluation(self, job_id: str) -> dict[str, Any]: ...


class ProgressEvaluationHandler:
    """Return the validated result consumed by the Progress lifecycle hook."""

    def __init__(self, client: ProgressEvaluationClient, mode: str = "core_agent") -> None:
        normalized = mode.strip().lower()
        if normalized == "hermes":
            normalized = "core_agent"
        if normalized not in {"core_agent", "mock"}:
            raise ValueError("Progress evaluator mode must be core_agent or mock")
        self.client = client
        self.mode = normalized

    @classmethod
    def from_environment(cls, client: ProgressEvaluationClient) -> ProgressEvaluationHandler:
        return cls(client, os.environ.get("MMDASH_PROGRESS_EVALUATOR_MODE", "core_agent"))

    async def __call__(
        self,
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        del payload
        if context.cancellation_requested:
            raise HandlerError("JOB_CANCELLED", "Progress evaluation was cancelled")
        if self.mode == "mock":
            evaluation = await asyncio.to_thread(
                self.client.get_progress_evaluation_input, context.job_id
            )
            output = _mock_output(evaluation)
            return {"output": output, "evaluator_mode": "mock"}
        execution = await asyncio.to_thread(self.client.execute_progress_evaluation, context.job_id)
        output = _parse_agent_output(execution.get("output"))
        return {
            "output": output,
            "evaluator_mode": "core_agent",
            "agent_instance_id": _required_string(execution, "agent_instance_id"),
            "agent_session_id": _required_string(execution, "agent_session_id"),
            "agent_run_id": _required_string(execution, "agent_run_id"),
        }


def _parse_agent_output(value: Any) -> dict[str, Any]:
    if not isinstance(value, str) or not value.strip():
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Agent returned no Progress output")
    encoded = value.strip().encode()
    if len(encoded) > MAX_AGENT_OUTPUT_BYTES:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Agent Progress output is too large")
    parsed = _decode_agent_json(value)
    return _validate_output(parsed)


def _decode_agent_json(value: str) -> Any:
    """Decode a strict response or one final JSON object after harmless preamble.

    Some runtimes persist short progress notes in the same assistant message as
    the final structured answer. Only a complete trailing object is accepted;
    commentary after it, a partial object, or an embedded object remains an
    invalid Progress result.
    """

    stripped = value.strip()
    try:
        return json.loads(stripped)
    except json.JSONDecodeError as strict_error:
        candidate_start: int | None = None
        depth = 0
        in_string = False
        escaped = False
        for index, character in enumerate(stripped):
            if depth == 0:
                if character == "{":
                    candidate_start = index
                    depth = 1
                continue
            if in_string:
                if escaped:
                    escaped = False
                elif character == "\\":
                    escaped = True
                elif character == '"':
                    in_string = False
                continue
            if character == '"':
                in_string = True
            elif character == "{":
                depth += 1
            elif character == "}":
                depth -= 1
                if depth == 0 and candidate_start is not None:
                    suffix = stripped[index + 1 :].strip()
                    if suffix in {"", "```"}:
                        try:
                            return json.loads(stripped[candidate_start : index + 1])
                        except json.JSONDecodeError:
                            pass
                    candidate_start = None
        raise HandlerError(
            "PROGRESS_INVALID_OUTPUT", "Agent Progress output is not valid JSON"
        ) from strict_error


def _validate_output(value: Any) -> dict[str, Any]:
    if not isinstance(value, Mapping):
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress output must be an object")
    allowed = {
        "stage",
        "summary",
        "changes_since_last",
        "completed_items",
        "in_progress_items",
        "blockers",
        "risks",
        "work_state_updates",
        "suggestions",
        "pending_questions",
    }
    if set(value) != allowed:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress output fields are invalid")
    stage = _required_string(value, "stage")
    summary = _required_string(value, "summary")
    result: dict[str, Any] = {
        "stage": stage,
        "summary": summary,
        "changes_since_last": _string_list(value, "changes_since_last", 200),
        "completed_items": _string_list(value, "completed_items", 200),
        "in_progress_items": _string_list(value, "in_progress_items", 200),
        "blockers": _string_list(value, "blockers", 200),
        "pending_questions": _string_list(value, "pending_questions", 200),
    }
    risks = value.get("risks")
    work_state_updates = value.get("work_state_updates")
    suggestions = value.get("suggestions")
    if not isinstance(risks, list) or len(risks) > 100:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress risks are invalid")
    if not isinstance(suggestions, list) or len(suggestions) > 100:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress suggestions are invalid")
    if not isinstance(work_state_updates, list) or len(work_state_updates) > 200:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress work states are invalid")
    result["risks"] = [_risk(item) for item in risks]
    result["work_state_updates"] = [_work_state_update(item) for item in work_state_updates]
    result["suggestions"] = [_suggestion(item) for item in suggestions]
    return result


def _risk(value: Any) -> dict[str, str]:
    item = _mapping(value)
    severity = _required_string(item, "severity")
    if severity not in {"low", "medium", "high", "critical"}:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress risk severity is invalid")
    return {
        "key": _required_string(item, "key"),
        "title": _required_string(item, "title"),
        "severity": severity,
        "detail": str(item.get("detail", "")),
    }


def _suggestion(value: Any) -> dict[str, Any]:
    item = _mapping(value)
    proposal_type = _required_string(item, "proposal_type")
    if proposal_type not in {
        "milestone.create",
        "milestone.update",
        "milestone.complete",
        "task.create",
        "task.update",
        "task.complete",
    }:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress suggestion type is invalid")
    changes = item.get("changes")
    if not isinstance(changes, Mapping):
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress suggestion changes are invalid")
    result: dict[str, Any] = {
        "key": _required_string(item, "key"),
        "proposal_type": proposal_type,
        "title": _required_string(item, "title"),
        "rationale": str(item.get("rationale", "")),
        "changes": dict(changes),
    }
    target_id = item.get("target_id")
    if target_id is not None:
        if not isinstance(target_id, str) or not target_id.strip():
            raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress target is invalid")
        result["target_id"] = target_id.strip()
    return result


def _work_state_update(value: Any) -> dict[str, str]:
    item = _mapping(value)
    if set(item) != {"task_id", "state"}:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress work state fields are invalid")
    state = _required_string(item, "state")
    if state not in {"todo", "in_progress", "blocked"}:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress work state is invalid")
    return {"task_id": _required_string(item, "task_id"), "state": state}


def _mock_output(evaluation: Mapping[str, Any]) -> dict[str, Any]:
    snapshot = _mapping(evaluation.get("input_snapshot"))
    progress = _mapping(snapshot.get("progress"))
    tasks = _mapping_sequence(progress.get("tasks"))
    milestones = _mapping_sequence(progress.get("milestones"))
    completed = [_label(item) for item in tasks if item.get("status") == "done"]
    active = [_label(item) for item in tasks if item.get("status") in {"todo", "in_progress"}]
    blockers = [_label(item) for item in tasks if item.get("status") == "blocked"]
    if not tasks and not milestones:
        stage = "planning"
    elif tasks and all(item.get("status") == "done" for item in tasks):
        stage = "review"
    else:
        stage = "execution"
    risks = [
        {
            "key": f"blocked-task:{item.get('task_id', index)}",
            "title": _label(item),
            "severity": "high",
            "detail": "Task is currently blocked.",
        }
        for index, item in enumerate(tasks)
        if item.get("status") == "blocked"
    ]
    suggestions: list[dict[str, Any]] = []
    if not tasks and not milestones:
        suggestions = [
            {
                "key": "mock:first-milestone",
                "proposal_type": "milestone.create",
                "title": "Define first milestone",
                "rationale": "The Project has no human-confirmed milestone yet.",
                "changes": {"title": "Define first milestone", "critical": True},
            },
            {
                "key": "mock:first-task",
                "proposal_type": "task.create",
                "title": "Define next step",
                "rationale": "The Project has no actionable TODO yet.",
                "changes": {"title": "Define next step", "status": "todo"},
            },
        ]
    return _validate_output(
        {
            "stage": stage,
            "summary": f"Mock evaluation: {len(completed)} completed, {len(active)} active, {len(blockers)} blocked.",
            "changes_since_last": [],
            "completed_items": completed,
            "in_progress_items": active,
            "blockers": blockers,
            "risks": risks,
            "work_state_updates": [],
            "suggestions": suggestions,
            "pending_questions": [],
        }
    )


def _mapping(value: Any) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress value is invalid")
    return value


def _mapping_sequence(value: Any) -> list[Mapping[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes)):
        raise HandlerError("PROGRESS_INVALID_OUTPUT", "Progress collection is invalid")
    return [_mapping(item) for item in value]


def _required_string(value: Mapping[str, Any], key: str) -> str:
    item = value.get(key)
    if not isinstance(item, str) or not item.strip():
        raise HandlerError("PROGRESS_INVALID_OUTPUT", f"Progress {key} is required")
    return item.strip()


def _string_list(value: Mapping[str, Any], key: str, maximum: int) -> list[str]:
    items = value.get(key)
    if not isinstance(items, list) or len(items) > maximum:
        raise HandlerError("PROGRESS_INVALID_OUTPUT", f"Progress {key} is invalid")
    result: list[str] = []
    for item in items:
        if not isinstance(item, str):
            raise HandlerError("PROGRESS_INVALID_OUTPUT", f"Progress {key} is invalid")
        result.append(item)
    return result


def _label(value: Mapping[str, Any]) -> str:
    for key in ("title", "task_id", "milestone_id"):
        candidate = value.get(key)
        if isinstance(candidate, str) and candidate.strip():
            return candidate.strip()
    return "Untitled Progress item"
