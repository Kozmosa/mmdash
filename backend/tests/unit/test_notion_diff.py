"""Comprehensive tests for Notion diff/apply using MockNotionAPI.

Tests cover real Notion API behavior observed from actual API probing.
"""

import copy
import pytest
from unittest.mock import AsyncMock, patch


@pytest.fixture
def mock_notion():
    """Create a fresh MockNotionAPI for each test."""
    from tests.mock_notion_api import MockNotionAPI
    return MockNotionAPI()


@pytest.fixture
def mock_transport(mock_notion):
    """Create an httpx mock that routes to MockNotionAPI."""

    class MockTransport:
        def __init__(self, api):
            self.api = api

        async def handle_async_request(self, request):
            method = request.method
            url = str(request.url)
            json_body = None
            if request.content:
                import json
                json_body = json.loads(request.content)
            return self.api.handle(method, url, json=json_body, params=dict(request.url.params))

    return MockTransport(mock_notion)


# ─── A helper that patches httpx.AsyncClient to use mock ────────────────────

async def _run_diff(api, page_id, desired_blocks):
    """Run diff_and_apply_blocks with the mock API patched in."""
    from app.services.notion import diff_and_apply_blocks
    from tests.mock_notion_api import MockNotionResponse

    fake_token = "ntn_mock"

    # We need to patch every httpx.AsyncClient usage inside the notion module
    # The cleanest way: patch httpx.AsyncClient to return our mock client
    class MockClient:
        def __init__(self, *a, **kw):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *a):
            pass

        async def get(self, url, headers=None, params=None):
            resp = api.handle("GET", url, params=params or {})
            resp.raise_for_status()
            return resp

        async def patch(self, url, headers=None, json=None):
            resp = api.handle("PATCH", url, json=json or {})
            resp.raise_for_status()
            return resp

        async def delete(self, url, headers=None):
            resp = api.handle("DELETE", url)
            resp.raise_for_status()
            return resp

    with patch("app.services.notion.httpx.AsyncClient", MockClient):
        await diff_and_apply_blocks(page_id, desired_blocks, fake_token)


# ─── Tests ──────────────────────────────────────────────────────────────────

class TestDiffAndApplyBlocks:
    @pytest.mark.asyncio
    async def test_first_time_creation_empty_page(self, mock_notion):
        """Creating content on a page with no existing blocks."""
        mock_notion.add_page("page-1", [])
        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Hello"}}
            ]}},
        ]
        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 1
        assert blocks[0]["type"] == "paragraph"
        rt = blocks[0]["paragraph"]["rich_text"]
        assert rt[0]["text"]["content"] == "Hello"

    @pytest.mark.asyncio
    async def test_incremental_update_same_type(self, mock_notion):
        """Update a paragraph's text (same type, different content)."""
        block = {"id": "b1", "object": "block", "type": "paragraph",
                 "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Old"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default"}}
        mock_notion.add_page("page-1", [block])
        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "New"}}
            ]}},
        ]
        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 1
        assert blocks[0]["paragraph"]["rich_text"][0]["text"]["content"] == "New"

    @pytest.mark.asyncio
    async def test_false_positive_avoided(self, mock_notion):
        """Same content should NOT trigger an update (prev was always false)."""
        block = {"id": "b1", "object": "block", "type": "paragraph",
                 "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Same"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default", "icon": None}}
        mock_notion.add_page("page-1", [block])
        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Same"}}
            ]}},
        ]
        # Should NOT try to update (content is same, decorations ignored)
        update_calls = []

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass

            async def get(self, url, headers=None, params=None):
                resp = mock_notion.handle("GET", url, params=params or {})
                resp.raise_for_status()
                return resp

            async def patch(self, url, headers=None, json=None):
                update_calls.append(("patch", url))
                resp = mock_notion.handle("PATCH", url, json=json or {})
                resp.raise_for_status()
                return resp

            async def delete(self, url, headers=None):
                resp = mock_notion.handle("DELETE", url)
                resp.raise_for_status()
                return resp

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import diff_and_apply_blocks
            await diff_and_apply_blocks("page-1", desired, "ntn_mock")

        # Should have 0 update calls (equivalent content)
        assert len(update_calls) == 0, f"Expected 0 updates, got {update_calls}"

    @pytest.mark.asyncio
    async def test_consecutive_type_changes(self, mock_notion):
        """Two consecutive type changes — the bug we fixed (KeyError: 'id')."""
        block_a = {"id": "b1", "object": "block", "type": "paragraph",
                   "paragraph": {"rich_text": [{"type": "text", "text": {"content": "A"}, "annotations": {}}], "color": "default"}}
        block_b = {"id": "b2", "object": "block", "type": "paragraph",
                   "paragraph": {"rich_text": [{"type": "text", "text": {"content": "B"}, "annotations": {}}], "color": "default"}}
        mock_notion.add_page("page-1", [block_a, block_b])

        desired = [
            {"object": "block", "type": "heading_1", "heading_1": {"rich_text": [
                {"type": "text", "text": {"content": "New A"}}
            ]}},
            {"object": "block", "type": "heading_2", "heading_2": {"rich_text": [
                {"type": "text", "text": {"content": "New B"}}
            ]}},
        ]

        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 2
        assert blocks[0]["type"] == "heading_1"
        assert blocks[1]["type"] == "heading_2"
        # Both should have real "id" fields (from Notion response, not id-less)
        assert "id" in blocks[0]
        assert "id" in blocks[1]

    @pytest.mark.asyncio
    async def test_full_replacement_different_types(self, mock_notion):
        """Replace all blocks with entirely different types."""
        mock_notion.add_page("page-1", [
            {"id": "b1", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Old"}}], "color": "default"}},
        ])

        desired = [
            {"object": "block", "type": "bulleted_list_item", "bulleted_list_item": {"rich_text": [
                {"type": "text", "text": {"content": "Item 1"}}
            ]}},
            {"object": "block", "type": "numbered_list_item", "numbered_list_item": {"rich_text": [
                {"type": "text", "text": {"content": "Item 2"}}
            ]}},
        ]

        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 2
        assert blocks[0]["type"] == "bulleted_list_item"
        assert blocks[1]["type"] == "numbered_list_item"

    @pytest.mark.asyncio
    async def test_clear_all_blocks(self, mock_notion):
        """Empty desired blocks should delete all existing blocks."""
        mock_notion.add_page("page-1", [
            {"id": "b1", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "X"}}], "color": "default"}},
            {"id": "b2", "object": "block", "type": "bulleted_list_item",
             "bulleted_list_item": {"rich_text": [{"type": "text", "text": {"content": "Y"}}], "color": "default"}},
        ])

        await _run_diff(mock_notion, "page-1", [])

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 0  # all deleted

    @pytest.mark.asyncio
    async def test_append_beyond_current_length(self, mock_notion):
        """More desired blocks than current — append extras."""
        mock_notion.add_page("page-1", [
            {"id": "b1", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "First"}}], "color": "default"}},
        ])

        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "First v2"}}
            ]}},
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Second"}}
            ]}},
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Third"}}
            ]}},
        ]

        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 3
        assert blocks[0]["paragraph"]["rich_text"][0]["text"]["content"] == "First v2"
        assert blocks[1]["paragraph"]["rich_text"][0]["text"]["content"] == "Second"
        assert blocks[2]["paragraph"]["rich_text"][0]["text"]["content"] == "Third"

    @pytest.mark.asyncio
    async def test_mixed_block_types(self, mock_notion):
        """Full spectrum of block types in one diff."""
        mock_notion.add_page("page-1", [])

        desired = [
            {"object": "block", "type": "heading_1", "heading_1": {"rich_text": [{"type": "text", "text": {"content": "Title"}}]}},
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Body text"}}]}},
            {"object": "block", "type": "bulleted_list_item", "bulleted_list_item": {"rich_text": [{"type": "text", "text": {"content": "Bullet"}}]}},
            {"object": "block", "type": "code", "code": {"rich_text": [{"type": "text", "text": {"content": "x=1"}}], "language": "python"}},
            {"object": "block", "type": "equation", "equation": {"expression": "E=mc^2"}},
            {"object": "block", "type": "quote", "quote": {"rich_text": [{"type": "text", "text": {"content": "Quote text"}}]}},
            {"object": "block", "type": "divider", "divider": {}},
        ]

        await _run_diff(mock_notion, "page-1", desired)

        blocks = mock_notion._blocks["page-1"]
        assert len(blocks) == 7
        types = [b["type"] for b in blocks]
        assert types == ["heading_1", "paragraph", "bulleted_list_item", "code", "equation", "quote", "divider"]


class TestBlocksEquivalent:
    def test_same_content_equivalent(self):
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Hello"}}]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Hello"}}]}}
        assert _blocks_equivalent(a, b) is True

    def test_different_content_not_equivalent(self):
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Hello"}}]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "World"}}]}}
        assert _blocks_equivalent(a, b) is False

    def test_different_type_not_equivalent(self):
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Hello"}}]}}
        b = {"type": "heading_1", "heading_1": {"rich_text": [{"type": "text", "text": {"content": "Hello"}}]}}
        assert _blocks_equivalent(a, b) is False

    def test_ignores_color_field(self):
        """API-auto-filled 'color' field should be ignored."""
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "X"}}]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "X"}}], "color": "default"}}
        assert _blocks_equivalent(a, b) is True

    def test_ignores_icon_field(self):
        """Paragraph icon=None should not affect equivalence."""
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "X"}}]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [{"type": "text", "text": {"content": "X"}}], "icon": None, "color": "default"}}
        assert _blocks_equivalent(a, b) is True

    def test_respects_annotations(self):
        """Bold/italic annotations DO matter."""
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [
            {"type": "text", "text": {"content": "X"}, "annotations": {"bold": True}}
        ]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [
            {"type": "text", "text": {"content": "X"}, "annotations": {"bold": False}}
        ]}}
        assert _blocks_equivalent(a, b) is False

    def test_divider_always_equivalent(self):
        from app.services.notion import _blocks_equivalent
        assert _blocks_equivalent(
            {"type": "divider", "divider": {}},
            {"type": "divider", "divider": {}},
        ) is True

    def test_equation_compares_expression(self):
        from app.services.notion import _blocks_equivalent
        assert _blocks_equivalent(
            {"type": "equation", "equation": {"expression": "E=mc^2"}},
            {"type": "equation", "equation": {"expression": "E=mc^2"}},
        ) is True
        assert _blocks_equivalent(
            {"type": "equation", "equation": {"expression": "E=mc^2"}},
            {"type": "equation", "equation": {"expression": "F=ma"}},
        ) is False

    def test_code_compares_language(self):
        from app.services.notion import _blocks_equivalent
        a = {"type": "code", "code": {"rich_text": [{"type": "text", "text": {"content": "x=1"}}], "language": "python"}}
        b = {"type": "code", "code": {"rich_text": [{"type": "text", "text": {"content": "x=1"}}], "language": "javascript"}}
        assert _blocks_equivalent(a, b) is False

    def test_rich_text_link_difference(self):
        from app.services.notion import _blocks_equivalent
        a = {"type": "paragraph", "paragraph": {"rich_text": [
            {"type": "text", "text": {"content": "link", "link": {"url": "https://a.com"}}}
        ]}}
        b = {"type": "paragraph", "paragraph": {"rich_text": [
            {"type": "text", "text": {"content": "link", "link": {"url": "https://b.com"}}}
        ]}}
        assert _blocks_equivalent(a, b) is False


class TestNotionProviderRoundTrip:
    @pytest.mark.asyncio
    async def test_markdown_to_notion_blocks_empty(self):
        from app.services.notion import markdown_to_notion_blocks
        blocks = markdown_to_notion_blocks("")
        assert blocks == []

    @pytest.mark.asyncio
    async def test_markdown_to_notion_blocks_paragraphs(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "Hello\n\nWorld"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 2
        assert blocks[0]["type"] == "paragraph"
        assert blocks[1]["type"] == "paragraph"

    @pytest.mark.asyncio
    async def test_markdown_to_notion_blocks_headings(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "# H1\n## H2\n### H3"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 3
        assert blocks[0]["type"] == "heading_1"
        assert blocks[1]["type"] == "heading_2"
        assert blocks[2]["type"] == "heading_3"

    @pytest.mark.asyncio
    async def test_markdown_to_notion_blocks_bold_italic(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "**bold** *italic* ***both*** `code`"
        blocks = markdown_to_notion_blocks(md)
        rt = blocks[0]["paragraph"]["rich_text"]
        # Should have multiple rich_text segments
        assert len(rt) >= 5

    @pytest.mark.asyncio
    async def test_notion_blocks_to_markdown_roundtrip(self):
        """Create blocks → convert to markdown → check fidelity."""
        from app.services.notion import markdown_to_notion_blocks
        from app.services.notion_fetch import notion_blocks_to_markdown

        md_in = "# Title\n\nBody text\n\n- Bullet\n\n1. Numbered\n\n```python\nprint(1)\n```\n\n> Quote\n\n---"
        blocks = markdown_to_notion_blocks(md_in)
        md_out = notion_blocks_to_markdown(blocks)

        assert "# Title" in md_out
        assert "Body text" in md_out
        assert "- Bullet" in md_out
        assert "```python" in md_out
        assert "> Quote" in md_out

    @pytest.mark.asyncio
    async def test_notion_blocks_to_markdown_preserves_bold(self):
        """Bold formatting should survive round-trip."""
        from app.services.notion import markdown_to_notion_blocks
        from app.services.notion_fetch import notion_blocks_to_markdown

        md_in = "This is **bold** text"
        blocks = markdown_to_notion_blocks(md_in)
        md_out = notion_blocks_to_markdown(blocks)

        assert "**bold**" in md_out
