import json
from pathlib import Path

import pytest

from main import PROTOCOL_VERSION, handle_client


class FakeWebSocket:
    def __init__(self, messages):
        self._messages = iter(messages)
        self.sent = []

    def __aiter__(self):
        return self

    async def __anext__(self):
        try:
            return next(self._messages)
        except StopIteration as exc:
            raise StopAsyncIteration from exc

    async def send(self, message):
        self.sent.append(message)

    async def wait_closed(self):
        return


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest.mark.anyio
async def test_shell_run_uses_structured_success_response(tmp_path: Path):
    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "shell.run",
                    "params": {
                        "command": "echo hello",
                        "repo_path": str(tmp_path),
                        "cwd": str(tmp_path),
                    },
                }
            )
        ]
    )

    await handle_client(websocket)

    response = json.loads(websocket.sent[0])
    assert response["ok"] is True
    assert response["protocol_version"] == PROTOCOL_VERSION
    assert response["action"] == "shell.run"
    assert response["data"]["returncode"] == 0
    assert response["data"]["stdout"] == "hello\n"
    assert response["data"]["stderr"] == ""
    assert response["data"]["task"]["status"] == "succeeded"


@pytest.mark.anyio
async def test_legacy_shell_action_is_still_supported(tmp_path: Path):
    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "shell",
                    "params": {
                        "command": "echo hello",
                        "repo_path": str(tmp_path),
                        "cwd": str(tmp_path),
                    },
                }
            )
        ]
    )

    await handle_client(websocket)

    response = json.loads(websocket.sent[0])
    assert response["ok"] is True
    assert response["action"] == "shell.run"
    assert response["data"]["stdout"] == "hello\n"


@pytest.mark.anyio
async def test_fs_read_and_write_are_scoped_to_repo(tmp_path: Path):
    repo_path = tmp_path / "repo"
    repo_path.mkdir()
    file_path = repo_path / "nested" / "analysis.md"

    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "fs.write",
                    "params": {
                        "repo_path": str(repo_path),
                        "path": str(file_path),
                        "content": "hello world",
                    },
                }
            ),
            json.dumps(
                {
                    "request_id": "2",
                    "action": "fs.read",
                    "params": {
                        "repo_path": str(repo_path),
                        "path": str(file_path),
                    },
                }
            ),
        ]
    )

    await handle_client(websocket)

    write_response = json.loads(websocket.sent[0])
    read_response = json.loads(websocket.sent[1])
    assert write_response["ok"] is True
    assert read_response["ok"] is True
    assert read_response["data"]["content"] == "hello world"


@pytest.mark.anyio
async def test_fs_read_rejects_out_of_repo_paths(tmp_path: Path):
    repo_path = tmp_path / "repo"
    repo_path.mkdir()
    outside_path = tmp_path / "outside.txt"
    outside_path.write_text("hello", encoding="utf-8")

    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "fs.read",
                    "params": {
                        "repo_path": str(repo_path),
                        "path": str(outside_path),
                    },
                }
            )
        ]
    )

    await handle_client(websocket)

    response = json.loads(websocket.sent[0])
    assert response["ok"] is False
    assert response["error"]["code"] == "path_out_of_bounds"


@pytest.mark.anyio
async def test_unknown_action_returns_structured_error():
    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "unknown.action",
                    "params": {},
                }
            )
        ]
    )

    await handle_client(websocket)

    response = json.loads(websocket.sent[0])
    assert response["ok"] is False
    assert response["error"]["code"] == "unknown_action"


@pytest.mark.anyio
async def test_shell_run_rejects_cwd_outside_repo(tmp_path: Path):
    repo_path = tmp_path / "repo"
    repo_path.mkdir()
    outside_dir = tmp_path / "outside"
    outside_dir.mkdir()

    websocket = FakeWebSocket(
        [
            json.dumps(
                {
                    "request_id": "1",
                    "action": "shell.run",
                    "params": {
                        "command": "pwd",
                        "repo_path": str(repo_path),
                        "cwd": str(outside_dir),
                    },
                }
            )
        ]
    )

    await handle_client(websocket)

    response = json.loads(websocket.sent[0])
    assert response["ok"] is False
    assert response["error"]["code"] == "path_out_of_bounds"
