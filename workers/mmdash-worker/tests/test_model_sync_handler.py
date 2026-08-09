from __future__ import annotations

import asyncio
from typing import Any

from mmdash_worker.jobs.handlers import HandlerContext
from mmdash_worker.model_sync.handler import ModelNotionHandler


class ExportClient:
    def __init__(self, exported: dict[str, Any]) -> None:
        self.exported = exported
        self.job_ids: list[str] = []

    def get_model_notion_export(self, job_id: str) -> dict[str, Any]:
        self.job_ids.append(job_id)
        return self.exported


def _text(value: str) -> list[dict[str, Any]]:
    return [
        {
            "plain_text": value,
            "annotations": {
                "bold": False,
                "italic": False,
                "strikethrough": False,
                "underline": False,
                "code": False,
                "color": "default",
            },
        }
    ]


def test_snapshot_normalizes_unicode_blocks_and_media_stably() -> None:
    exported = {
        "mode": "snapshot",
        "sync_id": "sync-1",
        "question_id": "question-1",
        "pages": [
            {
                "page_id": "page-1",
                "title": "Q1 模型",
                "url": "https://www.notion.so/page-1",
                "depth": 0,
                "blocks": [
                    {
                        "id": "heading-1",
                        "type": "heading_1",
                        "heading_1": {"rich_text": _text("假设")},
                    },
                    {
                        "id": "paragraph-1",
                        "type": "paragraph",
                        "paragraph": {
                            "rich_text": [
                                *_text("人口随时间增长："),
                                {
                                    "type": "equation",
                                    "plain_text": "P(t)",
                                    "equation": {"expression": "P(t)"},
                                    "annotations": {},
                                },
                            ]
                        },
                    },
                    {
                        "id": "equation-1",
                        "type": "equation",
                        "equation": {"expression": "P(t)=P_0e^{rt}"},
                    },
                    {
                        "id": "table-1",
                        "type": "table",
                        "table": {},
                        "children": [
                            {
                                "id": "row-1",
                                "type": "table_row",
                                "table_row": {
                                    "cells": [
                                        _text("参数"),
                                        [
                                            {
                                                "type": "equation",
                                                "plain_text": "",
                                                "equation": {"expression": "s_t"},
                                                "annotations": {},
                                            }
                                        ],
                                    ]
                                },
                            }
                        ],
                    },
                    {
                        "id": "bookmark-1",
                        "type": "bookmark",
                        "bookmark": {
                            "url": "https://github.com/imouup/mmdash-fork/tree/main",
                            "caption": _text("mmdash fork"),
                        },
                    },
                    {
                        "id": "image-1",
                        "type": "image",
                        "last_edited_time": "2026-08-09T00:00:00.000Z",
                        "image": {
                            "type": "file",
                            "file": {"url": "https://files.example/model.png?expires=1"},
                            "caption": _text("结果图"),
                        },
                    },
                ],
            }
        ],
    }
    client = ExportClient(exported)
    handler = ModelNotionHandler(client)

    result = asyncio.run(handler(HandlerContext(job_id="job-1", worker_id="worker-1"), {}))

    assert client.job_ids == ["job-1"]
    assert result["title"] == "Q1 模型"
    assert (
        result["content_text"]
        == "假设\n人口随时间增长：P(t)\nP(t)=P_0e^{rt}\n参数\ts_t\nmmdash fork"
    )
    assert result["blocks"][1]["rich_text"][1] == {
        "text": "P(t)",
        "expression": "P(t)",
    }
    assert result["outline"] == [{"block_id": "heading-1", "title": "假设", "level": 1}]
    assert result["media"][0]["source_block_id"] == "image-1"
    assert result["blocks"][3]["children"][0]["rows"] == [["参数", "s_t"]]
    assert result["blocks"][3]["children"][0]["cells"][1] == [{"text": "", "expression": "s_t"}]
    assert result["blocks"][4]["url"] == "https://github.com/imouup/mmdash-fork/tree/main"
    assert "| 参数 | s_t |" in result["content_markdown"]
    assert len(result["content_hash"]) == 64

    changed_url = dict(exported)
    changed_url["pages"] = [dict(exported["pages"][0])]
    changed_url["pages"][0]["blocks"] = [dict(value) for value in exported["pages"][0]["blocks"]]
    changed_url["pages"][0]["blocks"][5] = dict(changed_url["pages"][0]["blocks"][5])
    changed_url["pages"][0]["blocks"][5]["image"] = dict(
        changed_url["pages"][0]["blocks"][5]["image"]
    )
    changed_url["pages"][0]["blocks"][5]["image"]["file"] = {
        "url": "https://files.example/model.png?expires=2"
    }
    repeated = asyncio.run(
        ModelNotionHandler(ExportClient(changed_url))(
            HandlerContext(job_id="job-2", worker_id="worker-1"), {}
        )
    )
    assert repeated["content_hash"] == result["content_hash"]

    replaced_file = dict(exported)
    replaced_file["pages"] = [dict(exported["pages"][0])]
    replaced_file["pages"][0]["blocks"] = [dict(value) for value in exported["pages"][0]["blocks"]]
    replaced_file["pages"][0]["blocks"][5]["last_edited_time"] = "2026-08-09T00:01:00.000Z"
    replaced_result = asyncio.run(
        ModelNotionHandler(ExportClient(replaced_file))(
            HandlerContext(job_id="job-replaced", worker_id="worker-1"), {}
        )
    )
    assert replaced_result["content_hash"] != result["content_hash"]

    renamed = dict(exported)
    renamed["pages"] = [dict(exported["pages"][0])]
    renamed["pages"][0]["title"] = "Q1 模型（修订）"
    renamed_result = asyncio.run(
        ModelNotionHandler(ExportClient(renamed))(
            HandlerContext(job_id="job-3", worker_id="worker-1"), {}
        )
    )
    assert renamed_result["content_hash"] != result["content_hash"]


def test_discovery_preserves_parent_relationships() -> None:
    client = ExportClient(
        {
            "mode": "discover",
            "sync_id": "sync-1",
            "root_title": "模型根页面",
            "pages": [
                {
                    "page_id": "child-1",
                    "parent_page_id": "root-1",
                    "title": "Q1",
                    "url": "https://www.notion.so/child-1",
                    "depth": 1,
                    "blocks": [],
                }
            ],
        }
    )
    result = asyncio.run(
        ModelNotionHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
    )
    assert result == {
        "mode": "discover",
        "sync_id": "sync-1",
        "root_title": "模型根页面",
        "pages": [
            {
                "page_id": "child-1",
                "parent_page_id": "root-1",
                "title": "Q1",
                "url": "https://www.notion.so/child-1",
                "depth": 1,
                "has_children": False,
            }
        ],
    }
