from pydantic_settings import BaseSettings
from functools import lru_cache


class Settings(BaseSettings):
    APP_NAME: str = "数模Dashboard API"
    DEBUG: bool = False

    DATABASE_URL: str = "sqlite:///./mmdash.db"
    REDIS_URL: str = "redis://localhost:6379/0"

    SECRET_KEY: str = "your-secret-key-change-in-production"
    ALGORITHM: str = "HS256"
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 60 * 24 * 7  # 7 days

    NOTION_CLIENT_ID: str = ""
    NOTION_CLIENT_SECRET: str = ""
    NOTION_REDIRECT_URI: str = "http://localhost:3000/auth/notion/callback"

    OPENAI_API_KEY: str = ""
    OPENAI_BASE_URL: str = ""
    OPENAI_MODEL: str = ""

    DOCUMENT_PROVIDER: str = "notion"  # "notion" or "local_file"
    DOC_SERVER_URL: str = "http://localhost:8002"
    DOC_SERVER_API_KEY: str = ""

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"


@lru_cache()
def get_settings() -> Settings:
    return Settings()
