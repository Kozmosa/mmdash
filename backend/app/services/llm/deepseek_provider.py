import json
from typing import List, Dict, Any, Optional
import httpx
from .base import BaseProvider


class DeepseekProvider(BaseProvider):
    """Deepseek LLM Provider (OpenAI-compatible API)."""
    
    name = "deepseek"

    def __init__(self, api_key: Optional[str] = None, base_url: Optional[str] = None):
        self.api_key = api_key
        # Deepseek uses OpenAI-compatible API with different base URL
        self.base_url = base_url or "https://api.deepseek.com/v1"

    async def list_models(self) -> List[Dict[str, Any]]:
        if not self.api_key:
            return []
        url = f"{self.base_url}/models"
        headers = {"Authorization": f"Bearer {self.api_key}"}
        try:
            async with httpx.AsyncClient() as client:
                resp = await client.get(url, headers=headers, timeout=30)
                resp.raise_for_status()
                data = resp.json()
                models = []
                for m in data.get("data", []):
                    models.append({"id": m.get("id"), "description": m.get("description", "")})
                return models
        except Exception as e:
            # Return empty list on error instead of raising
            return []

    async def create_chat_completion(self, model: str, messages: list, **kwargs) -> Dict[str, Any]:
        if not self.api_key:
            raise RuntimeError("No API key configured for Deepseek provider")
        url = f"{self.base_url}/chat/completions"
        headers = {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"}
        payload = {"model": model, "messages": messages}
        payload.update(kwargs)
        async with httpx.AsyncClient() as client:
            resp = await client.post(url, headers=headers, json=payload, timeout=120)
            resp.raise_for_status()
            return resp.json()
