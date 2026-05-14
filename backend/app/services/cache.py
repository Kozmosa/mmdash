import json
import redis
from app.core.config import get_settings

settings = get_settings()

# Lazy initialization to allow import without Redis being available
_redis_client = None

def _get_redis():
    global _redis_client
    if _redis_client is None:
        _redis_client = redis.from_url(settings.REDIS_URL, decode_responses=True)
    return _redis_client


def _cache_key(provider_type: str, page_id: str) -> str:
    return f"{provider_type}:page:{page_id}"


def get_cached_page(provider_type: str, page_id: str) -> dict | None:
    try:
        key = _cache_key(provider_type, page_id)
        data = _get_redis().get(key)
        if data:
            return json.loads(data)
    except redis.ConnectionError:
        pass
    return None


def set_cached_page(provider_type: str, page_id: str, content: dict, expire_seconds: int = 300):
    try:
        key = _cache_key(provider_type, page_id)
        _get_redis().setex(key, expire_seconds, json.dumps(content))
    except redis.ConnectionError:
        pass


def invalidate_page(provider_type: str, page_id: str):
    try:
        key = _cache_key(provider_type, page_id)
        _get_redis().delete(key)
    except redis.ConnectionError:
        pass


# ─── Backward-compatible aliases for Notion ──────────────────────────────────

def get_cached_notion_page(page_id: str) -> dict | None:
    """Deprecated: use get_cached_page(provider_type, page_id)."""
    return get_cached_page("notion", page_id)


def set_cached_notion_page(page_id: str, content: dict, expire_seconds: int = 300):
    """Deprecated: use set_cached_page(provider_type, page_id, content)."""
    set_cached_page("notion", page_id, content, expire_seconds)


def invalidate_notion_page(page_id: str):
    """Deprecated: use invalidate_page(provider_type, page_id)."""
    invalidate_page("notion", page_id)


# ─── LLM Analysis Result Cache ──────────────────────────────────────────────

import hashlib

_LLM_CACHE_TTL = 3600 * 24  # 24 hours


def _llm_cache_key(project_id: str, tool: str, content: str) -> str:
    content_hash = hashlib.md5(content[:4000].encode()).hexdigest()
    return f"llm:{project_id}:{tool}:{content_hash}"


def get_cached_llm_result(project_id: str, tool: str, content: str) -> str | None:
    """Get cached LLM analysis result. Returns the raw JSON string or None."""
    try:
        key = _llm_cache_key(project_id, tool, content)
        return _get_redis().get(key)
    except redis.ConnectionError:
        return None


def set_cached_llm_result(project_id: str, tool: str, content: str, result: str):
    """Cache an LLM analysis result."""
    try:
        key = _llm_cache_key(project_id, tool, content)
        _get_redis().setex(key, _LLM_CACHE_TTL, result)
    except redis.ConnectionError:
        pass


def invalidate_llm_cache(project_id: str):
    """Invalidate all LLM cache entries for a project."""
    try:
        r = _get_redis()
        pattern = f"llm:{project_id}:*"
        keys = list(r.scan_iter(match=pattern))
        if keys:
            r.delete(*keys)
    except redis.ConnectionError:
        pass


# ─── Document Content Draft Cache ───────────────────────────────────────────

_DRAFT_CACHE_TTL = 3600 * 24 * 7  # 7 days


def _draft_cache_key(project_id: str) -> str:
    return f"doc:draft:{project_id}"


def get_draft_markdown(project_id: str) -> str | None:
    """Get cached draft markdown for a project."""
    try:
        return _get_redis().get(_draft_cache_key(project_id))
    except redis.ConnectionError:
        return None


def set_draft_markdown(project_id: str, markdown: str):
    """Cache draft markdown for a project (auto-save)."""
    try:
        _get_redis().setex(_draft_cache_key(project_id), _DRAFT_CACHE_TTL, markdown)
    except redis.ConnectionError:
        pass


def delete_draft_markdown(project_id: str):
    """Remove draft cache after explicit save to provider."""
    try:
        _get_redis().delete(_draft_cache_key(project_id))
    except redis.ConnectionError:
        pass
