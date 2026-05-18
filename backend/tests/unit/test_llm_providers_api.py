"""Tests requiring a real LLM API key (OPENAI_API_KEY in .env).

These tests make real HTTP calls to the configured API endpoint.
Run with: uv run pytest tests/unit/test_llm_providers_api.py -v
Skip automatically when OPENAI_API_KEY is empty.
"""

import pytest
from app.services.llm.openai_provider import OpenAIProvider
from app.services.llm.deepseek_provider import DeepseekProvider


def _get_settings():
    from app.core.config import get_settings
    return get_settings()


# ─── OpenAIProvider (with real API key + custom base_url) ────────────────────

@pytest.mark.requires_api
class TestOpenAIProviderWithAPI:
    @pytest.mark.asyncio
    async def test_list_models_returns_models(self):
        s = _get_settings()
        provider = OpenAIProvider(api_key=s.OPENAI_API_KEY, base_url=s.OPENAI_BASE_URL)
        models = await provider.list_models()
        assert isinstance(models, list)
        assert len(models) > 0
        for m in models:
            assert "id" in m

    @pytest.mark.asyncio
    async def test_create_chat_completion_returns_response(self):
        s = _get_settings()
        provider = OpenAIProvider(api_key=s.OPENAI_API_KEY, base_url=s.OPENAI_BASE_URL)
        model = s.OPENAI_MODEL or "deepseek-chat"
        result = await provider.create_chat_completion(
            model=model,
            messages=[{"role": "user", "content": "Say hello in exactly one word."}],
            max_tokens=10,
        )
        assert "choices" in result
        assert len(result["choices"]) > 0
        assert "message" in result["choices"][0]
        content = result["choices"][0]["message"]["content"]
        assert len(content) > 0

    @pytest.mark.asyncio
    async def test_create_chat_completion_with_system_prompt(self):
        s = _get_settings()
        provider = OpenAIProvider(api_key=s.OPENAI_API_KEY, base_url=s.OPENAI_BASE_URL)
        model = s.OPENAI_MODEL or "deepseek-chat"
        result = await provider.create_chat_completion(
            model=model,
            messages=[
                {"role": "system", "content": "You are a math tutor. Answer with numbers only."},
                {"role": "user", "content": "What is 2+2?"},
            ],
            max_tokens=10,
        )
        content = result["choices"][0]["message"]["content"]
        assert "4" in content


# ─── DeepseekProvider (with real API key) ────────────────────────────────────

@pytest.mark.requires_api
class TestDeepseekProviderWithAPI:
    @pytest.mark.asyncio
    async def test_list_models_returns_models(self):
        s = _get_settings()
        provider = DeepseekProvider(api_key=s.OPENAI_API_KEY)
        models = await provider.list_models()
        assert isinstance(models, list)
        assert len(models) > 0
        for m in models:
            assert "id" in m

    @pytest.mark.asyncio
    async def test_create_chat_completion_works(self):
        s = _get_settings()
        provider = DeepseekProvider(api_key=s.OPENAI_API_KEY)
        model = s.OPENAI_MODEL or "deepseek-chat"
        result = await provider.create_chat_completion(
            model=model,
            messages=[{"role": "user", "content": "Reply with exactly: OK"}],
            max_tokens=5,
        )
        assert "choices" in result
        assert len(result["choices"]) > 0


# ─── Cross-provider behavior ─────────────────────────────────────────────────

@pytest.mark.requires_api
class TestProviderConsistency:
    @pytest.mark.asyncio
    async def test_openai_and_deepseek_both_work_with_same_key(self):
        s = _get_settings()
        openai_p = OpenAIProvider(api_key=s.OPENAI_API_KEY, base_url=s.OPENAI_BASE_URL)
        deepseek_p = DeepseekProvider(api_key=s.OPENAI_API_KEY)
        openai_models = await openai_p.list_models()
        deepseek_models = await deepseek_p.list_models()
        assert len(openai_models) > 0
        assert len(deepseek_models) > 0
