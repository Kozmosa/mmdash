"""Mock Notion API that accurately emulates real Notion API behavior.

Based on observations from real Notion API probing:
- Blocks have auto-filled decoration fields (color, icon, is_toggleable, caption)
- Block type cannot be changed (validation_error)
- rich_text annotations always have all fields present
- divider blocks have empty dict content {}

Usage:
    mock = MockNotionAPI()
    mock.add_page("page-1", [existing_block, ...])
    # Then patch httpx.AsyncClient to use mock's handler
"""

import copy
import json
import uuid
from typing import Any


class MockNotionResponse:
    """Mimics httpx.Response for Notion API calls."""
    def __init__(self, status_code: int, json_data: Any):
        self.status_code = status_code
        self._json = json_data

    def json(self) -> Any:
        return self._json

    def raise_for_status(self):
        if self.status_code >= 400:
            import httpx
            raise httpx.HTTPStatusError(
                f"Mock error {self.status_code}",
                request=object(),
                response=self,
            )


class MockNotionAPI:
    """In-memory Notion API simulator with real-world response shapes."""

    def __init__(self):
        self._pages: dict[str, dict] = {}  # page_id → page metadata
        self._blocks: dict[str, list[dict]] = {}  # page_id → list of child blocks
        self._deleted_blocks: set[str] = set()

    def add_page(self, page_id: str, blocks: list[dict] | None = None):
        """Register a page with optional existing blocks."""
        self._pages[page_id] = {"id": page_id}
        self._blocks[page_id] = []
        if blocks:
            for b in blocks:
                self._blocks[page_id].append(self._normalize_block(b))

    def _normalize_block(self, block: dict) -> dict:
        """Simulate what Notion API does: auto-fill decoration fields on creation."""
        b = copy.deepcopy(block)
        block_type = b.get("type", "paragraph")
        content = b.get(block_type, {})

        if block_type in ("paragraph", "heading_1", "heading_2", "heading_3",
                          "bulleted_list_item", "numbered_list_item", "quote"):
            if "rich_text" in content:
                content["rich_text"] = [
                    self._normalize_rich_text(rt) for rt in content["rich_text"]
                ]
            content.setdefault("color", "default")
            if block_type in ("heading_1", "heading_2", "heading_3"):
                content.setdefault("is_toggleable", False)
            if block_type == "paragraph":
                content.setdefault("icon", None)

        elif block_type == "code":
            content.setdefault("language", "plain text")
            content.setdefault("caption", [])
            if "rich_text" in content:
                content["rich_text"] = [
                    self._normalize_rich_text(rt) for rt in content["rich_text"]
                ]

        elif block_type == "divider":
            b[block_type] = {}

        b.setdefault("id", str(uuid.uuid4()))
        b.setdefault("object", "block")
        return b

    def _normalize_rich_text(self, rt: dict) -> dict:
        """Ensure rich_text has all annotation fields (like real Notion)."""
        rt = copy.deepcopy(rt)
        if rt.get("type") == "equation":
            return rt
        ann = rt.setdefault("annotations", {})
        for key in ("bold", "italic", "strikethrough", "underline", "code"):
            ann.setdefault(key, False)
        ann.setdefault("color", "default")
        rt.setdefault("plain_text", rt.get("text", {}).get("content", ""))
        return rt

    # ─── API handlers (pass these to httpx mock) ──────────────────────────

    def handle(self, method: str, url: str, **kwargs) -> MockNotionResponse:
        """Route API calls to the right handler."""
        if method == "GET" and "/blocks/" in url and "/children" in url:
            page_id = url.split("/blocks/")[1].split("/children")[0]
            return self.get_block_children(page_id, kwargs.get("params", {}))
        if method == "PATCH" and "/blocks/" in url and "/children" in url:
            page_id = url.split("/blocks/")[1].split("/children")[0]
            return self.append_block_children(page_id, kwargs.get("json", {}))
        if method == "PATCH" and "/pages/" in url:
            page_id = url.split("/pages/")[1]
            return self.update_page(page_id, kwargs.get("json", {}))

        if method == "PATCH" and "/blocks/" in url:
            block_id = url.split("/blocks/")[1]
            return self.update_block(block_id, kwargs.get("json", {}))
        if method == "DELETE" and "/blocks/" in url:
            block_id = url.split("/blocks/")[1]
            return self.delete_block(block_id)
        if method == "GET" and "/pages/" in url:
            page_id = url.split("/pages/")[1]
            return self.get_page(page_id)
        return MockNotionResponse(404, {"object": "error", "message": "Not found", "status": 404})

    def get_block_children(self, page_id: str, params: dict) -> MockNotionResponse:
        blocks = self._blocks.get(page_id, [])
        return MockNotionResponse(200, {
            "object": "list",
            "results": list(blocks),
            "has_more": False,
            "next_cursor": None,
        })

    def append_block_children(self, page_id: str, body: dict) -> MockNotionResponse:
        children = body.get("children", [])
        after_id = body.get("after")
        blocks = self._blocks.setdefault(page_id, [])

        # Find insertion position (after the specified block, or at beginning)
        insert_at = 0
        if after_id:
            for idx, b in enumerate(blocks):
                if b["id"] == after_id:
                    insert_at = idx + 1
                    break

        results = []
        for child in children:
            normalized = self._normalize_block(child)
            blocks.insert(insert_at, normalized)
            insert_at += 1
            results.append(normalized)
        return MockNotionResponse(200, {
            "object": "list",
            "results": results,
            "has_more": False,
        })

    def update_page(self, page_id: str, body: dict) -> MockNotionResponse:
        """Update page properties (title etc)."""
        if page_id not in self._pages:
            return MockNotionResponse(404, {"object": "error", "message": "Page not found", "status": 404})
        # In a real mock we'd update stored properties, but for tests just return success
        return MockNotionResponse(200, {"object": "page", "id": page_id})

    def update_block(self, block_id: str, body: dict) -> MockNotionResponse:
        for page_id, blocks in self._blocks.items():
            for i, b in enumerate(blocks):
                if b["id"] == block_id:
                    new_type = next(iter(body.keys()))
                    if new_type != b["type"]:
                        return MockNotionResponse(400, {
                            "object": "error",
                            "status": 400,
                            "code": "validation_error",
                            "message": (
                                f"Block type mismatch: this block is a "
                                f"`{b['type']}`, but your request includes "
                                f"fields for a `{new_type}` block. Changing "
                                f"a block's type is not supported."
                            ),
                        })
                    updated = self._normalize_block({"id": block_id, **body})
                    blocks[i] = updated
                    return MockNotionResponse(200, updated)
        return MockNotionResponse(404, {"object": "error", "message": "Block not found", "status": 404})

    def delete_block(self, block_id: str) -> MockNotionResponse:
        for page_id, blocks in self._blocks.items():
            for i, b in enumerate(blocks):
                if b["id"] == block_id:
                    del blocks[i]
                    return MockNotionResponse(200, {
                        "object": "block",
                        "id": block_id,
                        "archived": True,
                    })
        return MockNotionResponse(200, {
            "object": "block",
            "id": block_id,
            "archived": True,
        })

    def get_page(self, page_id: str) -> MockNotionResponse:
        if page_id in self._pages:
            return MockNotionResponse(200, {
                "object": "page",
                "id": page_id,
                "properties": {
                    "title": {
                        "type": "title",
                        "title": [{"type": "text", "text": {"content": "Test Page"}, "plain_text": "Test Page"}],
                    }
                },
            })
        return MockNotionResponse(404, {"object": "error", "message": "Page not found", "status": 404})
