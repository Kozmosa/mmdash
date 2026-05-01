from abc import ABC, abstractmethod
from typing import Optional


class DocumentProvider(ABC):
    """Abstract base class for document providers.

    All document providers (Notion, local file system, etc.) must implement
    this interface to be used interchangeably by the backend.
    """

    @abstractmethod
    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        """Fetch page content.

        Returns a dict with at minimum:
        - page_id: str
        - blocks: list[dict] (Notion-compatible block structures)

        May also include title, metadata, etc.
        """
        pass

    @abstractmethod
    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        """Fetch page metadata (title, properties, etc.)."""
        pass

    @abstractmethod
    def get_provider_type(self) -> str:
        """Return provider type identifier, e.g. 'notion', 'local_file'."""
        pass

    def get_auth_url(self) -> Optional[str]:
        """Return OAuth authorization URL if this provider uses OAuth.

        Returns None if no OAuth flow is needed (e.g. API key auth).
        """
        return None

    async def exchange_auth_code(self, code: str) -> dict:
        """Exchange OAuth authorization code for access credentials.

        Returns a dict with credentials (e.g. {"access_token": "..."}).
        Raises NotImplementedError if OAuth is not supported.
        """
        raise NotImplementedError("OAuth not supported by this provider")

    async def create_page(self, title: str, content: str, credentials: dict) -> dict:
        """Create a new document in the provider.

        Args:
            title: Document title.
            content: Initial Markdown content (may be empty).
            credentials: Provider-specific credentials dict.

        Returns:
            {"page_id": str, "title": str}

        Raises:
            NotImplementedError: if provider does not support creation.
        """
        raise NotImplementedError("Create not supported by this provider")

    async def update_page_content(
        self, page_id: str, content: dict, credentials: dict
    ) -> dict:
        """Update document content (full replacement).

        Args:
            page_id: Document identifier.
            content: Dict with optional keys:
                - "title": str (optional)
                - "markdown": str (mutually exclusive with "blocks")
                - "blocks": list[dict] (mutually exclusive with "markdown")
            credentials: Provider-specific credentials dict.

        Returns:
            {"page_id": str, "title": str, "blocks": list, "markdown": str}

        Raises:
            NotImplementedError: if provider does not support updates.
        """
        raise NotImplementedError("Update not supported by this provider")


_PROVIDER_REGISTRY: dict[str, type[DocumentProvider]] = {}


def register_provider(provider_type: str, cls: type[DocumentProvider]):
    """Register a provider implementation."""
    _PROVIDER_REGISTRY[provider_type] = cls


def get_provider(provider_type: str) -> DocumentProvider:
    """Factory: instantiate a provider by type string."""
    if provider_type not in _PROVIDER_REGISTRY:
        raise ValueError(f"Unknown provider type: {provider_type}")
    return _PROVIDER_REGISTRY[provider_type]()


def list_providers() -> list[str]:
    """List all registered provider types."""
    return list(_PROVIDER_REGISTRY.keys())
