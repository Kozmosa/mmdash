#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$ROOT_DIR/logs"

mkdir -p "$LOG_DIR"

PIDS=()
SERVICES=()

log_file() {
    echo "$LOG_DIR/$1.log"
}

# Kill any process listening on a given port
kill_port() {
    local port=$1
    local pname=$2
    local pids
    pids=$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
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

    wait
    echo "  所有服务已停止"
    echo "  日志目录: $LOG_DIR"
    exit 0
}

trap cleanup INT TERM

echo "========================================"
echo "  数模Dashboard - 一键启动所有服务"
echo "========================================"
echo ""
echo "日志目录: $LOG_DIR"
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
sleep 1

# 2. Backend
echo "[2/6] 启动 Backend (FastAPI)..."
kill_port 8000 "Backend"
cd "$ROOT_DIR/backend"
echo "  → 运行数据库迁移 (alembic upgrade head)..."
uv run alembic upgrade head > "$(log_file backend-alembic)" 2>&1
echo "  → 启动 FastAPI 服务..."
uv run uvicorn app.main:app --reload --port 8000 > "$(log_file backend)" 2>&1 &
PIDS+=($!)
SERVICES+=("Backend")
sleep 1

# 3. Cloud Agent
echo "[3/6] 启动 Cloud Agent..."
kill_port 8001 "CloudAgent"
cd "$ROOT_DIR/cloud_agent"
uv run python main.py > "$(log_file cloud-agent)" 2>&1 &
PIDS+=($!)
SERVICES+=("CloudAgent")
sleep 1

# 4. Doc Server
echo "[4/6] 启动 Doc Server..."
kill_port 8002 "DocServer"
cd "$ROOT_DIR/doc_server"
PYTHONPATH="$ROOT_DIR" uv run uvicorn doc_server.main:app --port 8002 > "$(log_file doc-server)" 2>&1 &
PIDS+=($!)
SERVICES+=("DocServer")
sleep 1

# 5. Local Agent
echo "[5/6] 启动 Local Agent..."
kill_port 8765 "LocalAgent"
cd "$ROOT_DIR/local_agent"
uv run python main.py > "$(log_file local-agent)" 2>&1 &
PIDS+=($!)
SERVICES+=("LocalAgent")
sleep 1

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
npm run dev > "$(log_file frontend)" 2>&1 &
PIDS+=($!)
SERVICES+=("Frontend")
sleep 2

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
echo ""
echo "按 Ctrl+C 停止所有服务"
echo ""

# Keep the script running
while true; do
    all_alive=true
    for i in "${!PIDS[@]}"; do
        if ! kill -0 "${PIDS[$i]}" 2>/dev/null; then
            echo "警告: ${SERVICES[$i]} 已退出"
            all_alive=false
        fi
    done
    if [ "$all_alive" = false ]; then
        echo ""
        echo "有服务异常退出，正在关闭其他服务..."
        cleanup
    fi
    sleep 3
done
