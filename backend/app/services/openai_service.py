import json
import openai
from app.core.config import get_settings
from app.services.llm.prompts import DEFAULT_LLM_PROMPTS

settings = get_settings()
openai_client = openai.AsyncOpenAI(api_key=settings.OPENAI_API_KEY) if settings.OPENAI_API_KEY else None


def _render_prompt(template: str, **values: str) -> str:
    rendered = template
    for key, value in values.items():
        rendered = rendered.replace(f"{{{key}}}", value)
    return rendered


async def analyze_symbols(markdown_text: str, prompt: str | None = None) -> list:
    """Extract symbols and their meanings from model document."""
    if not openai_client:
        return []
    prompt = _render_prompt(prompt or DEFAULT_LLM_PROMPTS["symbols"], content=markdown_text[:4000])
    try:
        resp = await openai_client.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": prompt}],
            response_format={"type": "json_object"},
            temperature=0.3,
        )
        content = resp.choices[0].message.content
        data = json.loads(content)
        return data.get("symbols", [])
    except Exception:
        return []


async def analyze_structure(markdown_text: str, prompt: str | None = None) -> dict:
    """Analyze overall structure of the model document."""
    if not openai_client:
        return {}
    prompt = _render_prompt(prompt or DEFAULT_LLM_PROMPTS["structure"], content=markdown_text[:4000])
    try:
        resp = await openai_client.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": prompt}],
            response_format={"type": "json_object"},
            temperature=0.3,
        )
        content = resp.choices[0].message.content
        return json.loads(content)
    except Exception:
        return {}


async def explain_formula(formula: str, context: str = "", prompt: str | None = None) -> str:
    """Explain a mathematical formula in plain language."""
    if not openai_client:
        return "LLM service not configured."
    prompt = _render_prompt(prompt or DEFAULT_LLM_PROMPTS["formula"], formula=formula, context=context)
    try:
        resp = await openai_client.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": prompt}],
            temperature=0.3,
        )
        return resp.choices[0].message.content
    except Exception as e:
        return f"Explanation failed: {str(e)}"


async def find_errors(markdown_text: str, prompt: str | None = None) -> list:
    """Find logical errors and typos in the model document."""
    if not openai_client:
        return []
    prompt = _render_prompt(prompt or DEFAULT_LLM_PROMPTS["errors"], content=markdown_text[:4000])
    try:
        resp = await openai_client.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": prompt}],
            response_format={"type": "json_object"},
            temperature=0.3,
        )
        content = resp.choices[0].message.content
        data = json.loads(content)
        return data.get("errors", [])
    except Exception:
        return []
