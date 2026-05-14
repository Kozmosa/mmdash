from __future__ import annotations

import json
from copy import deepcopy
from typing import Any

from sqlalchemy.orm import Session

from app.models import Team

_MATH_DELIMITER_RULE = """
CRITICAL FORMATTING RULE: All mathematical expressions MUST use consistent delimiters:
- Inline math (variables, symbols, subscripts, superscripts, formulas): ALWAYS wrap in $...$, e.g. $x(t)$, $\\lambda(t)$, $\\Delta P(t)$
- Display math (equations, piecewise, multi-line): ALWAYS wrap in $$...$$, e.g. $$\\begin{{cases}}...\\end{{cases}}$$
NEVER use backticks `...` for math — backticks are for inline code only.
NEVER use **bold** or *italic* for variable names — use $...$ instead.
NEVER output raw LaTeX commands without $ or $$ delimiters.
"""

DEFAULT_LLM_PROMPTS: dict[str, str] = {
    "symbols": f"""Analyze the following mathematical modeling document and extract all mathematical symbols used.
For each symbol, provide:
1. The symbol itself (using LaTeX notation)
2. Its meaning/context in the model (explain in Chinese)
3. Its type: \"variable\", \"parameter\", \"constant\", \"function\", or \"operator\"

Return as a JSON array of objects with fields: symbol, meaning, type.
The meaning must be in Chinese. Symbols and LaTeX should remain in English.
{_MATH_DELIMITER_RULE}

Document:
{{content}}
""",
    "structure": f"""Analyze the structure of the following mathematical modeling document.
Provide a Chinese analysis with:
1. A brief overall summary (in Chinese)
2. Key sections identified (section titles in Chinese)
3. How the model relates to the problem statement (in Chinese)

Return as JSON with fields: summary, sections(array), problem_relationship.
All text content must be in Chinese. Technical terms, code, and formulas may remain in English.
{_MATH_DELIMITER_RULE}

Document:
{{content}}
""",
    "formula": f"""Explain the following mathematical formula in detail, breaking down each term and its meaning.

Formula: {{formula}}
Context: {{context}}

Provide a clear, educational explanation in Chinese. Keep variable names, formulas, and mathematical notation in English.
{_MATH_DELIMITER_RULE}
""",
    "errors": f"""Review the following mathematical modeling document for potential errors.
Look for:
1. Mathematical typos or inconsistent notation
2. Logical inconsistencies in the model assumptions
3. Missing constraints or boundary conditions
4. Formula errors or dimension mismatches

For each issue found, provide:
- The relevant text/excerpt (original text from the document)
- A description of the potential error (explain in Chinese)
- A severity level: \"warning\" or \"error\"

Return as a JSON array of objects with fields: excerpt, description, severity.
Description must be in Chinese. Keep code, formulas, and technical terms in English.
If no issues are found, return an empty array.
{_MATH_DELIMITER_RULE}

Document:
{{content}}
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
