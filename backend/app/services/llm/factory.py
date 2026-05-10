import json
from typing import Optional
from app.core.config import get_settings
from app.models import ProviderBinding
from .openai_provider import OpenAIProvider
from .deepseek_provider import DeepseekProvider

settings = get_settings()


def get_provider_for_binding(binding: Optional[ProviderBinding] = None):
    """Return a provider instance using a ProviderBinding or env settings.

    Binding.credentials is expected to be JSON with at least `api_key`.
    """
    if binding:
        creds = {}
        try:
            creds = json.loads(binding.credentials or "{}")
        except Exception:
            creds = {}
        provider_type = binding.provider_type or "openai"
        api_key = creds.get("api_key")
    else:
        provider_type = "openai"
        api_key = settings.OPENAI_API_KEY

    if provider_type == "openai":
        return OpenAIProvider(api_key=api_key)
    elif provider_type == "deepseek":
        return DeepseekProvider(api_key=api_key)
    # Fallback to OpenAI provider for unknown types for now
    return OpenAIProvider(api_key=api_key)
