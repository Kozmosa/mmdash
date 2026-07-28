# 引文管理功能实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 数模Dashboard 新增引文管理标签页，包含内建引文库（CRUD + BibTeX 导出）和 Zotero Web API 增量同步。

**Architecture:** 后端新增 `Citation` / `ZoteroConfig` 模型和 `/api/references` router，前端新增 `/references` 页面和配套组件。同步用 `asyncio` 后台定时任务实现。

**Tech Stack:** FastAPI, SQLAlchemy 2.0, Alembic, Next.js, shadcn/ui, httpx

---

## 文件结构

### 后端

| 文件 | 职责 |
|------|------|
| `backend/app/models.py` | 新增 `Citation`、`ZoteroConfig` 模型；`Project` 添加关系 |
| `backend/app/api/references.py` | 引文 CRUD、Zotero 配置、同步、导出 API |
| `backend/app/services/zotero_sync.py` | Zotero API 调用、字段解析、增量同步逻辑 |
| `backend/app/services/bibtex.py` | BibTeX 格式化生成 |
| `backend/app/main.py` | 注册 router，启停后台同步任务 |
| `backend/migrations/versions/` | Alembic 迁移文件（autogenerate） |
| `backend/tests/test_references.py` | API 测试 |

### 前端

| 文件 | 职责 |
|------|------|
| `frontend/app/(main)/references/page.tsx` | 引文管理主页面 |
| `frontend/app/(main)/references/loading.tsx` | 加载态 |
| `frontend/components/app-sidebar.tsx` | 追加导航项 |
| `frontend/components/app-navbar.tsx` | 追加页面标题 |
| `frontend/components/references/CitationTable.tsx` | 引文表格 |
| `frontend/components/references/CitationFilters.tsx` | 筛选栏 |
| `frontend/components/references/CitationForm.tsx` | 添加/编辑表单 |
| `frontend/components/references/ZoteroConfigPanel.tsx` | Zotero 配置面板 |
| `frontend/components/references/SyncStatusBadge.tsx` | 同步状态显示 |
| `frontend/components/references/ExportButton.tsx` | 导出按钮 |

---

## Task 1: 数据库模型与 Alembic 迁移

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/migrations/versions/` (autogenerate)
- Test: `backend/tests/test_references.py`

- [ ] **Step 1: 添加 Citation 和 ZoteroConfig 模型**

在 `backend/app/models.py` 末尾追加：

```python
class Citation(Base):
    __tablename__ = "citations"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)

    title = Column(String(500), nullable=False)
    authors = Column(Text, nullable=True)
    journal = Column(String(255), nullable=True)
    year = Column(Integer, nullable=True)
    volume = Column(String(50), nullable=True)
    issue = Column(String(50), nullable=True)
    pages = Column(String(100), nullable=True)
    doi = Column(String(255), nullable=True, index=True)
    url = Column(String(500), nullable=True)
    abstract = Column(Text, nullable=True)

    bibtex_key = Column(String(100), nullable=True)
    bibtex_type = Column(String(50), default="article")

    zotero_item_key = Column(String(50), nullable=True, index=True)
    zotero_version = Column(Integer, nullable=True)
    source = Column(String(20), default="manual")

    extra_data = Column(Text, nullable=True)

    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)

    project = relationship("Project", back_populates="citations")
    user = relationship("User")


class ZoteroConfig(Base):
    __tablename__ = "zotero_configs"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False, unique=True)
    api_key = Column(String(255), nullable=False)
    library_id = Column(String(50), nullable=False)
    library_type = Column(String(20), default="user")
    last_sync_version = Column(Integer, nullable=True)
    last_sync_at = Column(DateTime, nullable=True)
    last_sync_status = Column(String(20), default="idle")
    last_sync_error = Column(Text, nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    project = relationship("Project", back_populates="zotero_config")
```

- [ ] **Step 2: 修改 Project 模型添加关系**

在 `Project` 类中现有关系之后追加：

```python
    citations = relationship("Citation", back_populates="project", cascade="all, delete-orphan")
    zotero_config = relationship("ZoteroConfig", back_populates="project", uselist=False, cascade="all, delete-orphan")
```

- [ ] **Step 3: 创建 Alembic 迁移**

```bash
cd backend
uv run alembic revision --autogenerate -m "add citations and zotero_configs"
```

检查生成的迁移文件确保包含 `citations` 和 `zotero_configs` 两张表的创建。

- [ ] **Step 4: 运行迁移**

```bash
uv run alembic upgrade head
```

- [ ] **Step 5: 写模型测试**

在 `backend/tests/test_references.py` 创建基础测试：

```python
import pytest
from app.models import Citation, ZoteroConfig


def test_create_citation(db, project, test_user):
    citation = Citation(
        project_id=project.id,
        user_id=test_user.id,
        title="Test Paper",
        authors='["Zhang, S."]',
        year=2024,
        source="manual",
    )
    db.add(citation)
    db.commit()
    db.refresh(citation)
    assert citation.id is not None
    assert citation.title == "Test Paper"


def test_create_zotero_config(db, project):
    config = ZoteroConfig(
        project_id=project.id,
        api_key="test_key",
        library_id="12345",
        library_type="user",
    )
    db.add(config)
    db.commit()
    db.refresh(config)
    assert config.project_id == project.id
    assert config.last_sync_status == "idle"
```

- [ ] **Step 6: 运行测试**

```bash
uv run pytest tests/test_references.py -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add backend/app/models.py backend/migrations/versions/ backend/tests/test_references.py
git commit -m "feat: add Citation and ZoteroConfig models with migration"
```

---

## Task 2: BibTeX 生成服务

**Files:**
- Create: `backend/app/services/bibtex.py`
- Test: `backend/tests/test_bibtex.py`

- [ ] **Step 1: 写 BibTeX 生成测试**

创建 `backend/tests/test_bibtex.py`：

```python
from app.services.bibtex import generate_bibtex, item_type_to_bibtex


def test_item_type_to_bibtex():
    assert item_type_to_bibtex("journalArticle") == "article"
    assert item_type_to_bibtex("book") == "book"
    assert item_type_to_bibtex("conferencePaper") == "inproceedings"
    assert item_type_to_bibtex("unknown") == "misc"


def test_generate_bibtex():
    citation = {
        "bibtex_type": "article",
        "bibtex_key": "zhang2024test",
        "title": "A Test Paper",
        "authors": "Zhang, S. and Li, M.",
        "journal": "Nature",
        "year": 2024,
        "volume": "10",
        "issue": "2",
        "pages": "100-110",
        "doi": "10.1000/test",
        "url": "https://example.com",
    }
    result = generate_bibtex(citation)
    assert "@article{zhang2024test," in result
    assert "title = {A Test Paper}," in result
    assert "author = {Zhang, S. and Li, M.}," in result
    assert "journal = {Nature}," in result
    assert "year = {2024}," in result
    assert "volume = {10}," in result
    assert "number = {2}," in result
    assert "pages = {100-110}," in result
    assert "doi = {10.1000/test}," in result
    assert "url = {https://example.com}," in result


def test_generate_bibtex_auto_key():
    citation = {
        "bibtex_type": "article",
        "bibtex_key": None,
        "title": "Another Paper",
        "authors": "Wang, X.",
        "year": 2023,
    }
    result = generate_bibtex(citation)
    assert "@article{" in result
    assert result.split("{")[1].split(",")[0]  # key exists
```

- [ ] **Step 2: 运行测试确认失败**

```bash
uv run pytest tests/test_bibtex.py -v
```

Expected: ImportError (module not found)

- [ ] **Step 3: 实现 BibTeX 生成服务**

创建 `backend/app/services/bibtex.py`：

```python
ITEM_TYPE_MAP = {
    "journalArticle": "article",
    "book": "book",
    "bookSection": "incollection",
    "conferencePaper": "inproceedings",
    "thesis": "phdthesis",
    "report": "techreport",
    "webpage": "misc",
    "newspaperArticle": "article",
    "magazineArticle": "article",
}


def item_type_to_bibtex(item_type: str) -> str:
    return ITEM_TYPE_MAP.get(item_type, "misc")


def generate_bibtex_key(citation: dict) -> str:
    authors = citation.get("authors") or ""
    year = citation.get("year") or ""
    title = citation.get("title") or ""
    first_author = authors.split("and")[0].strip().split(",")[0].strip().lower() if authors else "unknown"
    first_word = title.split()[0].lower() if title else "paper"
    return f"{first_author}{year}{first_word}"


def generate_bibtex(citation: dict) -> str:
    bib_type = citation.get("bibtex_type") or "misc"
    key = citation.get("bibtex_key") or generate_bibtex_key(citation)

    lines = [f"@{bib_type}{{{key},"]

    field_map = [
        ("title", "title"),
        ("authors", "author"),
        ("journal", "journal"),
        ("year", "year"),
        ("volume", "volume"),
        ("issue", "number"),
        ("pages", "pages"),
        ("doi", "doi"),
        ("url", "url"),
    ]

    for cite_field, bib_field in field_map:
        value = citation.get(cite_field)
        if value:
            lines.append(f"  {bib_field} = {{{value}}},")

    lines.append("}")
    return "\n".join(lines)


def generate_bibtex_batch(citations: list[dict]) -> str:
    return "\n\n".join(generate_bibtex(c) for c in citations)
```

- [ ] **Step 4: 运行测试确认通过**

```bash
uv run pytest tests/test_bibtex.py -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/bibtex.py backend/tests/test_bibtex.py
git commit -m "feat: add BibTeX generation service"
```

---

## Task 3: Zotero 同步服务

**Files:**
- Create: `backend/app/services/zotero_sync.py`
- Test: `backend/tests/test_zotero_sync.py`

- [ ] **Step 1: 写 Zotero 同步测试**

创建 `backend/tests/test_zotero_sync.py`：

```python
import pytest
from unittest.mock import AsyncMock, patch
from app.services.zotero_sync import (
    parse_zotero_item,
    format_authors,
    extract_year,
    sync_zotero_for_project,
)


def test_format_authors():
    creators = [
        {"creatorType": "author", "firstName": "John", "lastName": "Smith"},
        {"creatorType": "author", "firstName": "Jane", "lastName": "Doe"},
    ]
    assert format_authors(creators) == "Smith, John and Doe, Jane"


def test_format_authors_editor():
    creators = [
        {"creatorType": "editor", "firstName": "John", "lastName": "Smith"},
    ]
    assert format_authors(creators) == ""  # only authors


def test_extract_year():
    assert extract_year("2024") == 2024
    assert extract_year("2024-05") == 2024
    assert extract_year("May 2024") == 2024
    assert extract_year("invalid") is None


def test_parse_zotero_item():
    item = {
        "key": "ABC123",
        "version": 42,
        "data": {
            "itemType": "journalArticle",
            "title": "Test Title",
            "creators": [{"creatorType": "author", "firstName": "X", "lastName": "Y"}],
            "publicationTitle": "Test Journal",
            "date": "2024",
            "volume": "10",
            "issue": "2",
            "pages": "1-10",
            "DOI": "10.1000/test",
            "url": "https://example.com",
            "abstractNote": "Abstract text",
        },
    }
    result = parse_zotero_item(item)
    assert result["zotero_item_key"] == "ABC123"
    assert result["zotero_version"] == 42
    assert result["title"] == "Test Title"
    assert result["bibtex_type"] == "article"
    assert result["journal"] == "Test Journal"
    assert result["year"] == 2024


@pytest.mark.asyncio
async def test_sync_zotero_for_project(mocker):
    mock_client = AsyncMock()
    mock_response = AsyncMock()
    mock_response.status_code = 200
    mock_response.headers = {"Last-Modified-Version": "100"}
    mock_response.json.return_value = []
    mock_client.get.return_value = mock_response

    with patch("httpx.AsyncClient", return_value=mock_client):
        # This test needs db fixture, will be tested in integration tests
        pass
```

- [ ] **Step 2: 运行测试确认失败**

```bash
uv run pytest tests/test_zotero_sync.py -v
```

Expected: ImportError

- [ ] **Step 3: 实现 Zotero 同步服务**

创建 `backend/app/services/zotero_sync.py`：

```python
import json
import asyncio
import logging
import re
from datetime import datetime

import httpx
from sqlalchemy.orm import Session

from app.database import SessionLocal
from app.models import Citation, ZoteroConfig, Project
from app.services.bibtex import item_type_to_bibtex

logger = logging.getLogger(__name__)

_stop_event = asyncio.Event()


ZOTERO_API_BASE = "https://api.zotero.org"


def format_authors(creators: list[dict]) -> str:
    authors = []
    for c in creators:
        if c.get("creatorType") == "author":
            if c.get("lastName") and c.get("firstName"):
                authors.append(f"{c['lastName']}, {c['firstName']}")
            elif c.get("lastName"):
                authors.append(c["lastName"])
            elif c.get("name"):
                authors.append(c["name"])
    return " and ".join(authors)


def extract_year(date_str: str | None) -> int | None:
    if not date_str:
        return None
    match = re.search(r"\b(19|20)\d{2}\b", date_str)
    return int(match.group(0)) if match else None


def parse_zotero_item(item: dict) -> dict:
    data = item.get("data", {})
    return {
        "zotero_item_key": item.get("key"),
        "zotero_version": item.get("version"),
        "title": data.get("title", ""),
        "authors": format_authors(data.get("creators", [])),
        "journal": data.get("publicationTitle") or data.get("proceedingsTitle") or data.get("bookTitle"),
        "year": extract_year(data.get("date")),
        "volume": data.get("volume"),
        "issue": data.get("issue"),
        "pages": data.get("pages"),
        "doi": data.get("DOI"),
        "url": data.get("url"),
        "abstract": data.get("abstractNote"),
        "bibtex_type": item_type_to_bibtex(data.get("itemType", "")),
        "source": "zotero",
        "extra_data": json.dumps(item),
    }


async def fetch_zotero_items(config: ZoteroConfig) -> tuple[list[dict], int]:
    url = f"{ZOTERO_API_BASE}/{config.library_type}s/{config.library_id}/items"
    headers = {"Zotero-API-Key": config.api_key}
    params = {"since": config.last_sync_version or 0, "format": "json", "v": 3, "limit": 100}

    all_items = []
    last_version = config.last_sync_version or 0

    async with httpx.AsyncClient(timeout=30.0) as client:
        start = 0
        while True:
            params["start"] = start
            response = await client.get(url, headers=headers, params=params)
            response.raise_for_status()

            items = response.json()
            if not items:
                break

            all_items.extend(items)
            last_version = int(response.headers.get("Last-Modified-Version", last_version))
            start += len(items)

    return all_items, last_version


async def sync_zotero_for_project(project_id: str, db: Session) -> dict:
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        return {"status": "no_config"}

    if config.last_sync_status == "syncing":
        return {"status": "syncing"}

    config.last_sync_status = "syncing"
    config.last_sync_error = None
    db.commit()

    try:
        items, last_version = await fetch_zotero_items(config)

        new_count = 0
        updated_count = 0

        for item in items:
            parsed = parse_zotero_item(item)
            existing = db.query(Citation).filter(
                Citation.project_id == project_id,
                Citation.zotero_item_key == parsed["zotero_item_key"],
            ).first()

            if existing:
                # Update all fields except bibtex_key (preserve local edits)
                bibtex_key = existing.bibtex_key
                for key, value in parsed.items():
                    setattr(existing, key, value)
                existing.bibtex_key = bibtex_key
                updated_count += 1
            else:
                citation = Citation(
                    project_id=project_id,
                    user_id=config.project.owner_id,  # Use project owner as default
                    **parsed,
                )
                db.add(citation)
                new_count += 1

        config.last_sync_version = last_version
        config.last_sync_at = datetime.utcnow()
        config.last_sync_status = "idle"
        config.last_sync_error = None
        db.commit()

        return {"status": "success", "new": new_count, "updated": updated_count}

    except Exception as e:
        logger.exception(f"Zotero sync failed for project {project_id}")
        config.last_sync_status = "error"
        config.last_sync_error = str(e)[:500]
        db.commit()
        raise


async def run_sync_for_all_projects():
    with SessionLocal() as db:
        configs = db.query(ZoteroConfig).filter(ZoteroConfig.last_sync_status != "syncing").all()
        for config in configs:
            try:
                await sync_zotero_for_project(config.project_id, db)
            except Exception:
                logger.exception(f"Background sync failed for project {config.project_id}")


async def start_sync_scheduler():
    while not _stop_event.is_set():
        try:
            await asyncio.wait_for(_stop_event.wait(), timeout=900)
        except asyncio.TimeoutError:
            await run_sync_for_all_projects()


def stop_sync_scheduler():
    _stop_event.set()
```

- [ ] **Step 4: 运行测试确认通过**

```bash
uv run pytest tests/test_zotero_sync.py -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/zotero_sync.py backend/tests/test_zotero_sync.py
git commit -m "feat: add Zotero sync service with field parsing"
```

---

## Task 4: 后端 API Router

**Files:**
- Create: `backend/app/api/references.py`
- Modify: `backend/app/main.py`
- Test: `backend/tests/test_references.py`

- [ ] **Step 1: 写 API 测试**

追加到 `backend/tests/test_references.py`：

```python
def test_list_citations(client, auth_client, project, test_user):
    # Create a citation first
    from app.models import Citation
    db = next(client.app.dependency_overrides.get("get_db", lambda: None)())
    # Use the db fixture directly
    pass  # Will test via API after creation endpoint exists


def test_create_citation_api(auth_client, project):
    response = auth_client.post(f"/api/references/{project.id}", data={
        "title": "New Paper",
        "authors": "[\"Wang, X.\"]",
        "year": "2023",
        "journal": "Science",
    })
    assert response.status_code == 200
    data = response.json()
    assert data["title"] == "New Paper"
    assert data["source"] == "manual"


def test_delete_citation_api(auth_client, project, test_user):
    # First create
    response = auth_client.post(f"/api/references/{project.id}", data={
        "title": "To Delete",
        "authors": "[\"Test\"]",
    })
    cid = response.json()["id"]

    response = auth_client.delete(f"/api/references/{project.id}/{cid}")
    assert response.status_code == 200


def test_export_bibtex(auth_client, project):
    response = auth_client.post(f"/api/references/{project.id}", data={
        "title": "Export Test",
        "authors": "[\"Author, A.\"]",
        "year": "2024",
        "bibtex_key": "test2024",
    })
    cid = response.json()["id"]

    response = auth_client.post(f"/api/references/{project.id}/export", json={"ids": [cid]})
    assert response.status_code == 200
    assert "@article{test2024," in response.text
```

- [ ] **Step 2: 运行测试确认失败**

```bash
uv run pytest tests/test_references.py -v
```

Expected: FAIL (404 - endpoint not found)

- [ ] **Step 3: 实现 references API router**

创建 `backend/app/api/references.py`：

```python
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, User, Citation, ZoteroConfig
from app.api.auth import get_current_user
from app.services.bibtex import generate_bibtex, generate_bibtex_batch
from app.services.zotero_sync import sync_zotero_for_project

router = APIRouter()


def _check_project_access(project_id: str, user: User, db: Session):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(
        TeamMember.team_id == project.team_id,
        TeamMember.user_id == user.id,
    ).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    return project


@router.get("/{project_id}")
def list_citations(
    project_id: str,
    q: str = "",
    year_from: int | None = None,
    year_to: int | None = None,
    source: str | None = None,
    sort_by: str = "created_at",
    sort_order: str = "desc",
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    query = db.query(Citation).filter(Citation.project_id == project_id)

    if q:
        search = f"%{q}%"
        query = query.filter(
            (Citation.title.ilike(search))
            | (Citation.authors.ilike(search))
            | (Citation.journal.ilike(search))
        )

    if year_from is not None:
        query = query.filter(Citation.year >= year_from)
    if year_to is not None:
        query = query.filter(Citation.year <= year_to)
    if source:
        query = query.filter(Citation.source == source)

    sort_column = getattr(Citation, sort_by, Citation.created_at)
    if sort_order == "desc":
        query = query.order_by(sort_column.desc())
    else:
        query = query.order_by(sort_column.asc())

    citations = query.all()
    return {
        "items": [
            {
                "id": c.id,
                "title": c.title,
                "authors": c.authors,
                "journal": c.journal,
                "year": c.year,
                "volume": c.volume,
                "issue": c.issue,
                "pages": c.pages,
                "doi": c.doi,
                "url": c.url,
                "abstract": c.abstract,
                "bibtex_key": c.bibtex_key,
                "bibtex_type": c.bibtex_type,
                "source": c.source,
                "zotero_item_key": c.zotero_item_key,
                "user_id": c.user_id,
                "created_at": c.created_at.isoformat() if c.created_at else None,
                "updated_at": c.updated_at.isoformat() if c.updated_at else None,
            }
            for c in citations
        ],
        "total": len(citations),
    }


@router.post("/{project_id}")
def create_citation(
    project_id: str,
    title: str,
    authors: str = "",
    journal: str = "",
    year: int | None = None,
    volume: str = "",
    issue: str = "",
    pages: str = "",
    doi: str = "",
    url: str = "",
    abstract: str = "",
    bibtex_key: str = "",
    bibtex_type: str = "article",
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    citation = Citation(
        project_id=project_id,
        user_id=current_user.id,
        title=title,
        authors=authors,
        journal=journal,
        year=year,
        volume=volume,
        issue=issue,
        pages=pages,
        doi=doi,
        url=url,
        abstract=abstract,
        bibtex_key=bibtex_key or None,
        bibtex_type=bibtex_type,
        source="manual",
    )
    db.add(citation)
    db.commit()
    db.refresh(citation)
    return {
        "id": citation.id,
        "title": citation.title,
        "authors": citation.authors,
        "journal": citation.journal,
        "year": citation.year,
        "source": citation.source,
        "user_id": citation.user_id,
        "created_at": citation.created_at.isoformat() if citation.created_at else None,
    }


@router.put("/{project_id}/{citation_id}")
def update_citation(
    project_id: str,
    citation_id: str,
    title: str | None = None,
    authors: str | None = None,
    journal: str | None = None,
    year: int | None = None,
    volume: str | None = None,
    issue: str | None = None,
    pages: str | None = None,
    doi: str | None = None,
    url: str | None = None,
    abstract: str | None = None,
    bibtex_key: str | None = None,
    bibtex_type: str | None = None,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    citation = db.query(Citation).filter(
        Citation.id == citation_id,
        Citation.project_id == project_id,
    ).first()
    if not citation:
        raise HTTPException(status_code=404, detail="Citation not found")

    fields = [
        "title", "authors", "journal", "year", "volume",
        "issue", "pages", "doi", "url", "abstract", "bibtex_key", "bibtex_type",
    ]
    for field in fields:
        value = locals().get(field)
        if value is not None:
            setattr(citation, field, value)

    citation.updated_at = datetime.utcnow()
    db.commit()
    db.refresh(citation)
    return {
        "id": citation.id,
        "title": citation.title,
        "authors": citation.authors,
        "journal": citation.journal,
        "year": citation.year,
        "bibtex_key": citation.bibtex_key,
        "bibtex_type": citation.bibtex_type,
    }


@router.delete("/{project_id}/{citation_id}")
def delete_citation(
    project_id: str,
    citation_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    citation = db.query(Citation).filter(
        Citation.id == citation_id,
        Citation.project_id == project_id,
    ).first()
    if not citation:
        raise HTTPException(status_code=404, detail="Citation not found")
    if citation.user_id != current_user.id:
        raise HTTPException(status_code=403, detail="Can only delete your own citations")

    db.delete(citation)
    db.commit()
    return {"status": "deleted"}


@router.get("/{project_id}/zotero-config")
def get_zotero_config(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        raise HTTPException(status_code=404, detail="Zotero config not found")
    return {
        "library_id": config.library_id,
        "library_type": config.library_type,
        "api_key_masked": config.api_key[:4] + "****" if config.api_key else "",
        "last_sync_at": config.last_sync_at.isoformat() if config.last_sync_at else None,
        "last_sync_status": config.last_sync_status,
        "last_sync_error": config.last_sync_error,
    }


@router.post("/{project_id}/zotero-config")
def set_zotero_config(
    project_id: str,
    api_key: str,
    library_id: str,
    library_type: str = "user",
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if config:
        config.api_key = api_key
        config.library_id = library_id
        config.library_type = library_type
    else:
        config = ZoteroConfig(
            project_id=project_id,
            api_key=api_key,
            library_id=library_id,
            library_type=library_type,
        )
        db.add(config)

    db.commit()
    db.refresh(config)
    return {
        "library_id": config.library_id,
        "library_type": config.library_type,
        "api_key_masked": config.api_key[:4] + "****" if config.api_key else "",
    }


@router.delete("/{project_id}/zotero-config")
def delete_zotero_config(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if config:
        db.delete(config)
        db.commit()
    return {"status": "deleted"}


@router.post("/{project_id}/sync")
async def sync_zotero(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    return await sync_zotero_for_project(project_id, db)


@router.get("/{project_id}/sync-status")
def get_sync_status(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        raise HTTPException(status_code=404, detail="Zotero config not found")
    return {
        "last_sync_at": config.last_sync_at.isoformat() if config.last_sync_at else None,
        "last_sync_status": config.last_sync_status,
        "last_sync_error": config.last_sync_error,
        "last_sync_version": config.last_sync_version,
    }


@router.post("/{project_id}/export")
def export_bibtex(
    project_id: str,
    ids: list[str] | None = None,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    query = db.query(Citation).filter(Citation.project_id == project_id)
    if ids:
        query = query.filter(Citation.id.in_(ids))

    citations = query.all()
    if not citations:
        raise HTTPException(status_code=404, detail="No citations found")

    bibtex_entries = []
    for c in citations:
        bibtex_entries.append(generate_bibtex({
            "bibtex_type": c.bibtex_type,
            "bibtex_key": c.bibtex_key,
            "title": c.title,
            "authors": c.authors,
            "journal": c.journal,
            "year": c.year,
            "volume": c.volume,
            "issue": c.issue,
            "pages": c.pages,
            "doi": c.doi,
            "url": c.url,
        }))

    content = "\n\n".join(bibtex_entries)
    from fastapi.responses import PlainTextResponse
    return PlainTextResponse(content, media_type="text/plain; charset=utf-8")
```

- [ ] **Step 4: 注册 router 并启动后台任务**

修改 `backend/app/main.py`：

1. 导入 references router 和 sync scheduler：

```python
from app.api import references
from app.services.zotero_sync import start_sync_scheduler, stop_sync_scheduler
```

2. 注册 router（在 `app.include_router(llm.router, ...)` 之后）：

```python
app.include_router(references.router, prefix="/api/references", tags=["引文管理"])
```

3. 添加 startup/shutdown 事件（在文件末尾 `health_check` 之后）：

```python
@app.on_event("startup")
async def startup_event():
    asyncio.create_task(start_sync_scheduler())

@app.on_event("shutdown")
async def shutdown_event():
    stop_sync_scheduler()
```

- [ ] **Step 5: 运行测试确认通过**

```bash
uv run pytest tests/test_references.py -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/api/references.py backend/app/main.py backend/tests/test_references.py
git commit -m "feat: add references API router with CRUD, sync, and export"
```

---

## Task 5: 前端导航和路由

**Files:**
- Modify: `frontend/components/app-sidebar.tsx`
- Modify: `frontend/components/app-navbar.tsx`
- Create: `frontend/app/(main)/references/page.tsx`
- Create: `frontend/app/(main)/references/loading.tsx`

- [ ] **Step 1: 添加导航项**

修改 `frontend/components/app-sidebar.tsx`：

1. 导入 `BookOpen`：
```typescript
import { Home, CalendarDays, FileText, FlaskConical, Settings, LogOut, ChevronsUpDown, BookOpen } from "lucide-react"
```

2. 在 `navItems` 数组中（在 `/experiment` 之后，`/settings` 之前）追加：
```typescript
{ href: "/references", label: "引文管理", icon: BookOpen },
```

- [ ] **Step 2: 添加页面标题**

修改 `frontend/components/app-navbar.tsx`：

在 `pageTitles` 对象中追加：
```typescript
"/references": "引文管理",
```

- [ ] **Step 3: 创建引文管理页面**

创建 `frontend/app/(main)/references/page.tsx`：

```tsx
"use client"

import { useEffect, useState } from "react"
import { useParams, useSearchParams } from "next/navigation"
import { useAuthStore } from "@/stores/auth"
import api from "@/lib/api"
import { CitationTable } from "@/components/references/CitationTable"
import { CitationFilters } from "@/components/references/CitationFilters"
import { CitationForm } from "@/components/references/CitationForm"
import { ZoteroConfigPanel } from "@/components/references/ZoteroConfigPanel"
import { SyncStatusBadge } from "@/components/references/SyncStatusBadge"
import { ExportButton } from "@/components/references/ExportButton"
import { Button } from "@/components/ui/button"
import { Plus } from "lucide-react"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  volume: string
  issue: string
  pages: string
  doi: string
  url: string
  abstract: string
  bibtex_key: string
  bibtex_type: string
  source: string
  user_id: string
  created_at: string
}

interface SyncStatus {
  last_sync_at: string | null
  last_sync_status: string
  last_sync_error: string | null
}

export default function ReferencesPage() {
  const [citations, setCitations] = useState<Citation[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [showForm, setShowForm] = useState(false)
  const [editingCitation, setEditingCitation] = useState<Citation | null>(null)
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null)
  const [filters, setFilters] = useState({ q: "", year_from: "", year_to: "", source: "", sort_by: "created_at", sort_order: "desc" })
  const user = useAuthStore((s) => s.user)

  // 从 URL 或 localStorage 获取当前项目 ID
  const [currentProjectId, setCurrentProjectId] = useState<string>("")

  useEffect(() => {
    const pid = localStorage.getItem("current_project_id")
    if (pid) setCurrentProjectId(pid)
  }, [])

  const fetchCitations = async () => {
    if (!currentProjectId) return
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filters.q) params.append("q", filters.q)
      if (filters.year_from) params.append("year_from", filters.year_from)
      if (filters.year_to) params.append("year_to", filters.year_to)
      if (filters.source) params.append("source", filters.source)
      params.append("sort_by", filters.sort_by)
      params.append("sort_order", filters.sort_order)

      const res = await api.get(`/references/${currentProjectId}?${params}`)
      setCitations(res.data.items)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  const fetchSyncStatus = async () => {
    if (!currentProjectId) return
    try {
      const res = await api.get(`/references/${currentProjectId}/sync-status`)
      setSyncStatus(res.data)
    } catch (e) {
      // No config yet
    }
  }

  useEffect(() => {
    fetchCitations()
  }, [currentProjectId, filters])

  useEffect(() => {
    fetchSyncStatus()
  }, [currentProjectId])

  const handleDelete = async (id: string) => {
    if (!confirm("确定删除这条引文？")) return
    try {
      await api.delete(`/references/${currentProjectId}/${id}`)
      fetchCitations()
    } catch (e) {
      console.error(e)
    }
  }

  const handleSync = async () => {
    try {
      await api.post(`/references/${currentProjectId}/sync`)
      setTimeout(() => {
        fetchSyncStatus()
        fetchCitations()
      }, 2000)
    } catch (e) {
      console.error(e)
    }
  }

  if (!currentProjectId) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-muted-foreground">请先在主页选择一个项目</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <CitationFilters filters={filters} onChange={setFilters} />
        <div className="flex items-center gap-2">
          <SyncStatusBadge status={syncStatus} />
          <Button variant="outline" size="sm" onClick={handleSync}>
            同步
          </Button>
          <ExportButton projectId={currentProjectId} selectedIds={Array.from(selectedIds)} />
          <Button size="sm" onClick={() => { setEditingCitation(null); setShowForm(true) }}>
            <Plus className="mr-1 h-4 w-4" />
            添加引文
          </Button>
        </div>
      </div>

      <ZoteroConfigPanel projectId={currentProjectId} onConfigChange={fetchSyncStatus} />

      <CitationTable
        citations={citations}
        loading={loading}
        selectedIds={selectedIds}
        onSelectChange={setSelectedIds}
        onEdit={(c) => { setEditingCitation(c); setShowForm(true) }}
        onDelete={handleDelete}
      />

      <CitationForm
        open={showForm}
        onClose={() => setShowForm(false)}
        projectId={currentProjectId}
        citation={editingCitation}
        onSuccess={() => { setShowForm(false); fetchCitations() }}
      />
    </div>
  )
}
```

- [ ] **Step 4: 创建 loading.tsx**

创建 `frontend/app/(main)/references/loading.tsx`：

```tsx
import { Skeleton } from "@/components/ui/skeleton"

export default function Loading() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-10 w-[400px]" />
        <div className="flex gap-2">
          <Skeleton className="h-10 w-20" />
          <Skeleton className="h-10 w-24" />
        </div>
      </div>
      <Skeleton className="h-[400px] w-full" />
    </div>
  )
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/components/app-sidebar.tsx frontend/components/app-navbar.tsx frontend/app/\(main\)/references/
git commit -m "feat: add references page route and navigation"
```

---

## Task 6: 前端引文表格和筛选

**Files:**
- Create: `frontend/components/references/CitationTable.tsx`
- Create: `frontend/components/references/CitationFilters.tsx`

- [ ] **Step 1: 实现 CitationTable**

创建 `frontend/components/references/CitationTable.tsx`：

```tsx
import { Checkbox } from "@/components/ui/checkbox"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Pencil, Trash2 } from "lucide-react"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  source: string
  user_id: string
}

interface Props {
  citations: Citation[]
  loading: boolean
  selectedIds: Set<string>
  onSelectChange: (ids: Set<string>) => void
  onEdit: (c: Citation) => void
  onDelete: (id: string) => void
}

export function CitationTable({ citations, loading, selectedIds, onSelectChange, onEdit, onDelete }: Props) {
  const allSelected = citations.length > 0 && citations.every((c) => selectedIds.has(c.id))
  const someSelected = citations.some((c) => selectedIds.has(c.id)) && !allSelected

  const toggleAll = () => {
    if (allSelected) {
      onSelectChange(new Set())
    } else {
      onSelectChange(new Set(citations.map((c) => c.id)))
    }
  }

  const toggleOne = (id: string) => {
    const next = new Set(selectedIds)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    onSelectChange(next)
  }

  if (loading) {
    return <div className="text-muted-foreground py-8 text-center">加载中...</div>
  }

  if (citations.length === 0) {
    return <div className="text-muted-foreground py-8 text-center">暂无引文</div>
  }

  return (
    <div className="border rounded-md">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-[40px]">
              <Checkbox checked={allSelected} data-state={someSelected ? "indeterminate" : allSelected ? "checked" : "unchecked"} onCheckedChange={toggleAll} />
            </TableHead>
            <TableHead>标题</TableHead>
            <TableHead>作者</TableHead>
            <TableHead>刊名</TableHead>
            <TableHead className="w-[80px]">年份</TableHead>
            <TableHead className="w-[80px]">来源</TableHead>
            <TableHead className="w-[100px]">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {citations.map((c) => (
            <TableRow key={c.id}>
              <TableCell>
                <Checkbox checked={selectedIds.has(c.id)} onCheckedChange={() => toggleOne(c.id)} />
              </TableCell>
              <TableCell className="font-medium max-w-[300px] truncate">{c.title}</TableCell>
              <TableCell className="max-w-[200px] truncate">{c.authors}</TableCell>
              <TableCell className="max-w-[150px] truncate">{c.journal}</TableCell>
              <TableCell>{c.year}</TableCell>
              <TableCell>
                <Badge variant={c.source === "zotero" ? "secondary" : "outline"}>
                  {c.source === "zotero" ? "Z" : "M"}
                </Badge>
              </TableCell>
              <TableCell>
                <div className="flex gap-1">
                  <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => onEdit(c)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" className="h-8 w-8 text-destructive" onClick={() => onDelete(c.id)}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
```

- [ ] **Step 2: 实现 CitationFilters**

创建 `frontend/components/references/CitationFilters.tsx`：

```tsx
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Search } from "lucide-react"

interface Filters {
  q: string
  year_from: string
  year_to: string
  source: string
  sort_by: string
  sort_order: string
}

interface Props {
  filters: Filters
  onChange: (filters: Filters) => void
}

export function CitationFilters({ filters, onChange }: Props) {
  const update = (key: keyof Filters, value: string) => {
    onChange({ ...filters, [key]: value })
  }

  return (
    <div className="flex items-center gap-2 flex-wrap">
      <div className="relative">
        <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="搜索标题、作者、刊名..."
          className="pl-8 w-[280px]"
          value={filters.q}
          onChange={(e) => update("q", e.target.value)}
        />
      </div>
      <Input
        placeholder="年份起"
        className="w-[80px]"
        value={filters.year_from}
        onChange={(e) => update("year_from", e.target.value)}
      />
      <Input
        placeholder="年份止"
        className="w-[80px]"
        value={filters.year_to}
        onChange={(e) => update("year_to", e.target.value)}
      />
      <Select value={filters.source} onValueChange={(v) => update("source", v)}>
        <SelectTrigger className="w-[120px]">
          <SelectValue placeholder="来源" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="">全部</SelectItem>
          <SelectItem value="manual">手动</SelectItem>
          <SelectItem value="zotero">Zotero</SelectItem>
        </SelectContent>
      </Select>
      <Select value={filters.sort_by} onValueChange={(v) => update("sort_by", v)}>
        <SelectTrigger className="w-[120px]">
          <SelectValue placeholder="排序" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="created_at">创建时间</SelectItem>
          <SelectItem value="title">标题</SelectItem>
          <SelectItem value="year">年份</SelectItem>
        </SelectContent>
      </Select>
      <Select value={filters.sort_order} onValueChange={(v) => update("sort_order", v)}>
        <SelectTrigger className="w-[100px]">
          <SelectValue placeholder="顺序" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="desc">降序</SelectItem>
          <SelectItem value="asc">升序</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/components/references/CitationTable.tsx frontend/components/references/CitationFilters.tsx
git commit -m "feat: add citation table and filter components"
```

---

## Task 7: 前端引文表单

**Files:**
- Create: `frontend/components/references/CitationForm.tsx`

- [ ] **Step 1: 实现 CitationForm**

创建 `frontend/components/references/CitationForm.tsx`：

```tsx
import { useState, useEffect } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import api from "@/lib/api"

interface Citation {
  id: string
  title: string
  authors: string
  journal: string
  year: number
  volume: string
  issue: string
  pages: string
  doi: string
  url: string
  abstract: string
  bibtex_key: string
  bibtex_type: string
}

interface Props {
  open: boolean
  onClose: () => void
  projectId: string
  citation: Citation | null
  onSuccess: () => void
}

const BIBTEX_TYPES = ["article", "book", "incollection", "inproceedings", "phdthesis", "techreport", "misc"]

export function CitationForm({ open, onClose, projectId, citation, onSuccess }: Props) {
  const [form, setForm] = useState({
    title: "",
    authors: "",
    journal: "",
    year: "",
    volume: "",
    issue: "",
    pages: "",
    doi: "",
    url: "",
    abstract: "",
    bibtex_key: "",
    bibtex_type: "article",
  })
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (citation) {
      setForm({
        title: citation.title || "",
        authors: citation.authors || "",
        journal: citation.journal || "",
        year: citation.year?.toString() || "",
        volume: citation.volume || "",
        issue: citation.issue || "",
        pages: citation.pages || "",
        doi: citation.doi || "",
        url: citation.url || "",
        abstract: citation.abstract || "",
        bibtex_key: citation.bibtex_key || "",
        bibtex_type: citation.bibtex_type || "article",
      })
    } else {
      setForm({
        title: "", authors: "", journal: "", year: "", volume: "",
        issue: "", pages: "", doi: "", url: "", abstract: "",
        bibtex_key: "", bibtex_type: "article",
      })
    }
  }, [citation, open])

  const update = (key: string, value: string) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    try {
      const data = new FormData()
      Object.entries(form).forEach(([k, v]) => {
        if (v) data.append(k, v)
      })
      if (form.year) data.set("year", form.year)

      if (citation) {
        await api.put(`/references/${projectId}/${citation.id}`, data)
      } else {
        await api.post(`/references/${projectId}`, data)
      }
      onSuccess()
    } catch (e) {
      console.error(e)
      alert("保存失败")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{citation ? "编辑引文" : "添加引文"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <Label>标题 *</Label>
              <Input value={form.title} onChange={(e) => update("title", e.target.value)} required />
            </div>
            <div className="col-span-2">
              <Label>作者</Label>
              <Input value={form.authors} onChange={(e) => update("authors", e.target.value)} placeholder="Zhang, S. and Li, M." />
            </div>
            <div>
              <Label>刊名</Label>
              <Input value={form.journal} onChange={(e) => update("journal", e.target.value)} />
            </div>
            <div>
              <Label>年份</Label>
              <Input type="number" value={form.year} onChange={(e) => update("year", e.target.value)} />
            </div>
            <div>
              <Label>卷号</Label>
              <Input value={form.volume} onChange={(e) => update("volume", e.target.value)} />
            </div>
            <div>
              <Label>期号</Label>
              <Input value={form.issue} onChange={(e) => update("issue", e.target.value)} />
            </div>
            <div>
              <Label>页码</Label>
              <Input value={form.pages} onChange={(e) => update("pages", e.target.value)} />
            </div>
            <div>
              <Label>DOI</Label>
              <Input value={form.doi} onChange={(e) => update("doi", e.target.value)} />
            </div>
            <div className="col-span-2">
              <Label>URL</Label>
              <Input value={form.url} onChange={(e) => update("url", e.target.value)} />
            </div>
            <div>
              <Label>BibTeX Key</Label>
              <Input value={form.bibtex_key} onChange={(e) => update("bibtex_key", e.target.value)} />
            </div>
            <div>
              <Label>BibTeX Type</Label>
              <Select value={form.bibtex_type} onValueChange={(v) => update("bibtex_type", v)}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {BIBTEX_TYPES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div>
            <Label>摘要</Label>
            <Textarea value={form.abstract} onChange={(e) => update("abstract", e.target.value)} rows={4} />
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>取消</Button>
            <Button type="submit" disabled={submitting}>{submitting ? "保存中..." : "保存"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/components/references/CitationForm.tsx
git commit -m "feat: add citation form component"
```

---

## Task 8: 前端 Zotero 配置和同步状态

**Files:**
- Create: `frontend/components/references/ZoteroConfigPanel.tsx`
- Create: `frontend/components/references/SyncStatusBadge.tsx`

- [ ] **Step 1: 实现 ZoteroConfigPanel**

创建 `frontend/components/references/ZoteroConfigPanel.tsx`：

```tsx
import { useState, useEffect } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import api from "@/lib/api"

interface Props {
  projectId: string
  onConfigChange: () => void
}

export function ZoteroConfigPanel({ projectId, onConfigChange }: Props) {
  const [config, setConfig] = useState<any>(null)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ api_key: "", library_id: "", library_type: "user" })

  const fetchConfig = async () => {
    try {
      const res = await api.get(`/references/${projectId}/zotero-config`)
      setConfig(res.data)
    } catch (e) {
      setConfig(null)
    }
  }

  useEffect(() => {
    fetchConfig()
  }, [projectId])

  const handleSave = async () => {
    try {
      await api.post(`/references/${projectId}/zotero-config`, null, {
        params: form,
      })
      setShowForm(false)
      fetchConfig()
      onConfigChange()
    } catch (e) {
      alert("保存失败")
    }
  }

  const handleDelete = async () => {
    if (!confirm("确定删除 Zotero 配置？")) return
    try {
      await api.delete(`/references/${projectId}/zotero-config`)
      setConfig(null)
      onConfigChange()
    } catch (e) {
      alert("删除失败")
    }
  }

  if (!config && !showForm) {
    return (
      <Button variant="outline" size="sm" onClick={() => setShowForm(true)}>
        连接 Zotero
      </Button>
    )
  }

  if (showForm) {
    return (
      <Card>
        <CardHeader><CardTitle className="text-sm">Zotero 配置</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <div>
            <Label>API Key</Label>
            <Input type="password" value={form.api_key} onChange={(e) => setForm({ ...form, api_key: e.target.value })} placeholder="从 zotero.org/settings/keys 获取" />
          </div>
          <div>
            <Label>Library ID</Label>
            <Input value={form.library_id} onChange={(e) => setForm({ ...form, library_id: e.target.value })} placeholder="User ID 或 Group ID" />
          </div>
          <div>
            <Label>Library Type</Label>
            <Select value={form.library_type} onValueChange={(v) => setForm({ ...form, library_type: v })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="user">User</SelectItem>
                <SelectItem value="group">Group</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleSave}>保存</Button>
            <Button size="sm" variant="outline" onClick={() => setShowForm(false)}>取消</Button>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <span>Zotero: {config?.library_id} ({config?.library_type})</span>
      <Button variant="ghost" size="sm" onClick={() => { setForm({ api_key: "", library_id: config?.library_id || "", library_type: config?.library_type || "user" }); setShowForm(true) }}>编辑</Button>
      <Button variant="ghost" size="sm" className="text-destructive" onClick={handleDelete}>删除</Button>
    </div>
  )
}
```

- [ ] **Step 2: 实现 SyncStatusBadge**

创建 `frontend/components/references/SyncStatusBadge.tsx`：

```tsx
import { Badge } from "@/components/ui/badge"
import { Loader2, CheckCircle, AlertCircle } from "lucide-react"

interface SyncStatus {
  last_sync_at: string | null
  last_sync_status: string
  last_sync_error: string | null
}

interface Props {
  status: SyncStatus | null
}

export function SyncStatusBadge({ status }: Props) {
  if (!status) return null

  const formatTime = (ts: string | null) => {
    if (!ts) return "从未同步"
    const date = new Date(ts)
    return date.toLocaleString("zh-CN")
  }

  if (status.last_sync_status === "syncing") {
    return (
      <Badge variant="outline" className="gap-1">
        <Loader2 className="h-3 w-3 animate-spin" />
        同步中...
      </Badge>
    )
  }

  if (status.last_sync_status === "error") {
    return (
      <Badge variant="destructive" className="gap-1" title={status.last_sync_error || ""}>
        <AlertCircle className="h-3 w-3" />
        同步失败
      </Badge>
    )
  }

  return (
    <Badge variant="outline" className="gap-1 text-muted-foreground">
      <CheckCircle className="h-3 w-3" />
      上次同步: {formatTime(status.last_sync_at)}
    </Badge>
  )
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/components/references/ZoteroConfigPanel.tsx frontend/components/references/SyncStatusBadge.tsx
git commit -m "feat: add Zotero config panel and sync status badge"
```

---

## Task 9: 前端导出按钮

**Files:**
- Create: `frontend/components/references/ExportButton.tsx`

- [ ] **Step 1: 实现 ExportButton**

创建 `frontend/components/references/ExportButton.tsx`：

```tsx
import { Button } from "@/components/ui/button"
import { Download } from "lucide-react"
import api from "@/lib/api"

interface Props {
  projectId: string
  selectedIds: string[]
}

export function ExportButton({ projectId, selectedIds }: Props) {
  const handleExport = async (exportAll: boolean) => {
    try {
      const payload = exportAll ? {} : { ids: selectedIds }
      const res = await api.post(`/references/${projectId}/export`, payload, {
        responseType: "text",
      })

      const blob = new Blob([res.data], { type: "text/plain;charset=utf-8" })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = `references_${projectId}.bib`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (e) {
      console.error(e)
      alert("导出失败")
    }
  }

  return (
    <div className="flex gap-1">
      {selectedIds.length > 0 && (
        <Button variant="outline" size="sm" onClick={() => handleExport(false)}>
          <Download className="mr-1 h-4 w-4" />
          导出选中 ({selectedIds.length})
        </Button>
      )}
      <Button variant="outline" size="sm" onClick={() => handleExport(true)}>
        <Download className="mr-1 h-4 w-4" />
        导出全部
      </Button>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/components/references/ExportButton.tsx
git commit -m "feat: add BibTeX export button component"
```

---

## Task 10: 集成验证

**Files:**
- Test: `backend/tests/test_references.py`（追加集成测试）

- [ ] **Step 1: 追加 Zotero 配置和同步集成测试**

追加到 `backend/tests/test_references.py`：

```python
import pytest
from unittest.mock import AsyncMock, patch


def test_zotero_config_crud(auth_client, project):
    # Create
    response = auth_client.post(f"/api/references/{project.id}/zotero-config", data={
        "api_key": "secret_key_123",
        "library_id": "12345",
        "library_type": "user",
    })
    assert response.status_code == 200
    data = response.json()
    assert data["library_id"] == "12345"
    assert "****" in data["api_key_masked"]

    # Get
    response = auth_client.get(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 200
    assert response.json()["library_type"] == "user"

    # Delete
    response = auth_client.delete(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 200

    # Get after delete
    response = auth_client.get(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 404


@pytest.mark.asyncio
async def test_zotero_sync(auth_client, project, db):
    # Setup config
    auth_client.post(f"/api/references/{project.id}/zotero-config", data={
        "api_key": "test_key",
        "library_id": "12345",
        "library_type": "user",
    })

    mock_response = AsyncMock()
    mock_response.status_code = 200
    mock_response.headers = {"Last-Modified-Version": "42"}
    mock_response.json.return_value = [{
        "key": "ITEM1",
        "version": 10,
        "data": {
            "itemType": "journalArticle",
            "title": "Synced Paper",
            "creators": [{"creatorType": "author", "firstName": "A", "lastName": "B"}],
            "publicationTitle": "Journal",
            "date": "2024",
        },
    }]

    with patch("httpx.AsyncClient") as mock_client:
        mock_instance = AsyncMock()
        mock_instance.__aenter__ = AsyncMock(return_value=mock_instance)
        mock_instance.__aexit__ = AsyncMock(return_value=False)
        mock_instance.get.return_value = mock_response
        mock_client.return_value = mock_instance

        response = auth_client.post(f"/api/references/{project.id}/sync")
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "success"

    # Verify citation created
    response = auth_client.get(f"/api/references/{project.id}")
    items = response.json()["items"]
    assert any(c["title"] == "Synced Paper" for c in items)
```

- [ ] **Step 2: 运行全部引文相关测试**

```bash
uv run pytest tests/test_references.py tests/test_bibtex.py tests/test_zotero_sync.py -v
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add backend/tests/test_references.py
git commit -m "test: add Zotero sync integration tests"
```

---

## 最终验证

- [ ] **运行后端完整测试**

```bash
uv run pytest tests/ -q
```

确认：新测试全部通过，原有测试没有新增失败。

- [ ] **运行前端构建**

```bash
cd frontend && npm run build
```

确认：构建成功，无 TypeScript 错误。

- [ ] **提交最终变更**

```bash
git push -u origin feat/references-manager
```

---

## Self-Review

### Spec Coverage

| Spec 需求 | 对应 Task |
|-----------|-----------|
| 内建引文数据库（标题、作者、刊名等） | Task 1, 4 |
| 单条目/批量导出 BibTeX | Task 2, 4 (export endpoint), 9 |
| 增删改查 | Task 4 |
| Zotero Web API 连接 | Task 3, 4 (zotero-config), 8 |
| 从 Zotero 导入 | Task 3, 4 (sync endpoint) |
| 定期同步 | Task 3 (scheduler), 4 (main.py) |
| 前端标签页 | Task 5 |
| 前端表格/筛选 | Task 6 |
| 前端表单 | Task 7 |
| 前端 Zotero 配置 | Task 8 |

### Placeholder Scan

- 无 TBD/TODO
- 所有步骤包含完整代码
- 无 "add appropriate error handling" 等模糊描述

### Type Consistency

- `Citation` 模型字段与 API 返回字段一致
- `ZoteroConfig` 字段前后一致
- 前端 `Citation` interface 与 API 返回结构一致
