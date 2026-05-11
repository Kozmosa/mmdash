import asyncio
import json
import os
import platform
import shutil
import subprocess
import sys
from datetime import datetime

import psutil
import websockets

HOST = "127.0.0.1"
PORT = 8765

connected_clients = set()


async def register_client(websocket):
    connected_clients.add(websocket)
    try:
        await websocket.wait_closed()
    finally:
        connected_clients.discard(websocket)


async def broadcast(message: dict):
    if connected_clients:
        await asyncio.gather(
            *[client.send(json.dumps(message)) for client in connected_clients],
            return_exceptions=True,
        )


async def handle_detect_env():
    env_info = {
        "python_version": sys.version,
        "python_path": sys.executable,
        "platform": platform.platform(),
        "cpu_count": psutil.cpu_count(),
        "memory_gb": round(psutil.virtual_memory().total / (1024**3), 2),
    }
    # Check conda
    conda_path = shutil.which("conda")
    env_info["conda_available"] = conda_path is not None
    if conda_path:
        try:
            proc = await asyncio.create_subprocess_exec(
                conda_path, "env", "list", "--json",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10)
            if proc.returncode == 0:
                env_info["conda_envs"] = json.loads(stdout.decode("utf-8", errors="replace")).get("envs", [])
        except asyncio.TimeoutError:
            proc.kill()
            env_info["conda_envs"] = []
        except Exception:
            env_info["conda_envs"] = []
    # Check gcc
    gcc_path = shutil.which("gcc")
    env_info["gcc_available"] = gcc_path is not None
    # Check git
    git_path = shutil.which("git")
    env_info["git_available"] = git_path is not None
    return env_info


async def handle_shell_command(command: str, cwd: str = None):
    try:
        proc = await asyncio.create_subprocess_shell(
            command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=300)
        return {
            "returncode": proc.returncode,
            "stdout": stdout.decode("utf-8", errors="replace"),
            "stderr": stderr.decode("utf-8", errors="replace"),
        }
    except asyncio.TimeoutError:
        proc.kill()
        return {"returncode": -1, "stdout": "", "stderr": "Command timed out after 300s"}
    except Exception as e:
        return {"returncode": -1, "stdout": "", "stderr": str(e)}


async def handle_experiment(params: dict):
    """Run experiment with given parameters."""
    solver_path = params.get("solver_path")
    param_grid = params.get("param_grid", {})
    git_repo_path = params.get("git_repo_path", ".")

    if not solver_path or not os.path.exists(solver_path):
        return {"status": "error", "message": "Solver file not found"}

    solver_name = os.path.splitext(os.path.basename(solver_path))[0]
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    result_dir = os.path.join(git_repo_path, "results", f"{timestamp}_{solver_name}")
    os.makedirs(os.path.join(result_dir, "fig"), exist_ok=True)

    results = []
    # Simple grid search implementation
    if param_grid:
        keys = list(param_grid.keys())
        values = list(param_grid.values())
        from itertools import product
        for combo in product(*values):
            run_params = dict(zip(keys, combo))
            env = os.environ.copy()
            for k, v in run_params.items():
                env[k] = str(v)
            try:
                proc = await asyncio.create_subprocess_exec(
                    sys.executable, solver_path,
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                    cwd=os.path.dirname(solver_path),
                    env=env,
                )
                stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=120)
                results.append({
                    "params": run_params,
                    "returncode": proc.returncode,
                    "stdout": stdout.decode("utf-8", errors="replace")[:2000],
                    "stderr": stderr.decode("utf-8", errors="replace")[:2000],
                })
            except asyncio.TimeoutError:
                proc.kill()
                results.append({"params": run_params, "returncode": -1, "error": "timeout"})
    else:
        try:
            proc = await asyncio.create_subprocess_exec(
                sys.executable, solver_path,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=os.path.dirname(solver_path),
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=120)
            results.append({
                "returncode": proc.returncode,
                "stdout": stdout.decode("utf-8", errors="replace")[:2000],
                "stderr": stderr.decode("utf-8", errors="replace")[:2000],
            })
        except asyncio.TimeoutError:
            proc.kill()
            results.append({"returncode": -1, "error": "timeout"})

    # Write log
    log_path = os.path.join(result_dir, "log.txt")
    with open(log_path, "w") as f:
        for r in results:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")

    # Write params snapshot
    snapshot_path = os.path.join(result_dir, "params_snapshot.json")
    with open(snapshot_path, "w") as f:
        json.dump(param_grid, f, ensure_ascii=False, indent=2)

    # Write analysis stub
    analysis_path = os.path.join(result_dir, "analysis.md")
    with open(analysis_path, "w") as f:
        f.write(f"# 实验分析\n\n## 参数\n\n```json\n{json.dumps(param_grid, ensure_ascii=False, indent=2)}\n```\n\n## 结果摘要\n\n")
        for r in results:
            f.write(f"- 参数: {r.get('params', {})} -> 返回码: {r['returncode']}\n")

    return {
        "status": "success",
        "result_dir": result_dir,
        "results": results,
    }


async def handle_client(websocket):
    # Start register_client as a background task so it doesn't block message handling
    registration_task = asyncio.create_task(register_client(websocket))
    try:
        async for message in websocket:
            try:
                data = json.loads(message)
                action = data.get("action")
                request_id = data.get("request_id")
                response = {"request_id": request_id, "action": action}

                if action == "detect_env":
                    response["data"] = await handle_detect_env()
                elif action == "shell":
                    response["data"] = await handle_shell_command(
                        data.get("command", ""), data.get("cwd")
                    )
                elif action == "run_experiment":
                    response["data"] = await handle_experiment(data.get("params", {}))
                elif action == "ping":
                    response["data"] = {"status": "pong"}
                else:
                    response["error"] = f"Unknown action: {action}"

                await websocket.send(json.dumps(response))
            except json.JSONDecodeError:
                try:
                    await websocket.send(json.dumps({"error": "Invalid JSON"}))
                except websockets.exceptions.ConnectionClosed:
                    pass
            except websockets.exceptions.ConnectionClosed:
                break
            except Exception as e:
                try:
                    await websocket.send(json.dumps({"error": str(e)}))
                except websockets.exceptions.ConnectionClosed:
                    pass
    finally:
        registration_task.cancel()
        try:
            await registration_task
        except asyncio.CancelledError:
            pass


async def main():
    print(f"Local Agent starting on ws://{HOST}:{PORT}")
    async with websockets.serve(handle_client, HOST, PORT):
        await asyncio.Future()  # run forever


if __name__ == "__main__":
    asyncio.run(main())
