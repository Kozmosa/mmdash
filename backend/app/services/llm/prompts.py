from __future__ import annotations

import json
from copy import deepcopy
from typing import Any

from sqlalchemy.orm import Session

from app.models import Team

DEFAULT_LLM_PROMPTS: dict[str, str] = {
    "symbols": """Analyze the following mathematical modeling document and extract all mathematical symbols used.
For each symbol, provide:
1. The symbol itself
2. Its meaning/context in the model
3. Whether it appears to be user-defined or standard notation

Return as a JSON array of objects with fields: symbol, meaning, source(\"user\" or \"inferred\").

Document:
{content}
""",
    "structure": """Analyze the structure of the following mathematical modeling document.
Provide:
1. A brief overall summary
2. Key sections identified
3. How the model relates to the problem statement

Return as JSON with fields: summary, sections(array), problem_relationship.

Document:
{content}
""",
    "formula": """Explain the following mathematical formula in detail, breaking down each term and its meaning.

Formula: {formula}
Context: {context}

Provide a clear, educational explanation suitable for a math modeling team member.
""",
    "errors": """Review the following mathematical modeling document for potential errors.
Look for:
1. Mathematical typos or inconsistent notation
2. Logical inconsistencies in the model assumptions
3. Missing constraints or boundary conditions
4. Formula errors or dimension mismatches

For each issue found, provide:
- The relevant text/excerpt
- A description of the potential error
- A severity level: \"warning\" or \"error\"

Return as a JSON array of objects with fields: excerpt, description, severity.
If no issues are found, return an empty array.

Document:
{content}
""",
}

PROMPT_KEYS = tuple(DEFAULT_LLM_PROMPTS.keys())


def get_default_llm_prompts() -> dict[str, str]:
    return deepcopy(DEFAULT_LLM_PROMPTS)


def normalize_llm_prompts(value: Any) -> dict[str, str]:
    prompts = get_default_llm_prompts()
    raw: dict[str, Any] = {}

    if isinstance(value, str):
        try:
            parsed = json.loads(value)
            if isinstance(parsed, dict):
                raw = parsed
        except Exception:
            raw = {}
    elif isinstance(value, dict):
        raw = value

    for key in PROMPT_KEYS:
        candidate = raw.get(key)
        if isinstance(candidate, str) and candidate.strip():
            prompts[key] = candidate.strip()

    return prompts


def serialize_llm_prompts(prompts: dict[str, str]) -> str:
    normalized = get_default_llm_prompts()
    for key in PROMPT_KEYS:
        value = prompts.get(key)
        if isinstance(value, str) and value.strip():
            normalized[key] = value.strip()
    return json.dumps(normalized, ensure_ascii=False)


def get_team_llm_prompts(db: Session, team_id: str) -> dict[str, str]:
    team = db.query(Team).filter(Team.id == team_id).first()
    if not team:
        return get_default_llm_prompts()
    return normalize_llm_prompts(team.llm_prompts)


def update_team_llm_prompts(db: Session, team_id: str, prompts: dict[str, str]) -> dict[str, str]:
    team = db.query(Team).filter(Team.id == team_id).first()
    if not team:
        raise ValueError("team not found")
    normalized = normalize_llm_prompts(prompts)
    team.llm_prompts = serialize_llm_prompts(normalized)
    db.commit()
    db.refresh(team)
    return normalized
