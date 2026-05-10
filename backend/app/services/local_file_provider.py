import httpx

from app.core.config import get_settings
from app.services.document_provider import DocumentProvider, register_provider
from app.services.markdown_blocks import blocks_to_markdown

settings = get_settings()


class LocalFileProvider(DocumentProvider):
    """Local filesystem document provider via doc_server HTTP API."""

    def get_provider_type(self) -> str:
        return "local_file"

    def _api_key(self, credentials: dict) -> str:
        return credentials.get("api_key") or settings.DOC_SERVER_API_KEY

    def _headers(self, credentials: dict) -> dict:
        api_key = self._api_key(credentials)
        return {"X-API-Key": api_key} if api_key else {}

    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        url = f"{settings.DOC_SERVER_URL}/api/pages/{page_id}"
        async with httpx.AsyncClient() as client:
            resp = await client.get(
                url,
                headers=self._headers(credentials),
            )
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "blocks": data.get("blocks", []),
            "markdown": data.get("content", ""),
            "title": data.get("title", ""),
        }

    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        content = await self.fetch_page_content(page_id, credentials)
        return {
            "page_id": content["page_id"],
            "title": content.get("title", ""),
        }

    async def create_page(self, title: str, content: str, credentials: dict) -> dict:
        url = f"{settings.DOC_SERVER_URL}/api/pages"
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                url,
                headers=self._headers(credentials),
                json={"title": title, "content": content},
                timeout=10.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {"page_id": data["page_id"], "title": data.get("title", title)}

    async def update_page_content(self, page_id: str, content: dict, credentials: dict) -> dict:
        markdown = content.get("markdown")
        if markdown is None and "blocks" in content:
            markdown = blocks_to_markdown(content.get("blocks", []))

        payload = {}
        if "title" in content:
            payload["title"] = content["title"]
        if markdown is not None:
            payload["content"] = markdown

        url = f"{settings.DOC_SERVER_URL}/api/pages/{page_id}"
        async with httpx.AsyncClient() as client:
            resp = await client.put(
                url,
                headers=self._headers(credentials),
                json=payload,
                timeout=30.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data.get("title", ""),
            "blocks": data.get("blocks", []),
            "markdown": data.get("content", markdown or ""),
        }


register_provider("local_file", LocalFileProvider)
