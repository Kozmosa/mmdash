import json
from typing import List, Dict, Any, Optional
import httpx
from app.core.config import get_settings
from .base import BaseProvider

settings = get_settings()


class OpenAIProvider(BaseProvider):
    name = "openai"

    def __init__(self, api_key: Optional[str] = None, base_url: Optional[str] = None):
        self.api_key = api_key or settings.OPENAI_API_KEY
        self.base_url = base_url or "https://api.openai.com/v1"

    async def list_models(self) -> List[Dict[str, Any]]:
        if not self.api_key:
            return []
        url = f"{self.base_url}/models"
        headers = {"Authorization": f"Bearer {self.api_key}"}
        async with httpx.AsyncClient() as client:
            resp = await client.get(url, headers=headers, timeout=30)
            resp.raise_for_status()
            data = resp.json()
            models = []
            for m in data.get("data", []):
                models.append({"id": m.get("id"), "description": m.get("description")})
            return models

    async def create_chat_completion(self, model: str, messages: list, **kwargs) -> Dict[str, Any]:
        if not self.api_key:
            raise RuntimeError("No API key configured for OpenAI provider")
        url = f"{self.base_url}/chat/completions"
        headers = {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"}
        payload = {"model": model, "messages": messages}
        payload.update(kwargs)
        async with httpx.AsyncClient() as client:
            resp = await client.post(url, headers=headers, json=payload, timeout=120)
            resp.raise_for_status()
            return resp.json()
