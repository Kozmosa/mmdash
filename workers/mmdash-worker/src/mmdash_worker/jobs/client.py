"""HTTP-only client for the Core Job API."""

from __future__ import annotations

import json
from collections.abc import Mapping, Sequence
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class JobAPIError(RuntimeError):
    """A stable error returned by Core or its transport."""

    def __init__(self, code: str, message: str, status: int = 0) -> None:
        super().__init__(message)
        self.code = code
        self.status = status


class CoreJobClient:
    """Calls Core over HTTP; Worker never opens a database connection."""

    def __init__(
        self,
        base_url: str,
        api_token: str,
        *,
        timeout_seconds: float = 15.0,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_token = api_token.strip()
        self.timeout_seconds = timeout_seconds
        if not self.base_url or not self.api_token:
            raise ValueError("Core base URL and API token are required")

    def heartbeat_worker(
        self,
        worker_id: str,
        version: str,
        capabilities: Sequence[str],
        metadata: Mapping[str, Any] | None = None,
    ) -> None:
        self._request(
            "POST",
            "/v1/jobs/workers/heartbeat",
            {
                "worker_id": worker_id,
                "version": version,
                "capabilities": list(capabilities),
                "metadata": dict(metadata or {}),
            },
            expect_empty=True,
        )

    def claim(
        self,
        worker_id: str,
        job_types: Sequence[str],
        lease_seconds: int,
    ) -> dict[str, Any] | None:
        response = self._request(
            "POST",
            "/v1/jobs/claim",
            {
                "worker_id": worker_id,
                "job_types": list(job_types),
                "lease_seconds": lease_seconds,
            },
        )
        job = response.get("job")
        if job is None:
            return None
        if not isinstance(job, dict):
            raise JobAPIError("INVALID_RESPONSE", "Core returned an invalid job claim")
        return job

    def renew(self, job_id: str, worker_id: str, lease_seconds: int) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/jobs/{job_id}/heartbeat",
            {"worker_id": worker_id, "lease_seconds": lease_seconds},
        )

    def append_log(
        self,
        job_id: str,
        worker_id: str,
        level: str,
        message: str,
        fields: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/jobs/{job_id}/logs",
            {
                "worker_id": worker_id,
                "level": level,
                "message": message,
                "fields": dict(fields or {}),
            },
        )

    def complete(
        self,
        job_id: str,
        worker_id: str,
        result: Mapping[str, Any],
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/jobs/{job_id}/complete",
            {"worker_id": worker_id, "result": dict(result)},
        )

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
        return self._request(
            "POST",
            f"/v1/jobs/{job_id}/fail",
            {
                "worker_id": worker_id,
                "code": code,
                "message": message,
                "retryable": retryable,
                "retry_delay_seconds": retry_delay_seconds,
            },
        )

    def _request(
        self,
        method: str,
        path: str,
        body: Mapping[str, Any],
        *,
        expect_empty: bool = False,
    ) -> dict[str, Any]:
        encoded = json.dumps(body, separators=(",", ":")).encode("utf-8")
        request = Request(
            self.base_url + path,
            data=encoded,
            method=method,
            headers={
                "Accept": "application/json",
                "Authorization": f"Bearer {self.api_token}",
                "Content-Type": "application/json",
                "User-Agent": "mmdash-worker/0.1",
            },
        )
        try:
            with urlopen(request, timeout=self.timeout_seconds) as response:
                payload = response.read()
        except HTTPError as error:
            payload = error.read()
            try:
                decoded = json.loads(payload)
            except (json.JSONDecodeError, UnicodeDecodeError):
                decoded = {}
            raise JobAPIError(
                str(decoded.get("code", "CORE_HTTP_ERROR")),
                str(decoded.get("message", f"Core returned HTTP {error.code}")),
                error.code,
            ) from error
        except URLError as error:
            raise JobAPIError("CORE_UNAVAILABLE", str(error.reason)) from error
        if expect_empty:
            return {}
        if not payload:
            raise JobAPIError("INVALID_RESPONSE", "Core returned an empty JSON response")
        try:
            decoded = json.loads(payload)
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            raise JobAPIError("INVALID_RESPONSE", "Core returned invalid JSON") from error
        if not isinstance(decoded, dict):
            raise JobAPIError("INVALID_RESPONSE", "Core returned a non-object response")
        return decoded
