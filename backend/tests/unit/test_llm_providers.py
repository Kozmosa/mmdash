import pytest
from unittest.mock import AsyncMock, patch
import json
from app.services.llm.openai_provider import OpenAIProvider
from app.services.llm.deepseek_provider import DeepseekProvider
from app.services.llm.factory import get_provider_for_binding
from app.models import ProviderBinding


class TestOpenAIProvider:
    def test_init_with_key(self):
        provider = OpenAIProvider(api_key="test-key")
        assert provider.api_key == "test-key"
        assert provider.base_url == "https://api.openai.com/v1"

    def test_init_custom_base_url(self):
        provider = OpenAIProvider(api_key="test-key", base_url="https://custom.com")
        assert provider.base_url == "https://custom.com"

    @pytest.mark.asyncio
    async def test_list_models_without_key(self):
        provider = OpenAIProvider(api_key=None)
        models = await provider.list_models()
        assert models == []

    @pytest.mark.asyncio
    async def test_create_chat_completion_without_key(self):
        provider = OpenAIProvider(api_key=None)
        with pytest.raises(RuntimeError, match="No API key"):
            await provider.create_chat_completion(
                model="gpt-4o-mini",
                messages=[{"role": "user", "content": "test"}]
            )


class TestDeepseekProvider:
    def test_init_with_key(self):
        provider = DeepseekProvider(api_key="test-key")
        assert provider.api_key == "test-key"
        assert provider.base_url == "https://api.deepseek.com/v1"

    def test_init_custom_base_url(self):
        provider = DeepseekProvider(api_key="test-key", base_url="https://custom.com")
        assert provider.base_url == "https://custom.com"

    @pytest.mark.asyncio
    async def test_list_models_without_key(self):
        provider = DeepseekProvider(api_key=None)
        models = await provider.list_models()
        assert models == []


class TestProviderFactory:
    def test_factory_without_binding(self):
        # Should return OpenAI provider with env key
        provider = get_provider_for_binding(binding=None)
        assert isinstance(provider, OpenAIProvider)

    def test_factory_with_openai_binding(self):
        binding = ProviderBinding(
            id="test-id",
            user_id="user-1",
            provider_type="openai",
            credentials=json.dumps({"api_key": "sk-test"})
        )
        provider = get_provider_for_binding(binding)
        assert isinstance(provider, OpenAIProvider)
        assert provider.api_key == "sk-test"

    def test_factory_with_deepseek_binding(self):
        binding = ProviderBinding(
            id="test-id",
            user_id="user-1",
            provider_type="deepseek",
            credentials=json.dumps({"api_key": "sk-deepseek"})
        )
        provider = get_provider_for_binding(binding)
        assert isinstance(provider, DeepseekProvider)
        assert provider.api_key == "sk-deepseek"

    def test_factory_with_invalid_json(self):
        # Should handle gracefully
        binding = ProviderBinding(
            id="test-id",
            user_id="user-1",
            provider_type="openai",
            credentials="invalid json"
        )
        provider = get_provider_for_binding(binding)
        # Should still return a provider (with default/env key)
        assert isinstance(provider, OpenAIProvider)
        # When api_key is None, it falls back to settings.OPENAI_API_KEY (which is "")
        assert provider.api_key == ""
