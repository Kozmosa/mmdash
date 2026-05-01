import httpx

from app.services.document_provider import DocumentProvider, register_provider


class DocumosaProvider(DocumentProvider):
    """Documosa document provider via REST API."""

    def get_provider_type(self) -> str:
        return "documosa"

    def _token(self, credentials: dict) -> str:
        return credentials.get("_token", "")

    def _base_url(self, credentials: dict) -> str:
        return credentials.get("base_url", "http://localhost:3000").rstrip("/")

    def _headers(self, credentials: dict) -> dict:
        return {"Authorization": f"Bearer {self._token(credentials)}"}

    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        url = f"{self._base_url(credentials)}/api/mmdash/documents/{page_id}/content"
        async with httpx.AsyncClient() as client:
            resp = await client.get(url, headers=self._headers(credentials), timeout=30.0)
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data.get("title", ""),
            "blocks": data.get("blocks", []),
        }

    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        url = f"{self._base_url(credentials)}/api/mmdash/documents/{page_id}"
        async with httpx.AsyncClient() as client:
            resp = await client.get(url, headers=self._headers(credentials), timeout=10.0)
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data.get("title", ""),
        }

    async def create_page(self, title: str, content: str, credentials: dict) -> dict:
        url = f"{self._base_url(credentials)}/api/mmdash/documents"
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                url,
                headers=self._headers(credentials),
                json={"title": title, "content": content},
                timeout=10.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {"page_id": data["page_id"], "title": data["title"]}

    async def update_page_content(self, page_id: str, content: dict, credentials: dict) -> dict:
        url = f"{self._base_url(credentials)}/api/mmdash/documents/{page_id}/content"
        payload = {}
        if "title" in content:
            payload["title"] = content["title"]
        if "blocks" in content:
            payload["blocks"] = content["blocks"]
        elif "markdown" in content:
            payload["markdown"] = content["markdown"]

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
            "markdown": data.get("markdown", ""),
        }


register_provider("documosa", DocumosaProvider)
