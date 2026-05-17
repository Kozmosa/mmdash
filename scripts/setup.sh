#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "========================================"
echo "  数模Dashboard - 一键初始化"
echo "========================================"
echo ""

# 1. Check and install uv
if ! command -v uv &> /dev/null; then
    echo "[1/5] uv 未安装，正在安装..."
    curl -LsSf https://astral.sh/uv/install.sh | sh
    # Try to source the cargo env to make uv available
    if [ -f "$HOME/.cargo/env" ]; then
        source "$HOME/.cargo/env"
    fi
    if ! command -v uv &> /dev/null; then
        echo "错误: uv 安装失败，请手动安装后重试"
        echo "   curl -LsSf https://astral.sh/uv/install.sh | sh"
        exit 1
    fi
    echo "[1/5] uv 安装完成"
else
    echo "[1/5] uv 已安装: $(uv --version)"
fi

# 2. Download and compile Redis
echo ""
echo "[2/5] 下载并编译 Redis..."
if [ -f "$ROOT_DIR/redis/bin/redis-server" ]; then
    echo "       Redis 已存在，跳过"
else
    "$SCRIPT_DIR/download-redis.sh"
fi

# 3. Create uv virtual environments and sync dependencies
echo ""
echo "[3/5] 创建 Python 虚拟环境并同步依赖..."

for module in backend cloud_agent local_agent doc_server; do
    echo "       → $module"
    cd "$ROOT_DIR/$module"
    uv sync
    echo "         完成"
done

# 4. Install frontend dependencies
echo ""
echo "[4/5] 安装前端依赖..."
cd "$ROOT_DIR/frontend"
if ! command -v npm &> /dev/null; then
    echo "错误: npm 未安装，请先安装 Node.js"
    exit 1
fi
npm install

# 5. Feishu CLI (optional — for IM notifications)
echo ""
echo "[可选] 安装飞书 CLI (用于 IM 通知)..."
if ! command -v lark-cli &>/dev/null; then
    if command -v npm &>/dev/null; then
        npm install -g @larksuite/cli 2>/dev/null && echo "  ✓ 飞书 CLI 安装完成" || echo "  ⚠ 飞书 CLI 安装失败，可稍后手动安装"
    else
        echo "  ⚠ 未检测到 npm，跳过飞书 CLI 安装"
    fi
else
    echo "  ✓ 飞书 CLI 已安装"
fi
if command -v lark-cli &>/dev/null; then
    lark-cli config init 2>/dev/null || true
    echo "  → 请手动运行 'lark-cli auth login --recommend' 完成飞书登录"
fi

# 6. Summary
echo ""
echo "========================================"
echo "  初始化完成！"
echo "========================================"
echo ""
echo "下一步: 启动所有服务"
echo "   ./scripts/start-all.sh"
echo ""
echo "或单独启动:"
echo "   Redis:      ./scripts/start-redis.sh"
echo "   Backend:    cd backend && uv run uvicorn app.main:app --reload"
echo "   CloudAgent: cd cloud_agent && uv run python main.py"
echo "   LocalAgent: cd local_agent && uv run python main.py"
echo "   DocServer:  cd doc_server && uv run uvicorn main:app --port 8002"
echo "   Frontend:   cd frontend && npm run dev"
echo ""
