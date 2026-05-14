from pydantic import BaseModel, ConfigDict, EmailStr
from datetime import datetime
from typing import Optional


class UserRegister(BaseModel):
    email: EmailStr
    password: str
    username: Optional[str] = None
    display_name: Optional[str] = None


class UserLogin(BaseModel):
    email: EmailStr
    password: str


class UserUpdate(BaseModel):
    display_name: Optional[str] = None
    username: Optional[str] = None


class ChangePasswordRequest(BaseModel):
    current_password: str
    new_password: str


class UserResponse(BaseModel):
    id: str
    email: str
    username: Optional[str]
    display_name: Optional[str]
    created_at: datetime

    model_config = ConfigDict(from_attributes=True)


class TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"


# ─── Provider Auth (replaces Notion-specific) ────────────────────────────────

class ProviderAuthUrl(BaseModel):
    auth_url: str


class ProviderCallback(BaseModel):
    code: str
    provider_type: Optional[str] = None
    team_id: Optional[str] = None


class ProviderSwitchRequest(BaseModel):
    provider_type: str  # "notion" or "local_file"
    team_id: Optional[str] = None  # 团队级 binding


# Backward-compatible aliases
NotionAuthUrl = ProviderAuthUrl
NotionCallback = ProviderCallback
