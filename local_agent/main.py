import asyncio
import json
import os
import platform
import shutil
import sys
import uuid
from contextlib import suppress
from datetime import datetime
from pathlib import Path
from typing import Any

import psutil
import websockets

HOST = "127.0.0.1"
PORT = 8765
PROTOCOL_VERSION = 2
MAX_OUTPUT_CHARS = 20000
MAX_FILE_BYTES = 512 * 1024
DEFAULT_SHELL_TIMEOUT_SECONDS = 300
DEFAULT_EXPERIMENT_TIMEOUT_SECONDS = 120

connected_clients = set()
server_started_at = datetime.now()
active_tasks: dict[str, dict[str, Any]] = {}
repo_locks: dict[str, asyncio.Lock] = {}


class AgentError(Exception):
    def __init__(
        self,
        code: str,
        message: str,
        *,
        retryable: bool = False,
        details: dict[str, Any] | None = None,
    ):
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable
        self.details = details or {}


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def log_event(event: str, **fields: Any) -> None:
    payload = {"ts": now_iso(), "event": event, **fields}
    print(json.dumps(payload, ensure_ascii=False), flush=True)


def ok_response(request_id: str | None, action: str, data: dict[str, Any]) -> dict[str, Any]:
    return {
        "request_id": request_id,
        "action": action,
        "ok": True,
        "protocol_version": PROTOCOL_VERSION,
        "data": data,
    }


def error_response(request_id: str | None, action: str, error: AgentError) -> dict[str, Any]:
    return {
        "request_id": request_id,
        "action": action,
        "ok": False,
        "protocol_version": PROTOCOL_VERSION,
        "error": {
            "code": error.code,
            "message": error.message,
            "retryable": error.retryable,
            "details": error.details,
        },
    }


def normalize_action(action: str | None) -> str:
    if action == "shell":
        return "shell.run"
    if action == "run_experiment":
        return "experiment.run"
    return action or ""


def normalize_params(data: dict[str, Any]) -> dict[str, Any]:
    params = data.get("params")
    if isinstance(params, dict):
        return params
    if params is None:
        return {}
    raise AgentError(
        "invalid_params",
        "params must be an object",
        details={"received_type": type(params).__name__},
    )


def trim_output(value: str, *, limit: int = MAX_OUTPUT_CHARS) -> str:
    if len(value) <= limit:
        return value
    return value[:limit] + "\n...[truncated]"


def ensure_existing_dir(path_value: str | None, field: str) -> Path | None:
    if path_value is None:
        return None
    path = Path(path_value).expanduser().resolve()
    if not path.exists():
        raise AgentError("path_not_found", f"{field} does not exist", details={field: str(path)})
    if not path.is_dir():
        raise AgentError("invalid_path", f"{field} must be a directory", details={field: str(path)})
    return path


def ensure_existing_file(path_value: str | None, field: str) -> Path:
    if not path_value:
        raise AgentError("missing_param", f"{field} is required", details={"field": field})
    path = Path(path_value).expanduser().resolve()
    if not path.exists():
        raise AgentError("path_not_found", f"{field} does not exist", details={field: str(path)})
    if not path.is_file():
        raise AgentError("invalid_path", f"{field} must be a file", details={field: str(path)})
    return path


def ensure_repo_scoped_dir(path_value: str | None, repo_path: Path, field: str) -> Path:
    if path_value is None:
        return repo_path
    path = ensure_existing_dir(path_value, field)
    assert path is not None
    return ensure_child_path(str(path), repo_path)


def ensure_child_path(path_value: str, base_dir: Path) -> Path:
    path = Path(path_value).expanduser().resolve()
    try:
        path.relative_to(base_dir)
    except ValueError as exc:
        raise AgentError(
            "path_out_of_bounds",
            "path must stay within repo_path",
            details={"path": str(path), "repo_path": str(base_dir)},
        ) from exc
    return path


def get_repo_lock(repo_path: Path) -> asyncio.Lock:
    key = str(repo_path)
    lock = repo_locks.get(key)
    if lock is None:
        lock = asyncio.Lock()
        repo_locks[key] = lock
    return lock


def start_task(action: str, params: dict[str, Any]) -> dict[str, Any]:
    task = {
        "task_id": uuid.uuid4().hex,
        "action": action,
        "status": "running",
        "started_at": now_iso(),
        "finished_at": None,
        "returncode": None,
        "stdout_tail": "",
        "stderr_tail": "",
        "params": params,
    }
    active_tasks[task["task_id"]] = task
    return task


def finish_task(
    task: dict[str, Any],
    *,
    status: str,
    returncode: int | None,
    stdout: str = "",
    stderr: str = "",
) -> None:
    task["status"] = status
    task["finished_at"] = now_iso()
    task["returncode"] = returncode
    task["stdout_tail"] = trim_output(stdout)
    task["stderr_tail"] = trim_output(stderr)


async def register_client(websocket):
    connected_clients.add(websocket)
    log_event("client_connected", active_connections=len(connected_clients))
    try:
        await websocket.wait_closed()
    finally:
        connected_clients.discard(websocket)
        log_event("client_disconnected", active_connections=len(connected_clients))


async def handle_detect_env() -> dict[str, Any]:
    env_info = {
        "python_version": sys.version,
        "python_path": sys.executable,
        "platform": platform.platform(),
        "cpu_count": psutil.cpu_count(),
        "memory_gb": round(psutil.virtual_memory().total / (1024**3), 2),
        "checks": {},
    }

    conda_path = shutil.which("conda")
    env_info["conda_available"] = conda_path is not None
    env_info["checks"]["conda"] = {"available": env_info["conda_available"], "error": None}
    if conda_path:
        try:
            proc = await asyncio.create_subprocess_exec(
                conda_path,
                "env",
                "list",
                "--json",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=10)
            if proc.returncode == 0:
                env_info["conda_envs"] = json.loads(stdout.decode("utf-8", errors="replace")).get(
                    "envs", []
                )
            else:
                env_info["conda_envs"] = []
                env_info["checks"]["conda"]["error"] = "conda env list failed"
        except asyncio.TimeoutError:
            proc.kill()
            env_info["conda_envs"] = []
            env_info["checks"]["conda"]["error"] = "conda env list timed out"
        except Exception as exc:
            env_info["conda_envs"] = []
            env_info["checks"]["conda"]["error"] = str(exc)
    else:
        env_info["conda_envs"] = []

    gcc_path = shutil.which("gcc")
    env_info["gcc_available"] = gcc_path is not None
    env_info["checks"]["gcc"] = {"available": env_info["gcc_available"], "error": None}

    git_path = shutil.which("git")
    env_info["git_available"] = git_path is not None
    env_info["checks"]["git"] = {"available": env_info["git_available"], "error": None}
    return env_info


async def run_subprocess(
    args: list[str] | str,
    *,
    shell: bool,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
    timeout_seconds: int,
) -> tuple[int, str, str]:
    if shell:
        proc = await asyncio.create_subprocess_shell(
            args,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=str(cwd) if cwd else None,
            env=env,
        )
    else:
        proc = await asyncio.create_subprocess_exec(
            *args,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=str(cwd) if cwd else None,
            env=env,
        )
    try:
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout_seconds)
    except asyncio.TimeoutError as exc:
        proc.kill()
        with suppress(Exception):
            await proc.communicate()
        raise AgentError(
            "timeout",
            f"command timed out after {timeout_seconds}s",
            retryable=True,
            details={"timeout_seconds": timeout_seconds},
        ) from exc

    return (
        proc.returncode,
        stdout.decode("utf-8", errors="replace"),
        stderr.decode("utf-8", errors="replace"),
    )


async def handle_shell_run(params: dict[str, Any]) -> dict[str, Any]:
    command = params.get("command")
    if not command or not isinstance(command, str):
        raise AgentError("missing_param", "command is required", details={"field": "command"})

    repo_path = ensure_existing_dir(params.get("repo_path"), "repo_path")
    if repo_path is None:
        raise AgentError("missing_param", "repo_path is required", details={"field": "repo_path"})
    cwd = ensure_repo_scoped_dir(params.get("cwd"), repo_path, "cwd")
    timeout_seconds = int(params.get("timeout_seconds") or DEFAULT_SHELL_TIMEOUT_SECONDS)
    task = start_task(
        "shell.run",
        {"command": command, "repo_path": str(repo_path), "cwd": str(cwd)},
    )

    try:
        returncode, stdout, stderr = await run_subprocess(
            command,
            shell=True,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
        )
        finish_task(
            task,
            status="succeeded" if returncode == 0 else "failed",
            returncode=returncode,
            stdout=stdout,
            stderr=stderr,
        )
        return {
            "task": task,
            "returncode": returncode,
            "stdout": trim_output(stdout),
            "stderr": trim_output(stderr),
        }
    except AgentError as error:
        finish_task(task, status="failed", returncode=-1, stderr=error.message)
        raise
    finally:
        log_event(
            "shell_run_completed",
            task_id=task["task_id"],
            status=task["status"],
            returncode=task["returncode"],
        )


def build_result_dir(git_repo_path: Path, solver_name: str) -> Path:
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    return git_repo_path / "results" / f"{timestamp}_{solver_name}"


async def run_solver_once(
    solver_path: Path,
    *,
    cwd: Path,
    env: dict[str, str] | None,
    timeout_seconds: int,
) -> dict[str, Any]:
    returncode, stdout, stderr = await run_subprocess(
        [sys.executable, str(solver_path)],
        shell=False,
        cwd=cwd,
        env=env,
        timeout_seconds=timeout_seconds,
    )
    return {
        "returncode": returncode,
        "stdout": trim_output(stdout, limit=2000),
        "stderr": trim_output(stderr, limit=2000),
    }


async def handle_experiment_run(params: dict[str, Any]) -> dict[str, Any]:
    solver_path = ensure_existing_file(params.get("solver_path"), "solver_path")
    git_repo_path = ensure_existing_dir(params.get("git_repo_path", "."), "git_repo_path")
    param_grid = params.get("param_grid") or {}
    if not isinstance(param_grid, dict):
        raise AgentError("invalid_params", "param_grid must be an object")

    solver_name = solver_path.stem
    timeout_seconds = int(params.get("timeout_seconds") or DEFAULT_EXPERIMENT_TIMEOUT_SECONDS)
    result_dir = build_result_dir(git_repo_path, solver_name)
    result_dir.mkdir(parents=True, exist_ok=True)
    (result_dir / "fig").mkdir(exist_ok=True)

    task = start_task(
        "experiment.run",
        {
            "solver_path": str(solver_path),
            "git_repo_path": str(git_repo_path),
            "param_grid_keys": sorted(param_grid.keys()),
        },
    )
    repo_lock = get_repo_lock(git_repo_path)
    results = []

    async with repo_lock:
        try:
            if param_grid:
                from itertools import product

                keys = list(param_grid.keys())
                values = list(param_grid.values())
                for combo in product(*values):
                    run_params = dict(zip(keys, combo))
                    env = os.environ.copy()
                    for key, value in run_params.items():
                        env[key] = str(value)
                    result = await run_solver_once(
                        solver_path,
                        cwd=solver_path.parent,
                        env=env,
                        timeout_seconds=timeout_seconds,
                    )
                    result["params"] = run_params
                    results.append(result)
            else:
                results.append(
                    await run_solver_once(
                        solver_path,
                        cwd=solver_path.parent,
                        env=None,
                        timeout_seconds=timeout_seconds,
                    )
                )
        except AgentError as error:
            finish_task(task, status="failed", returncode=-1, stderr=error.message)
            raise

        log_path = result_dir / "log.txt"
        log_path.write_text(
            "\n".join(json.dumps(item, ensure_ascii=False) for item in results) + ("\n" if results else ""),
            encoding="utf-8",
        )

        snapshot_path = result_dir / "params_snapshot.json"
        snapshot_path.write_text(json.dumps(param_grid, ensure_ascii=False, indent=2), encoding="utf-8")

        analysis_path = result_dir / "analysis.md"
        with analysis_path.open("w", encoding="utf-8") as handle:
            handle.write("# 实验分析\n\n## 参数\n\n```json\n")
            handle.write(json.dumps(param_grid, ensure_ascii=False, indent=2))
            handle.write("\n```\n\n## 结果摘要\n\n")
            for result in results:
                handle.write(
                    f"- 参数: {result.get('params', {})} -> 返回码: {result['returncode']}\n"
                )

        last_result = results[-1] if results else {}
        finish_task(
            task,
            status="succeeded"
            if results and all(item["returncode"] == 0 for item in results)
            else "failed",
            returncode=last_result.get("returncode"),
            stdout=last_result.get("stdout", ""),
            stderr=last_result.get("stderr", ""),
        )

    log_event(
        "experiment_run_completed",
        task_id=task["task_id"],
        status=task["status"],
        result_dir=str(result_dir),
    )
    return {
        "task": task,
        "status": "success" if task["status"] == "succeeded" else "error",
        "result_dir": str(result_dir),
        "results": results,
    }


async def handle_fs_read(params: dict[str, Any]) -> dict[str, Any]:
    repo_path = ensure_existing_dir(params.get("repo_path"), "repo_path")
    path = ensure_child_path(params.get("path", ""), repo_path)
    if not path.exists():
        raise AgentError("path_not_found", "file does not exist", details={"path": str(path)})
    if not path.is_file():
        raise AgentError("invalid_path", "path must be a file", details={"path": str(path)})
    content = path.read_text(encoding="utf-8")
    if len(content.encode("utf-8")) > MAX_FILE_BYTES:
        raise AgentError("file_too_large", "file exceeds read limit", details={"path": str(path)})
    return {"path": str(path), "content": content}


async def handle_fs_write(params: dict[str, Any]) -> dict[str, Any]:
    repo_path = ensure_existing_dir(params.get("repo_path"), "repo_path")
    path = ensure_child_path(params.get("path", ""), repo_path)
    content = params.get("content")
    if not isinstance(content, str):
        raise AgentError("missing_param", "content is required", details={"field": "content"})
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    return {"path": str(path), "bytes_written": len(content.encode("utf-8"))}


async def handle_fs_list(params: dict[str, Any]) -> dict[str, Any]:
    repo_path = ensure_existing_dir(params.get("repo_path"), "repo_path")
    dir_path = ensure_child_path(params.get("path", str(repo_path)), repo_path)
    if not dir_path.exists():
        raise AgentError("path_not_found", "directory does not exist", details={"path": str(dir_path)})
    if not dir_path.is_dir():
        raise AgentError("invalid_path", "path must be a directory", details={"path": str(dir_path)})

    files = []
    for file_path in sorted(path for path in dir_path.rglob("*") if path.is_file())[:200]:
        files.append(str(file_path))
    return {"path": str(dir_path), "files": files}


async def handle_git_add_commit_push(params: dict[str, Any]) -> dict[str, Any]:
    repo_path = ensure_existing_dir(params.get("repo_path"), "repo_path")
    commit_message = params.get("commit_message") or "experiment results"
    if not isinstance(commit_message, str):
        raise AgentError("invalid_params", "commit_message must be a string")

    task = start_task(
        "git.add_commit_push",
        {"repo_path": str(repo_path), "commit_message": commit_message},
    )
    repo_lock = get_repo_lock(repo_path)
    outputs = []
    returncode = 0

    async with repo_lock:
        try:
            commands = [
                ("git add .", ["git", "add", "."]),
                (f'git commit -m "{commit_message}"', ["git", "commit", "-m", commit_message]),
                ("git push", ["git", "push"]),
            ]
            for label, args in commands:
                step_returncode, stdout, stderr = await run_subprocess(
                    args,
                    shell=False,
                    cwd=repo_path,
                    timeout_seconds=DEFAULT_SHELL_TIMEOUT_SECONDS,
                )
                outputs.append(
                    {
                        "command": label,
                        "returncode": step_returncode,
                        "stdout": trim_output(stdout, limit=4000),
                        "stderr": trim_output(stderr, limit=4000),
                    }
                )
                returncode = step_returncode
                if label.startswith("git commit") and step_returncode != 0 and "nothing to commit" in stderr:
                    returncode = 0
                    outputs[-1]["returncode"] = 0
                    continue
                if step_returncode != 0:
                    break
        except AgentError as error:
            finish_task(task, status="failed", returncode=-1, stderr=error.message)
            raise

    combined_stdout = "\n".join(item["stdout"] for item in outputs if item["stdout"])
    combined_stderr = "\n".join(item["stderr"] for item in outputs if item["stderr"])
    finish_task(
        task,
        status="succeeded" if returncode == 0 else "failed",
        returncode=returncode,
        stdout=combined_stdout,
        stderr=combined_stderr,
    )
    return {"task": task, "steps": outputs, "returncode": returncode}


async def handle_agent_info() -> dict[str, Any]:
    return {
        "protocol_version": PROTOCOL_VERSION,
        "host": HOST,
        "port": PORT,
        "uptime_seconds": int((datetime.now() - server_started_at).total_seconds()),
        "active_connections": len(connected_clients),
        "active_tasks": len([task for task in active_tasks.values() if task["status"] == "running"]),
    }


async def handle_task_get(params: dict[str, Any]) -> dict[str, Any]:
    task_id = params.get("task_id")
    if not task_id or not isinstance(task_id, str):
        raise AgentError("missing_param", "task_id is required", details={"field": "task_id"})
    task = active_tasks.get(task_id)
    if task is None:
        raise AgentError("task_not_found", "task not found", details={"task_id": task_id})
    return {"task": task}


async def dispatch(action: str, params: dict[str, Any]) -> dict[str, Any]:
    if action == "detect_env":
        return await handle_detect_env()
    if action == "ping":
        return {"status": "pong", **(await handle_agent_info())}
    if action == "agent.info":
        return await handle_agent_info()
    if action == "task.get":
        return await handle_task_get(params)
    if action == "shell.run":
        return await handle_shell_run(params)
    if action == "experiment.run":
        return await handle_experiment_run(params)
    if action == "fs.read":
        return await handle_fs_read(params)
    if action == "fs.write":
        return await handle_fs_write(params)
    if action == "fs.list":
        return await handle_fs_list(params)
    if action == "git.add_commit_push":
        return await handle_git_add_commit_push(params)
    raise AgentError("unknown_action", f"Unknown action: {action}", details={"action": action})


async def handle_client(websocket):
    registration_task = asyncio.create_task(register_client(websocket))
    try:
        async for message in websocket:
            request_id = None
            raw_action = ""
            try:
                data = json.loads(message)
                request_id = data.get("request_id")
                raw_action = data.get("action") or ""
                action = normalize_action(raw_action)
                params = normalize_params(data)
                log_event("request_received", request_id=request_id, action=action)
                response = ok_response(request_id, action, await dispatch(action, params))
                await websocket.send(json.dumps(response))
            except json.JSONDecodeError:
                error = AgentError("invalid_json", "Invalid JSON payload")
                await websocket.send(json.dumps(error_response(request_id, raw_action or "unknown", error)))
            except AgentError as error:
                log_event(
                    "request_failed",
                    request_id=request_id,
                    action=normalize_action(raw_action),
                    code=error.code,
                )
                try:
                    await websocket.send(
                        json.dumps(error_response(request_id, normalize_action(raw_action), error))
                    )
                except websockets.exceptions.ConnectionClosed:
                    break
            except websockets.exceptions.ConnectionClosed:
                break
            except Exception as exc:
                log_event(
                    "request_failed",
                    request_id=request_id,
                    action=normalize_action(raw_action),
                    code="internal_error",
                    detail=str(exc),
                )
                try:
                    await websocket.send(
                        json.dumps(
                            error_response(
                                request_id,
                                normalize_action(raw_action),
                                AgentError("internal_error", "Internal agent error", retryable=True),
                            )
                        )
                    )
                except websockets.exceptions.ConnectionClosed:
                    break
    finally:
        registration_task.cancel()
        with suppress(asyncio.CancelledError):
            await registration_task


async def main():
    log_event(
        "agent_starting",
        host=HOST,
        port=PORT,
        protocol_version=PROTOCOL_VERSION,
        python=sys.version.split()[0],
    )
    async with websockets.serve(handle_client, HOST, PORT, ping_interval=20, ping_timeout=20):
        await asyncio.Future()


if __name__ == "__main__":
    asyncio.run(main())
