#!/usr/bin/env bash
set -uo pipefail
# Note: NOT using `set -e` — we handle failures explicitly per step.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$ROOT_DIR/logs"
FRONTEND_MODE="prod"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dev)
            FRONTEND_MODE="dev"
            shift
            ;;
        *)
            echo "错误: 未知参数 $1"
            echo "用法: ./scripts/start-all.sh [--dev]"
            exit 1
            ;;
    esac
done

mkdir -p "$LOG_DIR"

PIDS=()
SERVICES=()
CWD_VARS=()

log_file() {
    echo "$LOG_DIR/$1.log"
}

wait_for_port() {
    local port=$1
    local name=$2
    local attempts=${3:-30}
    local i
    for ((i=1; i<=attempts; i++)); do
        # Use ss first (more reliable), fall back to lsof
        if ss -tlnp "sport = :$port" 2>/dev/null | grep -q ":$port"; then
            return 0
        fi
        if lsof -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.2
    done
    echo "错误: $name 未在预期时间内监听端口 $port"
    return 1
}

wait_for_http() {
    local url=$1
    local name=$2
    local attempts=${3:-60}
    local i
    for ((i=1; i<=attempts; i++)); do
        if curl -sS -I --max-time 2 "$url" >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.2
    done
    echo "错误: $name 未在预期时间内通过 HTTP 就绪检查 ($url)"
    return 1
}

wait_for_local_agent_ready() {
    local attempts=${1:-30}
    local i
    for ((i=1; i<=attempts; i++)); do
        if python - <<'PY' >/dev/null 2>&1
import asyncio
import json
import websockets

async def main():
    async with websockets.connect("ws://127.0.0.1:8765") as ws:
        await ws.send(json.dumps({"request_id": "ready-check", "action": "agent.info", "params": {}}))
        raw = await asyncio.wait_for(ws.recv(), timeout=2)
        data = json.loads(raw)
        assert data.get("ok") is True

asyncio.run(main())
PY
        then
            return 0
        fi
        sleep 0.2
    done
    echo "错误: Local Agent 未在预期时间内通过协议就绪检查"
    return 1
}

kill_port() {
    local port=$1
    local pname=$2
    local pids
    # Use ss first (more reliable), fall back to lsof
    pids=$(ss -tlnp "sport = :$port" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | sort -u || true)
    if [ -z "$pids" ]; then
        pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
    fi
    if [ -n "$pids" ]; then
        echo "  → 端口 $port 被占用，正在停止旧 $pname 进程..."
        echo "$pids" | xargs kill -9 2>/dev/null || true
        sleep 1
    fi
}

cleanup() {
    echo ""
    echo "========================================"
    echo "  正在关闭所有服务..."
    echo "========================================"

    for i in "${!PIDS[@]}"; do
        local pid="${PIDS[$i]}"
        local name="${SERVICES[$i]}"
        if kill -0 "$pid" 2>/dev/null; then
            echo "  → 停止 $name (PID: $pid)"
            kill -TERM "$pid" 2>/dev/null || true
        fi
    done

    # Wait a bit then force kill any remaining
    sleep 1
    for i in "${!PIDS[@]}"; do
        local pid="${PIDS[$i]}"
        local name="${SERVICES[$i]}"
        if kill -0 "$pid" 2>/dev/null; then
            echo "  → 强制停止 $name (PID: $pid)"
            kill -KILL "$pid" 2>/dev/null || true
        fi
    done

    wait 2>/dev/null || true
    echo "  所有服务已停止"
    echo "  日志目录: $LOG_DIR"
}

# Ensure cleanup only runs once
_CLEANED_UP=false
_cleanup_once() {
    if [ "$_CLEANED_UP" = false ]; then
        _CLEANED_UP=true
        cleanup
    fi
    exit 0
}

trap _cleanup_once INT TERM

echo "========================================"
echo "  数模Dashboard - 一键启动所有服务"
echo "========================================"
echo ""
echo "日志目录: $LOG_DIR"
echo "前端模式: $FRONTEND_MODE"
echo "按 Ctrl+C 优雅退出"
echo ""

# 1. Redis
echo "[1/6] 启动 Redis..."
kill_port 6379 "Redis"
if [ ! -f "$ROOT_DIR/redis/bin/redis-server" ]; then
    echo "错误: Redis 未安装，请先运行 ./scripts/setup.sh"
    exit 1
fi
mkdir -p "$ROOT_DIR/redis/data"
cd "$ROOT_DIR"
"$ROOT_DIR/redis/bin/redis-server" "$ROOT_DIR/redis/redis.conf" > "$(log_file redis)" 2>&1 &
PIDS+=($!)
SERVICES+=("Redis")
CWD_VARS+=("$ROOT_DIR")
wait_for_port 6379 "Redis" || { echo "Redis 启动失败，正在退出..."; cleanup; exit 1; }

# 2. Backend
echo "[2/6] 启动 Backend (FastAPI)..."
kill_port 8000 "Backend"
cd "$ROOT_DIR/backend"
echo "  → 运行数据库迁移 (alembic upgrade head)..."
if ! uv run alembic upgrade head > "$(log_file backend-alembic)" 2>&1; then
    echo "错误: 数据库迁移失败，请查看 $(log_file backend-alembic)"
    exit 1
fi
echo "  → 启动 FastAPI 服务..."
uv run uvicorn app.main:app --reload --port 8000 > "$(log_file backend)" 2>&1 &
PIDS+=($!)
SERVICES+=("Backend")
CWD_VARS+=("$ROOT_DIR/backend")
if ! wait_for_port 8000 "Backend" 45; then
    echo "Backend 启动失败，正在退出..."
    cleanup
    exit 1
fi

# 3. Cloud Agent
echo "[3/6] 启动 Cloud Agent..."
kill_port 8001 "CloudAgent"
cd "$ROOT_DIR/cloud_agent"
uv run python main.py > "$(log_file cloud-agent)" 2>&1 &
PIDS+=($!)
SERVICES+=("CloudAgent")
CWD_VARS+=("$ROOT_DIR/cloud_agent")
wait_for_port 8001 "CloudAgent" || { echo "CloudAgent 启动失败，正在退出..."; cleanup; exit 1; }

# 4. Doc Server
echo "[4/6] 启动 Doc Server..."
kill_port 8002 "DocServer"
cd "$ROOT_DIR/doc_server"
PYTHONPATH="$ROOT_DIR" uv run uvicorn doc_server.main:app --port 8002 > "$(log_file doc-server)" 2>&1 &
PIDS+=($!)
SERVICES+=("DocServer")
CWD_VARS+=("$ROOT_DIR/doc_server")
wait_for_port 8002 "DocServer" || { echo "DocServer 启动失败，正在退出..."; cleanup; exit 1; }

# 5. Local Agent
echo "[5/6] 启动 Local Agent..."
kill_port 8765 "LocalAgent"
cd "$ROOT_DIR/local_agent"
uv run python main.py > "$(log_file local-agent)" 2>&1 &
PIDS+=($!)
SERVICES+=("LocalAgent")
CWD_VARS+=("$ROOT_DIR/local_agent")
wait_for_port 8765 "LocalAgent" || { echo "LocalAgent 启动失败，正在退出..."; cleanup; exit 1; }
wait_for_local_agent_ready || { echo "LocalAgent 协议就绪检查失败，正在退出..."; cleanup; exit 1; }

# 6. Frontend
echo "[6/6] 启动 Frontend (Next.js)..."
kill_port 3000 "Frontend"
# Also clean up any zombie Next.js dev servers for this project
for pid in $(ps aux | grep "next" | grep -v grep | awk '{print $2}'); do
    cwd=$(readlink /proc/$pid/cwd 2>/dev/null || true)
    if [[ "$cwd" == *"mmdash/frontend"* ]]; then
        kill -9 "$pid" 2>/dev/null || true
    fi
done
cd "$ROOT_DIR/frontend"
if [ "$FRONTEND_MODE" = "dev" ]; then
    npm run dev > "$(log_file frontend)" 2>&1 &
else
    echo "  → 构建 Frontend 生产包..."
    npm run build > "$(log_file frontend-build)" 2>&1
    echo "  → 启动 Frontend 生产服务..."
    npm run start > "$(log_file frontend)" 2>&1 &
fi
PIDS+=($!)
SERVICES+=("Frontend")
CWD_VARS+=("$ROOT_DIR/frontend")
# Dev mode Next.js takes longer — give it 90s
FRONTEND_WAIT=60
[ "$FRONTEND_MODE" = "dev" ] && FRONTEND_WAIT=90
if ! wait_for_http "http://127.0.0.1:3000" "Frontend" $FRONTEND_WAIT; then
    echo "Frontend 启动失败，正在退出..."
    cleanup
    exit 1
fi

echo ""
echo "========================================"
echo "  所有服务已启动"
echo "========================================"
echo ""
echo "  Redis:       http://localhost:6379"
echo "  Backend:     http://localhost:8000"
echo "  CloudAgent:  http://localhost:8001"
echo "  DocServer:   http://localhost:8002"
echo "  LocalAgent:  ws://127.0.0.1:8765"
echo "  Frontend:    http://localhost:3000"
if [ "$FRONTEND_MODE" = "dev" ]; then
    echo "  FrontendMode: development"
else
    echo "  FrontendMode: production"
fi
echo ""
echo "按 Ctrl+C 停止所有服务"
echo ""

# Keep the script running with retry-based health monitoring.
# 3 consecutive failures needed before triggering cleanup — this handles
# transient restarts (e.g., uvicorn --reload window) gracefully.
FAILURE_COUNT=0
MAX_FAILURES=3
while true; do
    all_alive=true
    for i in "${!PIDS[@]}"; do
        if ! kill -0 "${PIDS[$i]}" 2>/dev/null; then
            all_alive=false
        fi
    done

    if [ "$all_alive" = false ]; then
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        for i in "${!PIDS[@]}"; do
            if ! kill -0 "${PIDS[$i]}" 2>/dev/null; then
                echo "警告: ${SERVICES[$i]} 已退出 (${FAILURE_COUNT}/${MAX_FAILURES})"
            fi
        done

        if [ "$FAILURE_COUNT" -ge "$MAX_FAILURES" ]; then
            echo ""
            echo "服务连续 ${MAX_FAILURES} 次检测失败，正在关闭所有服务..."
            _cleanup_once
        fi
    else
        # All alive — reset failure counter
        if [ "$FAILURE_COUNT" -gt 0 ]; then
            echo "所有服务已恢复"
        fi
        FAILURE_COUNT=0
    fi
    sleep 3
done
