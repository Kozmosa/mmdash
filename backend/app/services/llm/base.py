from typing import List, Dict, Any


class BaseProvider:
    """Abstract LLM provider interface."""

    name: str = "base"

    async def list_models(self) -> List[Dict[str, Any]]:
        raise NotImplementedError()

    async def create_chat_completion(self, model: str, messages: list, **kwargs) -> Dict[str, Any]:
        raise NotImplementedError()
