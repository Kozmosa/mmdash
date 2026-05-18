"""Comprehensive tests for Notion diff/apply using MockNotionAPI.

Tests cover real Notion API behavior observed from actual API probing.
"""

import copy
import httpx
import pytest
from unittest.mock import AsyncMock, patch


@pytest.fixture(autouse=True)
def _zero_rate_limit_delay(monkeypatch):
    """Disable the rate-limit sleep during tests."""
    monkeypatch.setattr("app.services.notion._RATE_LIMIT_DELAY", 0.0)


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


# ─── Block parsing edge cases ────────────────────────────────────────────────

class TestMarkdownToNotionBlocksEdgeCases:
    def test_inline_equation(self):
        from app.services.notion import markdown_to_notion_blocks
        blocks = markdown_to_notion_blocks("$$E=mc^2$$")
        assert len(blocks) == 1
        assert blocks[0]["type"] == "equation"
        assert "mc^2" in blocks[0]["equation"]["expression"]

    def test_multi_line_equation(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "$$\nE = mc^2\nF = ma\n$$"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 1
        assert blocks[0]["type"] == "equation"
        expr = blocks[0]["equation"]["expression"]
        assert "mc^2" in expr
        assert "ma" in expr

    def test_multi_line_equation_no_closing(self):
        """Equation block without closing $$ — should still work (parsed to EOF)."""
        from app.services.notion import markdown_to_notion_blocks
        md = "$$\nE = mc^2"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 1
        assert blocks[0]["type"] == "equation"

    def test_table_detection(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "| a | b |\n|---|----|\n| 1 | 2 |"
        blocks = markdown_to_notion_blocks(md)
        # Tables are stored as code blocks with language=markdown
        assert len(blocks) == 1
        assert blocks[0]["type"] == "code"
        assert blocks[0]["code"]["language"] == "markdown"

    def test_single_pipe_line_not_table(self):
        """A single | line without a separator row is NOT a table."""
        from app.services.notion import markdown_to_notion_blocks
        md = "| just a pipe line"
        blocks = markdown_to_notion_blocks(md)
        # Should be a paragraph, not a code/table block
        assert len(blocks) == 1
        assert blocks[0]["type"] in ("paragraph",)

    def test_paragraph_merging(self):
        """Consecutive non-special lines should merge into one paragraph."""
        from app.services.notion import markdown_to_notion_blocks
        md = "Line 1\nLine 2\nLine 3"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 1
        assert blocks[0]["type"] == "paragraph"
        rt = blocks[0]["paragraph"]["rich_text"]
        text = "".join(t["text"]["content"] for t in rt)
        assert "Line 1" in text
        assert "Line 2" in text

    def test_blockquote_continuation(self):
        from app.services.notion import markdown_to_notion_blocks
        md = "> Line 1\n> Line 2\n> Line 3"
        blocks = markdown_to_notion_blocks(md)
        assert len(blocks) == 1
        assert blocks[0]["type"] == "quote"

    def test_is_special_line_heading(self):
        from app.services.notion import _is_special_line
        assert _is_special_line("# Heading") is True
        assert _is_special_line("## Heading") is True
        assert _is_special_line("### Heading") is True

    def test_is_special_line_code_divider_quote(self):
        from app.services.notion import _is_special_line
        assert _is_special_line("```python") is True
        assert _is_special_line("> quote") is True
        assert _is_special_line("- bullet") is True
        assert _is_special_line("1. numbered") is True
        assert _is_special_line("---") is True
        assert _is_special_line("") is True  # empty line is special

    def test_is_special_line_plain_text(self):
        from app.services.notion import _is_special_line
        assert _is_special_line("Just a paragraph") is False
        assert _is_special_line("   indented text") is False

    def test_empty_paragraph_produces_rich_text(self):
        """Empty input should produce no blocks."""
        from app.services.notion import markdown_to_notion_blocks
        blocks = markdown_to_notion_blocks("")
        assert blocks == []


# ─── Same-type update path ───────────────────────────────────────────────────

class TestSameTypeUpdate:
    @pytest.mark.asyncio
    async def test_update_paragraph_same_type(self, mock_notion):
        """Same type + different content → should call update_block."""
        block = {
            "id": "b1", "object": "block", "type": "paragraph",
            "paragraph": {
                "rich_text": [{"type": "text", "text": {"content": "Old"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}],
                "color": "default",
            },
        }
        mock_notion.add_page("page-1", [block])

        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Updated"}}
            ]}},
        ]

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
                if "/children" not in url:
                    update_calls.append(("update_block", url))
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

        assert len(update_calls) == 1, f"Expected 1 update, got {len(update_calls)}"
        assert mock_notion._blocks["page-1"][0]["paragraph"]["rich_text"][0]["text"]["content"] == "Updated"

    @pytest.mark.asyncio
    async def test_no_update_when_content_unchanged(self, mock_notion):
        """Same type + same content → should NOT call update_block."""
        block = {
            "id": "b1", "object": "block", "type": "paragraph",
            "paragraph": {
                "rich_text": [{"type": "text", "text": {"content": "Same"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}],
                "color": "default",
            },
        }
        mock_notion.add_page("page-1", [block])

        desired = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "Same"}}
            ]}},
        ]

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
                if "/children" not in url:
                    update_calls.append(url)
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

        assert len(update_calls) == 0, f"Expected 0 updates, got {update_calls}"

    @pytest.mark.asyncio
    async def test_update_code_same_language(self, mock_notion):
        """Same code block language + different content → update."""
        block = {
            "id": "b1", "object": "block", "type": "code",
            "code": {
                "rich_text": [{"type": "text", "text": {"content": "x=1"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}],
                "language": "python",
            },
        }
        mock_notion.add_page("page-1", [block])

        desired = [
            {"object": "block", "type": "code", "code": {
                "rich_text": [{"type": "text", "text": {"content": "x=2"}}],
                "language": "python",
            }},
        ]

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
                if "/children" not in url:
                    update_calls.append(url)
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

        assert len(update_calls) == 1
        assert mock_notion._blocks["page-1"][0]["code"]["rich_text"][0]["text"]["content"] == "x=2"


# ─── Pagination ──────────────────────────────────────────────────────────────

class TestPagination:
    @pytest.mark.asyncio
    async def test_get_all_children_pagination(self, mock_notion):
        """When a page has >100 blocks, pagination kicks in."""
        # Create 250 blocks — more than the default page_size
        blocks = []
        for i in range(250):
            blocks.append({
                "id": f"b{i}", "object": "block", "type": "paragraph",
                "paragraph": {
                    "rich_text": [{"type": "text", "text": {"content": f"Block {i}"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}],
                    "color": "default",
                },
            })
        mock_notion.add_page("page-1", blocks)

        desired = [
            {"object": "block", "type": "heading_1", "heading_1": {"rich_text": [
                {"type": "text", "text": {"content": "New Title"}}
            ]}},
        ]

        # This should fetch all 250 blocks via pagination, then diff
        await _run_diff(mock_notion, "page-1", desired)

        # Heading replaced the first paragraph
        result = mock_notion._blocks["page-1"]
        assert result[0]["type"] == "heading_1"

    @pytest.mark.asyncio
    async def test_append_after_chunking(self, mock_notion):
        """Appending >100 blocks at once triggers chunked appends."""
        mock_notion.add_page("page-1", [])

        # Create 250 desired blocks — more than CHUNK_SIZE
        desired = []
        for i in range(250):
            desired.append(
                {"object": "block", "type": "bulleted_list_item", "bulleted_list_item": {"rich_text": [
                    {"type": "text", "text": {"content": f"Item {i}"}}
                ]}}
            )

        await _run_diff(mock_notion, "page-1", desired)

        result = mock_notion._blocks["page-1"]
        assert len(result) == 250
        assert result[0]["type"] == "bulleted_list_item"
        assert result[249]["type"] == "bulleted_list_item"


# ─── create_page ─────────────────────────────────────────────────────────────

class TestCreatePage:
    @pytest.mark.asyncio
    async def test_create_page_returns_id(self, mock_notion):
        """create_page should return the new page ID from Notion API."""
        # Add a parent page first
        mock_notion.add_page("parent-1", [])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, headers=None, json=None):
                # Simulate Notion create page response
                from tests.mock_notion_api import MockNotionResponse
                page_id = "new-page-123"
                mock_notion.add_page(page_id, [])
                return MockNotionResponse(200, {
                    "object": "page",
                    "id": page_id,
                    "properties": {
                        "title": {"title": [{"text": {"content": json.get("properties", {}).get("title", {}).get("title", [{}])[0].get("text", {}).get("content", "")}}]}
                    },
                })

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import create_page
            page_id = await create_page("parent-1", "Test Page", "ntn_mock")

        assert page_id == "new-page-123"
        assert page_id in mock_notion._pages

    @pytest.mark.asyncio
    async def test_create_page_response_shape_matches_real_api(self, mock_notion):
        """The mock should return the same shape as real Notion API."""
        call_args = []

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, headers=None, json=None):
                call_args.append({"url": url, "json": json})
                from tests.mock_notion_api import MockNotionResponse
                return MockNotionResponse(200, {
                    "object": "page",
                    "id": "created-page-id",
                })

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import create_page
            await create_page("parent-1", "My Page", "ntn_mock")

        # Verify the request shape
        assert len(call_args) == 1
        req = call_args[0]
        assert "pages" in req["url"]
        assert req["json"]["parent"]["page_id"] == "parent-1"
        title_blocks = req["json"]["properties"]["title"]["title"]
        assert title_blocks[0]["text"]["content"] == "My Page"


class TestDeleteBlockErrorHandling:
    """delete_block should silently skip 4xx errors but propagate 5xx and network errors."""

    @pytest.mark.asyncio
    async def test_delete_block_swallows_400(self):
        """HTTP 400 (block cannot be deleted) → should not raise."""
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def delete(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                resp = MockNotionResponse(400, {"message": "Block cannot be deleted"})
                resp.raise_for_status()

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import delete_block
            # Should not raise
            await delete_block("some-block-id", "ntn_test")

    @pytest.mark.asyncio
    async def test_delete_block_propagates_500(self):
        """HTTP 500 (server error) → should propagate."""
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def delete(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                resp = MockNotionResponse(500, {"message": "Internal error"})
                resp.raise_for_status()

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import delete_block
            with pytest.raises(httpx.HTTPStatusError):
                await delete_block("some-block-id", "ntn_test")

    @pytest.mark.asyncio
    async def test_delete_block_propagates_404(self):
        """HTTP 404 (already deleted) → should swallow (4xx)."""
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def delete(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                resp = MockNotionResponse(404, {"message": "Block not found"})
                resp.raise_for_status()

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            from app.services.notion import delete_block
            # Should not raise
            await delete_block("missing-block-id", "ntn_test")
