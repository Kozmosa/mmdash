import asyncio
import logging
import shutil

from app.services.im_provider import IMProvider, register_im_provider

logger = logging.getLogger(__name__)


class FeishuCLIProvider(IMProvider):
    """Feishu/Lark IM provider using the lark-cli tool."""

    def get_provider_type(self) -> str:
        return "feishu_cli"

    def is_configured(self) -> bool:
        if not shutil.which("lark-cli"):
            return False
        try:
            result = asyncio.run(self._auth_status())
            return result.returncode == 0
        except Exception:
            return False

    async def _auth_status(self):
        proc = await asyncio.create_subprocess_exec(
            "lark-cli", "auth", "status",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        await proc.wait()
        return proc

    async def send_message(self, recipient_type: str, recipient_id: str, title: str, body: str) -> bool:
        text = f"{title}\n\n{body}"
        try:
            proc = await asyncio.create_subprocess_exec(
                "lark-cli", "messenger", "send",
                "--recipient-type", recipient_type,
                "--recipient-id", recipient_id,
                "--text", text,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            try:
                stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10.0)
            except asyncio.TimeoutError:
                proc.kill()
                logger.warning("lark-cli send timed out for %s:%s", recipient_type, recipient_id)
                return False

            if proc.returncode != 0:
                logger.error("lark-cli send failed: %s", stderr.decode() if stderr else "unknown")
                return False
            return True
        except FileNotFoundError:
            logger.warning("lark-cli not found, skipping IM send")
            return False
        except Exception:
            logger.exception("Unexpected error sending IM message")
            return False


register_im_provider("feishu_cli", FeishuCLIProvider)
