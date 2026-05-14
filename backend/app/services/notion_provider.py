import secrets
from typing import Optional

from app.core.config import get_settings
from app.services.document_provider import DocumentProvider, register_provider
from app.services.notion_fetch import (
    fetch_notion_page_content,
    fetch_notion_page_metadata,
    notion_blocks_to_markdown,
)
from app.services.notion import (
    exchange_code_for_token,
    create_page as create_notion_page,
    search_accessible_pages,
    markdown_to_notion_blocks,
    diff_and_apply_blocks,
)

settings = get_settings()


class NotionProvider(DocumentProvider):
    """Notion API document provider."""

    def get_provider_type(self) -> str:
        return "notion"

    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        access_token = credentials["access_token"]
        content = await fetch_notion_page_content(page_id, access_token)
        blocks = content.get("blocks", [])
        markdown = notion_blocks_to_markdown(blocks)
        title = ""
        try:
            meta = await fetch_notion_page_metadata(page_id, access_token)
            for prop in meta.get("properties", {}).values():
                if prop.get("type") == "title":
                    title = "".join(
                        t.get("plain_text", "") for t in prop.get("title", [])
                    )
                    break
        except Exception:
            pass
        return {
            "page_id": page_id,
            "blocks": blocks,
            "markdown": markdown,
            "title": title,
        }

    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        access_token = credentials["access_token"]
        meta = await fetch_notion_page_metadata(page_id, access_token)
        title = ""
        for prop in meta.get("properties", {}).values():
            if prop.get("type") == "title":
                title = "".join(
                    t.get("plain_text", "") for t in prop.get("title", [])
                )
                break
        return {"page_id": page_id, "title": title}

    def get_auth_url(self) -> Optional[str]:
        state = secrets.token_urlsafe(32)
        return (
            f"https://api.notion.com/v1/oauth/authorize?"
            f"client_id={settings.NOTION_CLIENT_ID}&"
            f"redirect_uri={settings.NOTION_REDIRECT_URI}&"
            f"response_type=code&"
            f"owner=user&"
            f"state={state}"
        )

    async def exchange_auth_code(self, code: str) -> dict:
        token_data = await exchange_code_for_token(code)
        return {
            "access_token": token_data.get("access_token"),
            "workspace_id": token_data.get("workspace_id"),
            "workspace_name": token_data.get("workspace_name"),
        }

    async def create_page(self, title: str, content: str, credentials: dict, parent_page_id: str | None = None) -> dict:
        if not parent_page_id:
            pages = await search_accessible_pages(credentials["access_token"])
            if not pages:
                raise ValueError("No accessible Notion pages found. Please share a page with the integration first.")
            parent_page_id = pages[0]["id"]
        page_id = await create_notion_page(parent_page_id, title, credentials["access_token"])
        return {"page_id": page_id, "title": title}

    async def list_accessible_pages(self, credentials: dict) -> list[dict]:
        return await search_accessible_pages(credentials["access_token"])

    async def update_page_content(self, page_id: str, content: dict, credentials: dict) -> dict:
        token = credentials["access_token"]
        title = content.get("title", "")
        blocks = content.get("blocks")
        markdown = content.get("markdown", "")
        if not blocks and markdown:
            blocks = markdown_to_notion_blocks(markdown)

        async with __import__("httpx").AsyncClient() as client:
            if title:
                await client.patch(
                    f"https://api.notion.com/v1/pages/{page_id}",
                    headers={
                        "Authorization": f"Bearer {token}",
                        "Notion-Version": "2022-06-28",
                        "Content-Type": "application/json",
                    },
                    json={"properties": {"title": {"title": [{"text": {"content": title}}]}}},
                )
            if blocks is not None:
                await diff_and_apply_blocks(page_id, blocks, token)

        if markdown:
            result_md = markdown
        elif blocks:
            result_md = notion_blocks_to_markdown(blocks)
        else:
            result_md = ""
        return {
            "page_id": page_id,
            "title": title,
            "blocks": blocks or [],
            "markdown": result_md,
        }


register_provider("notion", NotionProvider)
