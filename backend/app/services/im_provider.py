from abc import ABC, abstractmethod


class IMProvider(ABC):
    """Abstract base class for IM notification providers."""

    @abstractmethod
    async def send_message(self, recipient_type: str, recipient_id: str, title: str, body: str) -> bool:
        """Send a text message. recipient_type is 'user' or 'chat'.
        Returns True on success, False on failure.
        """

    @abstractmethod
    def get_provider_type(self) -> str:
        """Return provider type identifier, e.g. 'feishu_cli'."""

    @abstractmethod
    def is_configured(self) -> bool:
        """Check prerequisites: CLI installed, authenticated. Called synchronously."""


_IM_PROVIDER_REGISTRY: dict[str, type[IMProvider]] = {}


def register_im_provider(provider_type: str, cls: type[IMProvider]):
    """Register an IM provider implementation."""
    _IM_PROVIDER_REGISTRY[provider_type] = cls


def get_im_providers() -> list[IMProvider]:
    """Return all configured (installed + logged-in) IM providers."""
    return [
        cls() for cls in _IM_PROVIDER_REGISTRY.values()
        if cls().is_configured()
    ]


def list_im_providers() -> list[dict]:
    """List all registered providers with their configuration status."""
    return [
        {
            "type": cls().get_provider_type(),
            "configured": cls().is_configured(),
            "name": cls().get_provider_type(),
        }
        for cls in _IM_PROVIDER_REGISTRY.values()
    ]
