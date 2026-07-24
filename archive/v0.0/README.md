# 数模Dashboard

**数学建模竞赛的端云协同协作平台**  
让题目解析、模型推导、实验求解与团队协同进入同一个可观察、可编排、可复现的运行时界面。

[![Python](https://img.shields.io/badge/Python-3.11+-3776AB?style=for-the-badge&logo=python&logoColor=white)] [![FastAPI](https://img.shields.io/badge/FastAPI-Backend-009688?style=for-the-badge&logo=fastapi&logoColor=white)] [![Next.js](https://img.shields.io/badge/Next.js-Frontend-000000?style=for-the-badge&logo=nextdotjs&logoColor=white)] [![React](https://img.shields.io/badge/React-18-61DAFB?style=for-the-badge&logo=react&logoColor=black)] [![SQLite](https://img.shields.io/badge/SQLite-Database-003B57?style=for-the-badge&logo=sqlite&logoColor=white)] [![Redis](https://img.shields.io/badge/Redis-Cache-DC382D?style=for-the-badge&logo=redis&logoColor=white)] [![OpenAI](https://img.shields.io/badge/OpenAI-LLM-412991?style=for-the-badge&logo=openai&logoColor=white)]

[Why](#-why-数模dashboard) · [Capabilities](#-core-capabilities) · [Architecture](#-architecture) · [Quick Start](#-quick-start) · [Development](#-development)

---

## What is 数模Dashboard?

**数模Dashboard** 将数学建模竞赛的工作流从"分散的文档 + 本地脚本 + 临时沟通"收敛成一个工程化控制面：

- 用 **Web 面板** 统一管理题目、模型、时间线与实验；
- 用 **Notion 集成** 持久化存储题目解析与模型文档；
- 用 **LLM 辅助** 完成符号识别、公式解释、结构解析与错误纠正；
- 用 **Local Agent** 在本地执行自动化实验与参数网格搜索；
- 用 **版本控制** 追踪模型快照与实验结果的完整生命周期。

它不是一个普通的文档仓库，也不是单一的代码编辑器。数模Dashboard 的目标是成为数模团队的 **协作中枢**：任务可分配、模型可追溯、实验可复现、成果可同步。

---

## Console Preview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 数模Dashboard                              Team: Alpha      Project: MCM    │
├─────────────────────┬────────────────────────────────────────────────────────┤
│  主页               │  模型面板                                               │
│  时间线             │  ┌─ 符号表 ─────────────────────────────────────────┐  │
│  模型               │  │  x  — 决策变量        λ  — 拉格朗日乘子          │  │
│  实验求解           │  │  μ  — 均值参数        σ  — 标准差                │  │
│  Agent              │  └───────────────────────────────────────────────────┘  │
│                     │                                                        │
│  团队 TODO          │  结构解析                                                │
│  [ ] 完成灵敏度分析 │  ├── 目标函数优化                                       │
│  [x] 题目 PDF 解析  │  ├── 约束条件建模                                       │
│  [ ] Git 提交 v1.0  │  └── 结果验证                                           │
│                     │                                                        │
│  个人 TODO          │  版本历史                                                │
│  [x] 模型 v0.1      │  v0.1  init        v0.2  fix-constraint              │
│  [ ] 实验参数调优   │  v0.3  optimize    v1.0  final                        │
└─────────────────────┴────────────────────────────────────────────────────────┘
```

---

## Why 数模Dashboard?

传统数模竞赛团队通常同时面对几类复杂度：

| 问题 | 数模Dashboard 的处理方式 |
|------|------------------------|
| 题目和模型散落在不同文档中 | Notion 集成统一存储，面板实时同步 |
| 模型公式和符号含义难以快速理解 | LLM 自动识别符号表、解释公式、解析结构 |
| 实验过程不可复现、参数管理混乱 | Local Agent 自动化实验 + 参数快照 + 规范化结果目录 |
| 团队进度不透明、任务分配混乱 | Timeline + TODO 双轨管理，团队与个人视角分离 |
| 模型版本无追踪、修改难以回溯 | Commit 快照 + Diff 对比，完整版本历史 |
| 本地实验与团队成果割裂 | Git 绑定实验记录，Push 即同步，隔离性与协作并存 |

---

## Core Capabilities

### 主页面板

- 题目文件上传与解析（PDF / 纯文本）。
- 团队 TODO 与个人 TODO 双轨管理。
- 团队 Timeline 与个人 Timeline 概览。
- 整体进度信息一目了然。

### 时间线面板

- 类似日程表的时间轴视图。
- 支持团队事件与个人事件分离管理。
- 与 TODO 联动，任务与时间节点绑定。

### 模型面板

- **符号表**：LLM 自动识别模型符号，悬浮显示含义（优先手工定义， fallback 上下文推断）。
- **结构解析**：模型总体与分块解释，配合"模型 ↔ 题目"对应关系图。
- **公式解释**：复杂数学表达拆解说明。
- **版本控制 (Commit)**：标记关键模型版本，完整快照历史。
- **Diff 功能**：对比版本差异，识别修改人与修改内容。
- **逻辑与笔误纠正**：红色高亮批注标记潜在错误（仅批注，不修改原文）。
- **快捷导出**：支持 PDF / Markdown 格式导出。
- **安全边界**：所有 AI 分析结果强制标注"仅供参考"。

### 实验与求解面板

- **环境接管**：Local Agent 检测本地 Python、Conda、GCC 等环境配置。
- **版本追踪**：基于本地 Git log 查看 Solver 文件修改历史。
- **自动化实验**：
  - 选取 Git 树下的任意版本、任意 Solver 文件。
  - 提取可调节参数，前端设定范围和步长，自动执行网格搜索。
- **规范化结果**：`/results/[时间戳]_[solver文件名]/`
  - `fig/` — 图表
  - `log.txt` — 运行日志
  - `analysis.md` — 初步分析草稿
  - `params_snapshot.json` — 参数快照

### Agent 端云协同

- **Cloud Agent (云端记忆中枢)**：
  - 为每个 Project 维护全局记忆向量。
  - 实时掌握题目文件、模型文档、TODO 状态。
  - 负责下发记忆上下文到各成员本地。
- **Local Agent (本地执行引擎)**：
  - 本地后台守护进程，WebSocket 直连浏览器。
  - 接管本地 Shell、执行自动化实验。
  - 结合云端模型解释与本地实验结果，自动生成分析报告。

---

## Architecture

```
┌─────────────────────────────────┐
│         Next.js WebUI            │
│  主页 / 时间线 / 模型 / 实验求解  │
└───────────────┬─────────────────┘
                │ HTTP
┌───────────────▼─────────────────┐
│        FastAPI Backend           │
│  Auth / Teams / Projects / API   │
│  SQLite (state) + Redis (cache)  │
└───────────────┬─────────────────┘
                │ HTTP
┌───────────────▼─────────────────┐
│        Cloud Agent               │
│  Context Vector / Notion Sync    │
└───────────────┬─────────────────┘
                │ HTTP (push context)
┌───────────────▼─────────────────┐
│        Local Agent               │
│  WebSocket ws://127.0.0.1:8765   │
│  Shell / Experiment / Git        │
└─────────────────────────────────┘
```

**核心架构原则**：

- **轻量云端 (Thin Server)**：服务器仅负责 Notion API 调用、数据状态缓存及团队基础信息存储。
- **重度本地 (Fat Client)**：所有高强度计算、自动化实验均由部署在用户本机的 Local Agent 执行。
- **本地化实验记录**：实验产出与 Git 仓库强绑定，未 Push 的数据对团队其他成员不可见，保障本地调试隐私与环境隔离。

---

## Quick Start

### 一键初始化（首次使用）

```bash
# 安装 uv、编译 Redis、创建虚拟环境、同步所有依赖
./scripts/setup.sh
```

### 一键启动所有服务

```bash
# 同时启动 Redis + Backend + CloudAgent + LocalAgent + Frontend
./scripts/start-all.sh
```

按 `Ctrl+C` 即可优雅关闭所有服务，避免僵尸进程。

### 服务地址

| 服务 | 地址 |
|------|------|
| Frontend | http://localhost:3000 |
| Backend API | http://localhost:8000 |
| Backend Docs | http://localhost:8000/docs |
| Cloud Agent | http://localhost:8001 |
| Local Agent | ws://127.0.0.1:8765 |
| Redis | localhost:6379 |

### 单独启动（开发调试）

```bash
# Redis
./scripts/start-redis.sh

# Backend
cd backend && uv run uvicorn app.main:app --reload

# Cloud Agent
cd cloud_agent && uv run python main.py

# Local Agent
cd local_agent && uv run python main.py

# Frontend
cd frontend && npm run dev
```

---

## Project Layout

```
.
├── backend/              # FastAPI 后端服务 (uv 虚拟环境)
│   ├── app/
│   │   ├── api/          # API 路由 (auth, teams, projects, ...)
│   │   ├── core/         # 配置与设置
│   │   ├── models.py     # SQLAlchemy 数据模型
│   │   └── database.py   # 数据库连接 (SQLite)
│   └── pyproject.toml    # uv 依赖管理
├── frontend/             # Next.js 前端
│   ├── app/              # 页面路由
│   │   ├── home/         # 主页面板
│   │   ├── timeline/     # 时间线面板
│   │   ├── model/        # 模型面板
│   │   ├── experiment/   # 实验求解面板
│   │   └── auth/         # 登录注册
│   └── lib/              # 工具函数与 Agent 客户端
├── cloud_agent/          # 云端记忆中枢 (FastAPI)
│   └── main.py
├── local_agent/          # 本地执行引擎 (WebSocket)
│   └── main.py
├── scripts/              # 一键初始化、一键启动、Redis 管理脚本
├── logs/                 # 服务运行日志
├── redis/                # Redis 本地安装目录
├── PRD.md                # 产品需求文档
└── stage1-plan.md        # 实现计划
```

---

## Tech Stack

| Layer | Stack |
|-------|-------|
| Backend API | Python 3.11+, FastAPI, Uvicorn, SQLAlchemy 2.0 |
| Database | SQLite (内置), Redis 7.x (缓存) |
| Frontend | Next.js 15, React 18, TypeScript, Tailwind CSS |
| State Management | Zustand |
| AI / LLM | OpenAI API |
| Document Sync | Notion API |
| Local Agent | Python asyncio, websockets, psutil |
| Cloud Agent | FastAPI, httpx |
| Version Control | GitPython |

---

## Development

后端代码检查：

```bash
cd backend
python -m pytest  # 运行测试
```

前端代码检查：

```bash
cd frontend
npm run lint
npm run build
```

---

## Documentation

- [产品需求文档](PRD.md) — 完整功能规格说明
- [实现计划](stage1-plan.md) — 开发里程碑与验收标准

---

## License

MIT License
