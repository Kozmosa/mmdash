# 引文管理功能设计文档

**日期**: 2026-05-15  
**状态**: 已确认，待实现  
**方案**: 方案 1 — 完整内建引文库 + 后台增量同步

---

## 1. 概述

为 数模Dashboard 新增"引文管理"标签页，每个项目维护独立的引文库。支持本地手动增删改查引文，以及通过 Zotero Web API 单向增量导入文献。引文可单条或批量导出为 BibTeX 格式。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 引文库归属 | 项目级别 | 每个项目引文不同，复用现有 project/team 权限模型 |
| Zotero 集成方式 | Web API (方案 B) | 不依赖本地客户端，服务器端可做定期同步 |
| API key 绑定粒度 | 项目级别 | 团队指定一人提供 Zotero 账号即可 |
| 同步方向 | 单向导入 (Zotero → 本地) | 简单，无需冲突处理 |
| 同步策略 | 增量同步 | 节省 API 配额，速度快 |
| 同步触发 | 手动 + 每 15 分钟自动 | 兼顾即时性和自动化 |
| 编辑权限 | 类似 timeline | 团队成员可增改，删除仅限创建者 |

## 3. 数据库模型

### 3.1 Citation（引文）

绑定 `project_id`，每个项目独立引文库。

```python
class Citation(Base):
    __tablename__ = "citations"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)

    # 核心书目字段
    title = Column(String(500), nullable=False)
    authors = Column(Text, nullable=True)        # JSON 数组，如 ["Zhang, S.", "Li, M."]
    journal = Column(String(255), nullable=True) # 刊名/会议名
    year = Column(Integer, nullable=True)
    volume = Column(String(50), nullable=True)
    issue = Column(String(50), nullable=True)
    pages = Column(String(100), nullable=True)
    doi = Column(String(255), nullable=True, index=True)
    url = Column(String(500), nullable=True)
    abstract = Column(Text, nullable=True)

    # BibTeX 专用
    bibtex_key = Column(String(100), nullable=True)
    bibtex_type = Column(String(50), default="article")

    # Zotero 溯源（本地手动添加的留空）
    zotero_item_key = Column(String(50), nullable=True, index=True)
    zotero_version = Column(Integer, nullable=True)
    source = Column(String(20), default="manual")  # "manual" | "zotero"

    # 原始数据备份（灵活扩展）
    extra_data = Column(Text, nullable=True)  # JSON，存 Zotero 原始 item 的完整数据

    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)
```

### 3.2 ZoteroConfig（Zotero 连接配置）

每个项目最多一条配置。

```python
class ZoteroConfig(Base):
    __tablename__ = "zotero_configs"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False, unique=True)
    api_key = Column(String(255), nullable=False)
    library_id = Column(String(50), nullable=False)
    library_type = Column(String(20), default="user")  # "user" | "group"
    last_sync_version = Column(Integer, nullable=True)
    last_sync_at = Column(DateTime, nullable=True)
    last_sync_status = Column(String(20), default="idle")  # "idle" | "syncing" | "error"
    last_sync_error = Column(Text, nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)
```

### 3.3 现有模型修改

`Project` 模型添加关系：

```python
citations = relationship("Citation", back_populates="project", cascade="all, delete-orphan")
zotero_config = relationship("ZoteroConfig", back_populates="project", uselist=False, cascade="all, delete-orphan")
```

### 3.4 Alembic 迁移

新增 `citations` 和 `zotero_configs` 两张表。SQLAlchemy 关系修改不需要数据库迁移。

## 4. 后端 API 设计

新建 `backend/app/api/references.py`，在 `app/main.py` 注册为 `/api/references`。

### 4.1 引文 CRUD

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/references/{project_id}` | 列出引文，支持查询参数：q, year_from, year_to, source, sort_by, sort_order |
| POST | `/api/references/{project_id}` | 创建本地引文（source="manual"） |
| PUT | `/api/references/{project_id}/{citation_id}` | 更新引文（source/zotero_* 字段不可改） |
| DELETE | `/api/references/{project_id}/{citation_id}` | 删除引文（仅限创建者） |

### 4.2 Zotero 配置

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/references/{project_id}/zotero-config` | 获取配置（api_key 脱敏显示） |
| POST | `/api/references/{project_id}/zotero-config` | 创建或更新配置 |
| DELETE | `/api/references/{project_id}/zotero-config` | 删除配置 |

### 4.3 同步与导出

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/references/{project_id}/sync` | 手动触发增量同步 |
| GET | `/api/references/{project_id}/sync-status` | 获取同步状态 |
| POST | `/api/references/{project_id}/export` | 导出 BibTeX，Body: ids(可选) |

### 4.4 权限模型

复用现有 `TeamMember` 检查（团队成员可访问），删除时额外检查 `citation.user_id == current_user.id`。

### 4.5 BibTeX 生成

后端直接字符串组装，按 BibTeX 格式输出：

```
@article{key,
  title = {Title},
  author = {Author},
  journal = {Journal},
  year = {2024},
  ...
}
```

## 5. 同步架构

### 5.1 增量同步核心

Zotero API 支持 `since={last_modified_version}` 参数，只返回自该版本以来变更/新增的条目：

```
GET /users/{library_id}/items?since={last_sync_version}&format=json&v=3&limit=100
```

响应头 `Last-Modified-Version` 是服务器当前最新全局版本号，存入 `zotero_configs.last_sync_version` 作为下次同步基准。

### 5.2 字段映射

| Zotero 字段 | Citation 字段 | 转换逻辑 |
|-------------|---------------|----------|
| `title` | `title` | 直接 |
| `creators` (type=author) | `authors` | 格式化为 `Last, First and Last, First` |
| `publicationTitle` | `journal` | 直接 |
| `date` | `year` | 正则提取 4 位年份 |
| `volume` | `volume` | 直接 |
| `issue` | `issue` | 直接 |
| `pages` | `pages` | 直接 |
| `DOI` | `doi` | 直接 |
| `url` | `url` | 直接 |
| `abstractNote` | `abstract` | 直接 |
| `itemType` | `bibtex_type` | 映射表（见下方详细映射） |
| `key` | `zotero_item_key` | 直接 |
| `version` | `zotero_version` | 直接 |
| 整个 item JSON | `extra_data` | `json.dumps(item)` |

**Zotero itemType → bibtex_type 映射表：**

| Zotero itemType | bibtex_type |
|-----------------|-------------|
| `journalArticle` | `article` |
| `book` | `book` |
| `bookSection` | `incollection` |
| `conferencePaper` | `inproceedings` |
| `thesis` | `phdthesis` |
| `report` | `techreport` |
| `webpage` | `misc` |
| `newspaperArticle` | `article` |
| `magazineArticle` | `article` |
| 其他 | `misc` |

### 5.3 同步流程

1. 读取 `ZoteroConfig`，检查 `last_sync_status != "syncing"`
2. 设置 `status = "syncing"`
3. 调用 Zotero API（循环分页直到无更多数据）
4. 对每个返回的 item：
   - 按 `zotero_item_key` 查找本地引文
   - 存在 → 更新字段（Zotero 来源的引文全部覆盖，但保留本地用户修改的 `bibtex_key`；manual 来源的引文不会被 Zotero 同步触及）
   - 不存在 → 新建引文（`source="zotero"`）
5. 更新 `last_sync_version = Last-Modified-Version`
6. 更新 `last_sync_at`，`status = "idle"`

### 5.4 后台定时任务

项目没有 Celery/APScheduler，使用 `asyncio` 原生实现：

- 在 `app/main.py` 的 `@app.on_event("startup")` 中启动 `asyncio.create_task` 协程
- 协程内部用 `asyncio.Event.wait(timeout=900)` 实现 15 分钟定时
- 遍历所有有 `ZoteroConfig` 且 `status != "syncing"` 的项目执行同步
- 在 `@app.on_event("shutdown")` 中设置 Event，优雅退出
- 单个项目同步失败不影响其他项目

### 5.5 关于删除同步

增量同步不处理 Zotero 端**删除**的条目（单向导入的合理语义）。如果用户需要清理，手动删除本地引文即可。

## 6. 前端设计

### 6.1 路由和导航

- 新建 `frontend/app/(main)/references/page.tsx` 和 `loading.tsx`
- `app-sidebar.tsx` 的 `navItems` 追加：`{ href: "/references", label: "引文管理", icon: BookOpen }`
- `app-navbar.tsx` 的 `pageTitles` 追加：`"/references": "引文管理"`

### 6.2 页面布局

```
┌─────────────────────────────────────────────────────────────┐
│  [搜索框] [年份筛选 ▼] [来源筛选 ▼] [排序 ▼]                  │  ← 筛选栏
│              [+ 添加引文] [↻ 同步] [⬇ 导出BibTeX]            │  ← 操作栏
├─────────────────────────────────────────────────────────────┤
│  ☑ │ 标题              │ 作者        │ 刊名      │ 年份 │ 源 │ 操作 │
├────┼───────────────────┼─────────────┼───────────┼──────┼────┼──────┤
│  ☑ │ A Review on ...   │ Zhang, Li   │ Nature    │ 2024 │ Z  │ ✏ 🗑 │
│  ☐ │ 手动添加的论文...  │ Wang        │ Science   │ 2023 │ M  │ ✏ 🗑 │
└─────────────────────────────────────────────────────────────┘
│  共 42 条  [◀ 1 2 3 ▶]                                      │  ← 分页
```

### 6.3 组件拆分

放在 `frontend/components/references/` 目录下：

| 组件 | 职责 |
|------|------|
| `CitationTable` | 表格主体，列：复选框、标题、作者、刊名、年份、来源标签、操作按钮 |
| `CitationFilters` | 搜索框 + 年份范围 + 来源筛选 + 排序 |
| `CitationForm` | 添加/编辑引文的表单（Dialog/Sheet） |
| `ZoteroConfigPanel` | Zotero 连接配置面板 |
| `SyncStatusBadge` | 显示同步状态和上次同步时间 |
| `ExportButton` | 导出 BibTeX，支持"导出选中"和"导出全部" |

### 6.4 状态管理

- 引文列表用 React `useState` + `useEffect` 拉取
- 筛选条件本地 state，变化时重新请求
- 选中的行用 `Set<string>` 管理

### 6.5 BibTeX 导出

- 调用 `POST /api/references/{project_id}/export` 获取 BibTeX 文本
- 前端用 `Blob` + `URL.createObjectURL` 触发 `.bib` 文件下载

## 7. 错误处理与边界情况

| 场景 | 处理策略 |
|------|----------|
| Zotero API 403（key 无效） | 标记 error，前端显示"API key 无效，请重新配置" |
| Zotero API 404（library 不存在） | 标记 error，提示检查 library ID |
| 网络超时 | 重试 3 次（指数退避），仍失败则标记 error |
| 同步中用户再次触发 | 返回 `{"status": "syncing"}` |
| Zotero 返回大量数据 | 分页拉取（`limit=100`，循环 `start` 偏移） |
| Zotero 字段缺失 | 缺失字段留空，不阻塞同步 |
| BibTeX key 为空 | 自动生成：`author_year_hash` 格式 |
| 用户删除本地手动引文 | 正常删除，不影响 Zotero |
| 用户删除 Zotero 端条目 | 本地保留（单向导入语义） |

## 8. 测试策略

- **单元测试**: BibTeX 生成、Zotero 字段解析、作者格式化
- **API 测试**: CRUD 权限、导出功能、同步触发
- **集成测试**: Zotero API 调用（mock httpx）

## 9. 文件清单（待创建/修改）

### 后端
- `backend/app/models.py` — 新增 Citation、ZoteroConfig 模型，修改 Project 关系
- `backend/app/api/references.py` — 新建引文 API router
- `backend/app/services/zotero_sync.py` — 同步服务和后台任务
- `backend/app/main.py` — 注册 router，启停后台任务
- `backend/migrations/versions/` — Alembic 迁移

### 前端
- `frontend/app/(main)/references/page.tsx`
- `frontend/app/(main)/references/loading.tsx`
- `frontend/components/app-sidebar.tsx` — 添加导航项
- `frontend/components/app-navbar.tsx` — 添加页面标题
- `frontend/components/references/CitationTable.tsx`
- `frontend/components/references/CitationFilters.tsx`
- `frontend/components/references/CitationForm.tsx`
- `frontend/components/references/ZoteroConfigPanel.tsx`
- `frontend/components/references/SyncStatusBadge.tsx`
- `frontend/components/references/ExportButton.tsx`

### 测试
- `backend/tests/test_references.py` — API 测试
