"""Integration tests for NotionProvider using MockNotionAPI.

Covers the full provider flow: OAuth exchange, page creation, content
fetch with markdown conversion, metadata fetch, and content update.
"""

import pytest
from unittest.mock import patch, AsyncMock


@pytest.fixture
def mock_notion():
    from tests.mock_notion_api import MockNotionAPI
    return MockNotionAPI()


@pytest.fixture
def provider():
    from app.services.notion_provider import NotionProvider
    return NotionProvider()


@pytest.fixture
def credentials():
    return {"access_token": "ntn_test123"}


# ─── OAuth ──────────────────────────────────────────────────────────────────

class TestOAuth:
    def test_get_auth_url_contains_client_id(self, provider, monkeypatch):
        monkeypatch.setattr("app.services.notion_provider.settings.NOTION_CLIENT_ID", "test-client-id")
        monkeypatch.setattr("app.services.notion_provider.settings.NOTION_REDIRECT_URI", "http://localhost:3000/callback")
        url = provider.get_auth_url()
        assert url is not None
        assert "test-client-id" in url
        assert "redirect_uri=http://localhost:3000/callback" in url
        assert "response_type=code" in url
        assert "state=" in url

    @pytest.mark.asyncio
    async def test_exchange_auth_code(self, provider):
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, auth=None, json=None, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                return MockNotionResponse(200, {
                    "access_token": "ntn_exchanged",
                    "workspace_id": "ws_123",
                    "workspace_name": "Test Workspace",
                })

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.exchange_auth_code("test-code")

        assert result["access_token"] == "ntn_exchanged"
        assert result["workspace_id"] == "ws_123"
        assert result["workspace_name"] == "Test Workspace"

    @pytest.mark.asyncio
    async def test_exchange_auth_code_failure(self, provider):
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, auth=None, json=None, headers=None):
                import httpx
                from tests.mock_notion_api import MockNotionResponse
                resp = MockNotionResponse(400, {"error": "invalid_grant"})
                raise httpx.HTTPStatusError("bad request", request=object(), response=resp)

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            with pytest.raises(Exception):
                await provider.exchange_auth_code("bad-code")


# ─── Fetch Content ──────────────────────────────────────────────────────────

class TestFetchContent:
    @pytest.mark.asyncio
    async def test_fetch_page_content_with_blocks(self, provider, mock_notion, credentials, monkeypatch):
        """Full fetch: blocks → markdown conversion + metadata title."""
        # Setup mock blocks in the "Notion page"
        mock_notion.add_page("page-1", [
            {"id": "b1", "object": "block", "type": "heading_1",
             "heading_1": {"rich_text": [{"type": "text", "text": {"content": "Model Title"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default", "is_toggleable": False}},
            {"id": "b2", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "This is content."}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default"}},
        ])

        # Mock the HTTP layer: blocks fetch + metadata fetch
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass

            async def get(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                if "/blocks/" in url and "/children" in url:
                    return mock_notion.get_block_children("page-1", {})
                if "/pages/" in url:
                    return MockNotionResponse(200, {
                        "object": "page",
                        "id": "page-1",
                        "properties": {
                            "title": {"type": "title", "title": [
                                {"type": "text", "text": {"content": "My Page Title"}, "plain_text": "My Page Title"}
                            ]},
                        },
                    })
                return MockNotionResponse(404, {})

        # Need to patch both notion_fetch and notion modules
        with patch("app.services.notion_fetch.httpx.AsyncClient", MockClient):
            result = await provider.fetch_page_content("page-1", credentials)

        assert result["page_id"] == "page-1"
        assert result["title"] == "My Page Title"
        assert len(result["blocks"]) == 2
        assert "# Model Title" in result["markdown"]
        assert "This is content." in result["markdown"]

    @pytest.mark.asyncio
    async def test_fetch_page_content_no_title(self, provider, mock_notion, credentials):
        """Page without title property — should return empty title."""
        mock_notion.add_page("page-2", [])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass

            async def get(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                if "/blocks/" in url:
                    return mock_notion.get_block_children("page-2", {})
                if "/pages/" in url:
                    return MockNotionResponse(200, {
                        "object": "page",
                        "id": "page-2",
                        "properties": {},  # No title property
                    })
                return MockNotionResponse(404, {})

        with patch("app.services.notion_fetch.httpx.AsyncClient", MockClient):
            result = await provider.fetch_page_content("page-2", credentials)

        assert result["title"] == ""
        assert result["markdown"] == ""

    @pytest.mark.asyncio
    async def test_fetch_page_content_metadata_error_graceful(self, provider, mock_notion, credentials):
        """When metadata fetch fails, title should be empty (not crash)."""
        mock_notion.add_page("page-3", [
            {"id": "b1", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Content"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default"}},
        ])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def get(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                if "/blocks/" in url:
                    return mock_notion.get_block_children("page-3", {})
                if "/pages/" in url:
                    return MockNotionResponse(500, {"error": "Internal error"})
                return MockNotionResponse(404, {})

        with patch("app.services.notion_fetch.httpx.AsyncClient", MockClient):
            result = await provider.fetch_page_content("page-3", credentials)

        # Should not crash — title is empty, content still fetched
        assert result["title"] == ""
        assert len(result["blocks"]) == 1

    @pytest.mark.asyncio
    async def test_fetch_page_metadata(self, provider, credentials):
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def get(self, url, headers=None):
                from tests.mock_notion_api import MockNotionResponse
                return MockNotionResponse(200, {
                    "object": "page",
                    "id": "page-1",
                    "properties": {
                        "title": {"type": "title", "title": [
                            {"type": "text", "text": {"content": "Metadata Title"}, "plain_text": "Metadata Title"}
                        ]},
                    },
                })

        with patch("app.services.notion_fetch.httpx.AsyncClient", MockClient):
            result = await provider.fetch_page_metadata("page-1", credentials)

        assert result["page_id"] == "page-1"
        assert result["title"] == "Metadata Title"


# ─── Create Page ────────────────────────────────────────────────────────────

class TestCreatePage:
    @pytest.mark.asyncio
    async def test_create_page_with_parent(self, provider, credentials):
        """Create a page under a specific parent."""
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass

            async def post(self, url, headers=None, json=None):
                from tests.mock_notion_api import MockNotionResponse
                if "/pages" in url:
                    return MockNotionResponse(200, {"id": "new-page-id", "object": "page"})
                if "/search" in url:
                    return MockNotionResponse(200, {
                        "results": [{
                            "id": "search-result-1",
                            "properties": {"title": {"type": "title", "title": [{"plain_text": "Found Page"}]}},
                        }],
                    })
                return MockNotionResponse(404, {})

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.create_page("My Page", "", credentials, "parent-1")

        assert result["page_id"] == "new-page-id"
        assert result["title"] == "My Page"

    @pytest.mark.asyncio
    async def test_create_page_no_parent_searches(self, provider, credentials):
        """When no parent_page_id given, search for accessible pages first."""
        search_called = False

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass

            async def post(self, url, headers=None, json=None):
                nonlocal search_called
                from tests.mock_notion_api import MockNotionResponse
                if "/search" in url:
                    search_called = True
                    return MockNotionResponse(200, {
                        "results": [{
                            "id": "first-accessible-page",
                            "properties": {"title": {"type": "title", "title": [{"plain_text": "Accessible"}]}},
                        }],
                    })
                if "/pages" in url:
                    return MockNotionResponse(200, {"id": "created-page", "object": "page"})
                return MockNotionResponse(404, {})

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.create_page("Test", "", credentials, parent_page_id=None)

        assert search_called is True
        assert result["page_id"] == "created-page"

    @pytest.mark.asyncio
    async def test_create_page_no_accessible_pages(self, provider, credentials):
        """When no accessible pages found, raise ValueError."""
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, headers=None, json=None):
                from tests.mock_notion_api import MockNotionResponse
                if "/search" in url:
                    return MockNotionResponse(200, {"results": []})
                return MockNotionResponse(404, {})

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            with pytest.raises(ValueError, match="No accessible Notion pages"):
                await provider.create_page("Test", "", credentials, parent_page_id=None)

    @pytest.mark.asyncio
    async def test_list_accessible_pages(self, provider, credentials):
        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def post(self, url, headers=None, json=None):
                from tests.mock_notion_api import MockNotionResponse
                return MockNotionResponse(200, {
                    "results": [
                        {"id": "p1", "properties": {"title": {"type": "title", "title": [{"plain_text": "Page 1"}]}}},
                        {"id": "p2", "properties": {}},  # No title property → "(untitled)"
                    ],
                })

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            pages = await provider.list_accessible_pages(credentials)

        assert len(pages) == 2
        assert pages[0]["id"] == "p1"
        assert pages[0]["title"] == "Page 1"
        assert pages[1]["title"] == "(untitled)"


# ─── Update Content (Provider Level) ─────────────────────────────────────────

class TestUpdateContent:
    @pytest.mark.asyncio
    async def test_update_with_markdown(self, provider, mock_notion, credentials):
        """Full flow: markdown → notion blocks → diff → save."""
        mock_notion.add_page("page-1", [
            {"id": "b1", "object": "block", "type": "paragraph",
             "paragraph": {"rich_text": [{"type": "text", "text": {"content": "Old text"}, "annotations": {"bold": False, "italic": False, "strikethrough": False, "underline": False, "code": False, "color": "default"}}], "color": "default"}},
        ])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def get(self, url, headers=None, params=None):
                resp = mock_notion.handle("GET", url, params=params or {})
                resp.raise_for_status()
                return resp
            async def patch(self, url, headers=None, json=None):
                resp = mock_notion.handle("PATCH", url, json=json or {})
                resp.raise_for_status()
                return resp
            async def delete(self, url, headers=None):
                resp = mock_notion.handle("DELETE", url)
                resp.raise_for_status()
                return resp

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.update_page_content(
                "page-1",
                {"markdown": "# New Heading\n\nUpdated content"},
                credentials,
            )

        assert result["page_id"] == "page-1"
        assert "# New Heading" in result["markdown"]
        assert "Updated content" in result["markdown"]
        assert len(result["blocks"]) == 2  # heading + paragraph

    @pytest.mark.asyncio
    async def test_update_with_title_only(self, provider, mock_notion, credentials):
        """Update only the title, no blocks."""
        mock_notion.add_page("page-1", [])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def get(self, url, headers=None, params=None):
                return mock_notion.handle("GET", url, params=params or {})
            async def patch(self, url, headers=None, json=None):
                resp = mock_notion.handle("PATCH", url, json=json or {})
                resp.raise_for_status()
                return resp
            async def delete(self, url, headers=None):
                resp = mock_notion.handle("DELETE", url)
                resp.raise_for_status()
                return resp

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.update_page_content(
                "page-1",
                {"title": "New Title"},
                credentials,
            )

        assert result["title"] == "New Title"

    @pytest.mark.asyncio
    async def test_update_with_blocks(self, provider, mock_notion, credentials):
        """Update using pre-built blocks instead of markdown."""
        mock_notion.add_page("page-1", [])

        class MockClient:
            def __init__(self, *a, **kw): pass
            async def __aenter__(self): return self
            async def __aexit__(self, *a): pass
            async def get(self, url, headers=None, params=None):
                return mock_notion.handle("GET", url, params=params or {})
            async def patch(self, url, headers=None, json=None):
                resp = mock_notion.handle("PATCH", url, json=json or {})
                resp.raise_for_status()
                return resp
            async def delete(self, url, headers=None):
                resp = mock_notion.handle("DELETE", url)
                resp.raise_for_status()
                return resp

        blocks = [
            {"object": "block", "type": "paragraph", "paragraph": {"rich_text": [
                {"type": "text", "text": {"content": "From blocks"}}
            ]}},
        ]

        with patch("app.services.notion.httpx.AsyncClient", MockClient):
            result = await provider.update_page_content(
                "page-1",
                {"blocks": blocks},
                credentials,
            )

        assert "From blocks" in result["markdown"]
        assert len(result["blocks"]) == 1
