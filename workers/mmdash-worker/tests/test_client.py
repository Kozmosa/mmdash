import io
import json
from typing import Any, Self
from urllib.error import HTTPError

import pytest

from mmdash_worker.jobs import client as client_module
from mmdash_worker.jobs.client import CoreJobClient, JobAPIError


class FakeResponse:
    def __init__(self, payload: bytes = b"{}") -> None:
        self.payload = payload

    def __enter__(self) -> Self:
        return self

    def __exit__(self, *_args: object) -> None:
        return None

    def read(self) -> bytes:
        return self.payload


def test_claim_sends_api_token_and_decodes_empty_poll(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, Any] = {}

    def fake_urlopen(request: object, timeout: float) -> FakeResponse:
        captured["request"] = request
        captured["timeout"] = timeout
        return FakeResponse(b'{"job":null}')

    monkeypatch.setattr(client_module, "urlopen", fake_urlopen)
    client = CoreJobClient("http://core:8080/", "secret-token", timeout_seconds=3)
    assert client.claim("worker-1", ["system.test"], 60) is None
    request = captured["request"]
    assert request.full_url == "http://core:8080/v1/jobs/claim"
    assert request.headers["Authorization"] == "Bearer secret-token"
    assert json.loads(request.data) == {
        "worker_id": "worker-1",
        "job_types": ["system.test"],
        "lease_seconds": 60,
    }
    assert captured["timeout"] == 3


def test_core_error_envelope_becomes_stable_exception(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def fake_urlopen(_request: object, timeout: float) -> FakeResponse:
        del timeout
        raise HTTPError(
            "http://core/v1/jobs/claim",
            409,
            "Conflict",
            {},
            io.BytesIO(b'{"code":"JOB_LEASE_LOST","message":"lease expired"}'),
        )

    monkeypatch.setattr(client_module, "urlopen", fake_urlopen)
    client = CoreJobClient("http://core:8080", "secret-token")
    with pytest.raises(JobAPIError) as caught:
        client.renew("job-1", "worker-1", 60)
    assert caught.value.code == "JOB_LEASE_LOST"
    assert caught.value.status == 409
    assert str(caught.value) == "lease expired"


def test_artifact_transfer_request_uses_worker_token(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, Any] = {}

    def fake_urlopen(request: object, timeout: float) -> FakeResponse:
        captured["request"] = request
        captured["timeout"] = timeout
        return FakeResponse(
            b'{"method":"GET","url":"http://core/transfer","headers":{},'
            b'"expires_at":"2026-07-30T12:00:00Z"}'
        )

    monkeypatch.setattr(client_module, "urlopen", fake_urlopen)
    client = CoreJobClient(
        "http://core:8080",
        "worker-token",
        timeout_seconds=4,
        model_export_timeout_seconds=123,
        model_completion_timeout_seconds=234,
    )
    result = client.request_artifact_transfer(
        "job-1",
        {"direction": "input", "version_id": "version-1"},
    )
    request = captured["request"]
    assert request.full_url.endswith("/v1/internal/artifact-preview-jobs/job-1/transfers")
    assert request.headers["Authorization"] == "Bearer worker-token"
    assert json.loads(request.data) == {
        "direction": "input",
        "version_id": "version-1",
    }
    assert captured["timeout"] == 4
    assert result["method"] == "GET"


def test_model_completion_uses_media_transfer_timeout(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, Any] = {}

    def fake_urlopen(request: object, timeout: float) -> FakeResponse:
        captured["request"] = request
        captured["timeout"] = timeout
        return FakeResponse(b'{"id":"job-1","status":"succeeded"}')

    monkeypatch.setattr(client_module, "urlopen", fake_urlopen)
    client = CoreJobClient(
        "http://core:8080",
        "worker-token",
        timeout_seconds=4,
        model_export_timeout_seconds=123,
        model_completion_timeout_seconds=234,
    )

    result = client.complete("job-1", "worker-1", {"media": []})

    request = captured["request"]
    assert request.full_url.endswith("/v1/jobs/job-1/complete")
    assert captured["timeout"] == 234
    assert result["status"] == "succeeded"


def test_model_notion_export_uses_bodyless_get(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, Any] = {}

    def fake_urlopen(request: object, timeout: float) -> FakeResponse:
        captured["request"] = request
        captured["timeout"] = timeout
        return FakeResponse(b'{"mode":"discover","pages":[]}')

    monkeypatch.setattr(client_module, "urlopen", fake_urlopen)
    client = CoreJobClient(
        "http://core:8080",
        "worker-token",
        timeout_seconds=4,
        model_export_timeout_seconds=123,
        model_completion_timeout_seconds=234,
    )

    result = client.get_model_notion_export("job-1")

    request = captured["request"]
    assert request.full_url.endswith("/v1/internal/model-notion-jobs/job-1/export")
    assert request.get_method() == "GET"
    assert request.data is None
    assert request.headers["Authorization"] == "Bearer worker-token"
    assert captured["timeout"] == 123
    assert result == {"mode": "discover", "pages": []}
