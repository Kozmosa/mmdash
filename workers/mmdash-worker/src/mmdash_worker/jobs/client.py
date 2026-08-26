"""HTTP-only client for the Core Job API."""

from __future__ import annotations

import http.client
import json
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import SplitResult, urlsplit, urlunsplit
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
        model_export_timeout_seconds: float = 300.0,
        model_completion_timeout_seconds: float = 300.0,
        progress_evaluation_timeout_seconds: float = 900.0,
        experiment_result_timeout_seconds: float = 3600.0,
        transfer_origin_override: str = "",
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_token = api_token.strip()
        self.timeout_seconds = timeout_seconds
        self.model_export_timeout_seconds = model_export_timeout_seconds
        self.model_completion_timeout_seconds = model_completion_timeout_seconds
        self.progress_evaluation_timeout_seconds = progress_evaluation_timeout_seconds
        self.experiment_result_timeout_seconds = experiment_result_timeout_seconds
        self.transfer_origin_override = self._validated_transfer_origin_override(
            transfer_origin_override
        )
        if not self.base_url or not self.api_token:
            raise ValueError("Core base URL and API token are required")
        if (
            self.timeout_seconds <= 0
            or self.model_export_timeout_seconds <= 0
            or self.model_completion_timeout_seconds <= 0
            or self.progress_evaluation_timeout_seconds <= 0
            or self.experiment_result_timeout_seconds <= 0
        ):
            raise ValueError("Core request timeouts must be positive")

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
            timeout_seconds=self.model_completion_timeout_seconds,
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

    def request_artifact_transfer(
        self,
        job_id: str,
        request: Mapping[str, Any],
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/internal/artifact-preview-jobs/{job_id}/transfers",
            request,
        )

    def get_model_notion_export(self, job_id: str) -> dict[str, Any]:
        """Fetch raw Notion content through a live Job-bound Core capability."""

        return self._request(
            "GET",
            f"/v1/internal/model-notion-jobs/{job_id}/export",
            timeout_seconds=self.model_export_timeout_seconds,
        )

    def get_progress_evaluation_input(self, job_id: str) -> dict[str, Any]:
        return self._request(
            "GET",
            f"/v1/internal/progress-evaluation-jobs/{job_id}/input",
            timeout_seconds=self.progress_evaluation_timeout_seconds,
        )

    def execute_progress_evaluation(self, job_id: str) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/internal/progress-evaluation-jobs/{job_id}/execute",
            timeout_seconds=self.progress_evaluation_timeout_seconds,
        )

    def execute_artifact_semantic_description(self, job_id: str) -> dict[str, Any]:
        """Ask Core to execute a claimed semantic Job through its Agent capability."""

        return self._request(
            "POST",
            f"/v1/internal/artifact-semantic-jobs/{job_id}/execute",
            timeout_seconds=self.progress_evaluation_timeout_seconds,
        )

    def get_experiment_result_input(self, job_id: str) -> dict[str, Any]:
        return self._request(
            "GET",
            f"/v1/internal/experiment-result-jobs/{job_id}/input",
            timeout_seconds=60,
        )

    def finalize_experiment_result(
        self,
        job_id: str,
        result: Mapping[str, Any],
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/internal/experiment-result-jobs/{job_id}/finalize",
            result,
            timeout_seconds=self.experiment_result_timeout_seconds,
        )

    def get_article_build_input(self, job_id: str) -> dict[str, Any]:
        return self._request(
            "GET", f"/v1/internal/article-build-jobs/{job_id}/input", timeout_seconds=60
        )

    def update_article_build_progress(
        self,
        job_id: str,
        progress_percent: int,
        progress_stage: str,
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/v1/internal/article-build-jobs/{job_id}/progress",
            {
                "progress_percent": progress_percent,
                "progress_stage": progress_stage,
            },
            timeout_seconds=30,
        )

    def upload_article_build_output(
        self,
        job_id: str,
        role: str,
        source: Path,
        *,
        filename: str,
        mime_type: str,
        sha256: str,
        size_bytes: int,
    ) -> dict[str, Any]:
        if size_bytes < 1 or source.stat().st_size != size_bytes:
            raise JobAPIError("ARTICLE_OUTPUT_CHANGED", "Article output size changed")
        parsed = urlsplit(
            self.base_url + f"/v1/internal/article-build-jobs/{job_id}/outputs/{role}"
        )
        connection_type = (
            http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        )
        connection = connection_type(parsed.hostname, port=parsed.port, timeout=300)
        target = parsed.path or "/"
        try:
            connection.putrequest("POST", target)
            connection.putheader("Accept", "application/json")
            connection.putheader("Authorization", f"Bearer {self.api_token}")
            connection.putheader("Content-Type", mime_type)
            connection.putheader("Content-Length", str(size_bytes))
            connection.putheader("X-Content-Length", str(size_bytes))
            connection.putheader("X-Content-SHA256", sha256)
            connection.putheader("X-Filename", filename)
            connection.putheader("User-Agent", "mmdash-worker/0.1")
            connection.endheaders()
            sent = 0
            with source.open("rb") as contents:
                while chunk := contents.read(64 * 1024):
                    sent += len(chunk)
                    if sent > size_bytes:
                        raise JobAPIError("ARTICLE_OUTPUT_CHANGED", "Article output changed")
                    connection.send(chunk)
            if sent != size_bytes:
                raise JobAPIError("ARTICLE_OUTPUT_CHANGED", "Article output changed")
            response = connection.getresponse()
            payload = response.read(1024 * 1024)
            if response.status < 200 or response.status >= 300:
                self._raise_transfer_error(response.status, payload)
            decoded = json.loads(payload)
            if not isinstance(decoded, dict):
                raise JobAPIError("INVALID_RESPONSE", "Core returned invalid Article output JSON")
            return decoded
        except OSError as error:
            raise JobAPIError("CORE_UNAVAILABLE", str(error)) from error
        finally:
            connection.close()

    def download_transfer(
        self,
        grant: Mapping[str, Any],
        destination: Path,
        *,
        max_bytes: int,
    ) -> dict[str, Any]:
        method, url, headers = self._validated_transfer(grant, "GET")
        del method
        transfer_url, transfer_headers = self._transfer_target(url, headers)
        request = Request(transfer_url, method="GET", headers=transfer_headers)
        try:
            with urlopen(request, timeout=self.timeout_seconds) as response:
                declared = response.headers.get("Content-Length", "").strip()
                if declared:
                    try:
                        declared_size = int(declared)
                    except ValueError as error:
                        raise JobAPIError(
                            "INVALID_TRANSFER_RESPONSE",
                            "Artifact transfer returned an invalid size",
                        ) from error
                    if declared_size < 0 or declared_size > max_bytes:
                        raise JobAPIError(
                            "ARTIFACT_PREVIEW_INPUT_TOO_LARGE",
                            "Artifact exceeds the Worker preview input limit",
                        )
                total = 0
                with destination.open("wb") as target:
                    while True:
                        chunk = response.read(64 * 1024)
                        if not chunk:
                            break
                        total += len(chunk)
                        if total > max_bytes:
                            raise JobAPIError(
                                "ARTIFACT_PREVIEW_INPUT_TOO_LARGE",
                                "Artifact exceeds the Worker preview input limit",
                            )
                        target.write(chunk)
                return {
                    "size_bytes": total,
                    "content_type": response.headers.get(
                        "Content-Type", "application/octet-stream"
                    ),
                }
        except HTTPError as error:
            self._raise_transfer_error(error.code, error.read())
        except URLError as error:
            raise JobAPIError("CORE_UNAVAILABLE", str(error.reason)) from error
        raise AssertionError("transfer error handler must raise")

    def upload_transfer(
        self,
        grant: Mapping[str, Any],
        source: Path,
        *,
        size_bytes: int,
    ) -> str:
        method, url, headers = self._validated_transfer(grant, "PUT")
        del method
        transfer_url, transfer_headers = self._transfer_target(url, headers)
        parsed = urlsplit(transfer_url)
        if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username:
            raise JobAPIError("INVALID_TRANSFER_GRANT", "Artifact transfer URL is invalid")
        connection_type = (
            http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        )
        port = parsed.port
        connection = connection_type(
            parsed.hostname,
            port=port,
            timeout=self.timeout_seconds,
        )
        target = parsed.path or "/"
        if parsed.query:
            target += "?" + parsed.query
        try:
            connection.putrequest("PUT", target, skip_host="Host" in transfer_headers)
            for name, value in transfer_headers.items():
                connection.putheader(name, value)
            connection.putheader("Content-Length", str(size_bytes))
            connection.endheaders()
            sent = 0
            with source.open("rb") as contents:
                while True:
                    chunk = contents.read(64 * 1024)
                    if not chunk:
                        break
                    sent += len(chunk)
                    if sent > size_bytes:
                        raise JobAPIError(
                            "ARTIFACT_PREVIEW_OUTPUT_CHANGED",
                            "Preview output changed during transfer",
                        )
                    connection.send(chunk)
            if sent != size_bytes:
                raise JobAPIError(
                    "ARTIFACT_PREVIEW_OUTPUT_CHANGED",
                    "Preview output changed during transfer",
                )
            response = connection.getresponse()
            payload = response.read(4096)
            if response.status < 200 or response.status >= 300:
                self._raise_transfer_error(response.status, payload)
            etag = response.getheader("ETag", "").strip().strip('"')
            if not etag or len(etag) > 1024:
                raise JobAPIError(
                    "INVALID_TRANSFER_RESPONSE",
                    "Artifact transfer did not return a valid ETag",
                    response.status,
                )
            return etag
        except OSError as error:
            raise JobAPIError("CORE_UNAVAILABLE", str(error)) from error
        finally:
            connection.close()

    def _request(
        self,
        method: str,
        path: str,
        body: Mapping[str, Any] | None = None,
        *,
        expect_empty: bool = False,
        timeout_seconds: float | None = None,
    ) -> dict[str, Any]:
        encoded = (
            json.dumps(body, separators=(",", ":")).encode("utf-8") if body is not None else None
        )
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
            with urlopen(
                request,
                timeout=self.timeout_seconds if timeout_seconds is None else timeout_seconds,
            ) as response:
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

    @staticmethod
    def _validated_transfer(
        grant: Mapping[str, Any],
        expected_method: str,
    ) -> tuple[str, str, dict[str, str]]:
        method = grant.get("method")
        url = grant.get("url")
        raw_headers = grant.get("headers")
        if (
            method != expected_method
            or not isinstance(url, str)
            or not url
            or not isinstance(raw_headers, Mapping)
        ):
            raise JobAPIError("INVALID_TRANSFER_GRANT", "Artifact transfer grant is invalid")
        headers: dict[str, str] = {}
        for name, value in raw_headers.items():
            if not isinstance(name, str) or not isinstance(value, str):
                raise JobAPIError(
                    "INVALID_TRANSFER_GRANT",
                    "Artifact transfer headers are invalid",
                )
            headers[name] = value
        return method, url, headers

    @staticmethod
    def _validated_transfer_origin_override(value: str) -> SplitResult | None:
        value = value.strip()
        if not value:
            return None
        parsed = urlsplit(value)
        if (
            parsed.scheme not in {"http", "https"}
            or not parsed.hostname
            or parsed.username
            or parsed.password
            or parsed.path not in {"", "/"}
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Worker transfer origin override must be an HTTP(S) origin")
        return parsed

    def _transfer_target(self, url: str, headers: Mapping[str, str]) -> tuple[str, dict[str, str]]:
        original = urlsplit(url)
        if (
            original.scheme not in {"http", "https"}
            or not original.hostname
            or original.username
            or original.password
            or original.fragment
        ):
            raise JobAPIError("INVALID_TRANSFER_GRANT", "Artifact transfer URL is invalid")
        result_headers = dict(headers)
        override = self.transfer_origin_override
        if override is None:
            return url, result_headers
        if original.hostname not in {"localhost", "127.0.0.1"}:
            raise JobAPIError(
                "INVALID_TRANSFER_GRANT",
                "Worker transfer origin override only accepts local signed URLs",
            )
        if original.scheme != override.scheme:
            raise JobAPIError(
                "INVALID_TRANSFER_GRANT",
                "Worker transfer origin override must preserve the signed URL scheme",
            )
        # Connect to the container-visible origin while preserving the signed
        # Host header. S3 includes Host in its signature, so changing both the
        # destination and Host would invalidate an otherwise valid grant.
        result_headers["Host"] = original.netloc
        return (
            urlunsplit(
                (
                    override.scheme,
                    override.netloc,
                    original.path,
                    original.query,
                    "",
                )
            ),
            result_headers,
        )

    @staticmethod
    def _raise_transfer_error(status: int, payload: bytes) -> None:
        try:
            decoded = json.loads(payload)
        except (json.JSONDecodeError, UnicodeDecodeError):
            decoded = {}
        raise JobAPIError(
            str(decoded.get("code", "ARTIFACT_TRANSFER_FAILED")),
            str(decoded.get("message", f"Artifact transfer returned HTTP {status}")),
            status,
        )
