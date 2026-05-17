import pytest
from unittest.mock import AsyncMock, patch


class TestIMProviderRegistry:
    def test_register_and_get_providers_filters_unconfigured(self):
        from app.services.im_provider import (
            IMProvider, register_im_provider, get_im_providers, _IM_PROVIDER_REGISTRY
        )

        class MockUnconfigured(IMProvider):
            def get_provider_type(self): return "mock"
            def is_configured(self): return False
            async def send_message(self, recipient_type, recipient_id, title, body): return True

        _IM_PROVIDER_REGISTRY.clear()
        register_im_provider("mock", MockUnconfigured)

        providers = get_im_providers()
        assert len(providers) == 0  # is_configured returns False → excluded

    def test_get_providers_returns_configured_only(self):
        from app.services.im_provider import (
            IMProvider, register_im_provider, get_im_providers, _IM_PROVIDER_REGISTRY
        )

        class MockConfigured(IMProvider):
            def get_provider_type(self): return "configured"
            def is_configured(self): return True
            async def send_message(self, recipient_type, recipient_id, title, body): return True

        _IM_PROVIDER_REGISTRY.clear()
        register_im_provider("configured", MockConfigured)

        providers = get_im_providers()
        assert len(providers) == 1
        assert providers[0].get_provider_type() == "configured"

    def test_list_im_providers(self):
        from app.services.im_provider import (
            IMProvider, register_im_provider, list_im_providers, _IM_PROVIDER_REGISTRY
        )

        class MockP(IMProvider):
            def get_provider_type(self): return "test"
            def is_configured(self): return True
            async def send_message(self, recipient_type, recipient_id, title, body): return True

        _IM_PROVIDER_REGISTRY.clear()
        register_im_provider("test", MockP)

        result = list_im_providers()
        assert len(result) == 1
        assert result[0]["type"] == "test"
        assert result[0]["configured"] is True


class TestFeishuCLIProvider:
    def test_provider_type(self):
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.get_provider_type() == "feishu_cli"

    def test_is_configured_no_cli(self, monkeypatch):
        import shutil
        monkeypatch.setattr(shutil, "which", lambda x: None)
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.is_configured() is False

    def test_is_configured_cli_exists_auth_fails(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")
        monkeypatch.setattr(
            "app.services.feishu_cli_provider.subprocess.run",
            lambda *args, **kwargs: type("R", (), {"returncode": 1})(),
        )
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.is_configured() is False

    def test_is_configured_ok(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")
        monkeypatch.setattr(
            "app.services.feishu_cli_provider.subprocess.run",
            lambda *args, **kwargs: type("R", (), {"returncode": 0})(),
        )
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.is_configured() is True

    @pytest.mark.asyncio
    async def test_send_message_success(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")

        async def mock_exec(*args, **kwargs):
            from collections import namedtuple
            Process = namedtuple("Process", ["returncode", "communicate"])
            async def comm():
                return (b"ok", b"")
            return Process(returncode=0, communicate=comm)

        monkeypatch.setattr("asyncio.create_subprocess_exec", mock_exec)

        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        result = await p.send_message("user", "u123", "Test", "Hello")
        assert result is True

    @pytest.mark.asyncio
    async def test_send_message_nonzero_return(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")

        async def mock_exec(*args, **kwargs):
            from collections import namedtuple
            Process = namedtuple("Process", ["returncode", "communicate"])
            async def comm():
                return (b"", b"error msg")
            return Process(returncode=1, communicate=comm)

        monkeypatch.setattr("asyncio.create_subprocess_exec", mock_exec)

        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        result = await p.send_message("user", "u123", "Test", "Hello")
        assert result is False
