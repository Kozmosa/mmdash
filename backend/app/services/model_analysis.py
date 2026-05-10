import json
from typing import Any

from fastapi import HTTPException
from sqlalchemy.orm import Session

from app.core.config import get_settings
from app.models import Project, ProviderBinding, User
from app.services.llm.factory import get_provider_for_binding
from app.services.llm.prompts import get_team_llm_prompts

settings = get_settings()

LLM_PROVIDER_TYPES = ("openai", "deepseek")
DEFAULT_ENV_MODEL = "gpt-4o-mini"


def _load_credentials(binding: ProviderBinding | None) -> dict[str, Any]:
    if not binding:
        return {}
    try:
        parsed = json.loads(binding.credentials or "{}")
    except Exception:
        return {}
    return parsed if isinstance(parsed, dict) else {}


def _find_llm_binding(project: Project, current_user: User, db: Session) -> ProviderBinding | None:
    team_binding = (
        db.query(ProviderBinding)
        .filter(
            ProviderBinding.team_id == project.team_id,
            ProviderBinding.provider_type.in_(LLM_PROVIDER_TYPES),
        )
        .order_by(ProviderBinding.created_at.desc())
        .first()
    )
    if team_binding:
        return team_binding

    return (
        db.query(ProviderBinding)
        .filter(
            ProviderBinding.user_id == current_user.id,
            ProviderBinding.team_id.is_(None),
            ProviderBinding.provider_type.in_(LLM_PROVIDER_TYPES),
        )
        .order_by(ProviderBinding.created_at.desc())
        .first()
    )


def _get_provider_and_model(project: Project, current_user: User, db: Session):
    binding = _find_llm_binding(project, current_user, db)
    if binding:
        credentials = _load_credentials(binding)
        selected_model = credentials.get("selected_model")
        if not selected_model:
            raise HTTPException(status_code=400, detail="请先在设置页选择要使用的模型")
        return get_provider_for_binding(binding), selected_model

    if settings.OPENAI_API_KEY:
        return get_provider_for_binding(None), DEFAULT_ENV_MODEL

    raise HTTPException(status_code=400, detail="请先在设置页配置模型服务")


def _extract_message_content(response: dict[str, Any]) -> str:
    try:
        content = response["choices"][0]["message"]["content"]
    except Exception:
        return ""
    return content if isinstance(content, str) else ""


def _parse_json_content(content: str) -> Any:
    if not content:
        return {}
    try:
        return json.loads(content)
    except Exception:
        start = content.find("{")
        end = content.rfind("}")
        if start >= 0 and end > start:
            try:
                return json.loads(content[start : end + 1])
            except Exception:
                return {}
    return {}


async def _chat(project: Project, current_user: User, db: Session, prompt: str, json_response: bool) -> str:
    provider, model = _get_provider_and_model(project, current_user, db)
    kwargs: dict[str, Any] = {"temperature": 0.3}
    if json_response:
        kwargs["response_format"] = {"type": "json_object"}
    try:
        response = await provider.create_chat_completion(
            model=model,
            messages=[{"role": "user", "content": prompt}],
            **kwargs,
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"LLM provider error: {str(e)}")
    return _extract_message_content(response)


async def analyze_symbols_with_configured_model(
    markdown_text: str,
    project: Project,
    current_user: User,
    db: Session,
) -> list[dict[str, Any]]:
    prompt = get_team_llm_prompts(db, project.team_id)["symbols"].format(content=markdown_text[:4000])
    content = await _chat(project, current_user, db, prompt, True)
    data = _parse_json_content(content)
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        symbols = data.get("symbols", [])
        return symbols if isinstance(symbols, list) else []
    return []


async def analyze_structure_with_configured_model(
    markdown_text: str,
    project: Project,
    current_user: User,
    db: Session,
) -> dict[str, Any]:
    prompt = get_team_llm_prompts(db, project.team_id)["structure"].format(content=markdown_text[:4000])
    content = await _chat(project, current_user, db, prompt, True)
    data = _parse_json_content(content)
    return data if isinstance(data, dict) else {}


async def explain_formula_with_configured_model(
    formula: str,
    context: str,
    project: Project,
    current_user: User,
    db: Session,
) -> str:
    prompt = get_team_llm_prompts(db, project.team_id)["formula"].format(formula=formula, context=context)
    return await _chat(project, current_user, db, prompt, False)


async def find_errors_with_configured_model(
    markdown_text: str,
    project: Project,
    current_user: User,
    db: Session,
) -> list[dict[str, Any]]:
    prompt = get_team_llm_prompts(db, project.team_id)["errors"].format(content=markdown_text[:4000])
    content = await _chat(project, current_user, db, prompt, True)
    data = _parse_json_content(content)
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        errors = data.get("errors", [])
        return errors if isinstance(errors, list) else []
    return []
