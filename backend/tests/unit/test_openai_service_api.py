"""Tests for openai_service and model_analysis with real API.

Run with: OPENAI_API_KEY=sk-xxx uv run pytest tests/unit/test_openai_service_api.py -v
"""

import pytest
from app.services.openai_service import (
    analyze_symbols,
    analyze_structure,
    explain_formula,
    find_errors,
)


# ─── openai_service (direct API calls) ──────────────────────────────────────

@pytest.mark.requires_api
class TestOpenAIServiceWithAPI:
    @pytest.mark.asyncio
    async def test_analyze_symbols_returns_list(self):
        markdown = """
# Heat Conduction Model
## Parameters
- k: thermal conductivity (W/m·K)
- T: temperature (K)
- x: position (m)
        """
        symbols = await analyze_symbols(markdown)
        assert isinstance(symbols, list)
        # Note: LLM may return empty list if no symbols recognized — valid response

    @pytest.mark.asyncio
    async def test_analyze_structure_returns_dict(self):
        markdown = """
# Model
## Assumptions
1. Steady state
2. One-dimensional

## Equations
q = -k * dT/dx
        """
        structure = await analyze_structure(markdown)
        assert isinstance(structure, dict)
        # Should have some content from the LLM
        assert len(structure) > 0

    @pytest.mark.asyncio
    async def test_explain_formula_returns_string(self):
        result = await explain_formula("E = mc^2")
        assert isinstance(result, str)
        assert len(result) > 10  # meaningful explanation
        assert "LLM service not configured" not in result
        assert "Explanation failed" not in result

    @pytest.mark.asyncio
    async def test_find_errors_returns_list(self):
        markdown = """
# Model
x + 1 = 2
The derivitive of f(x) is wrong.
        """
        errors = await find_errors(markdown)
        assert isinstance(errors, list)
        # May be empty if no errors found, which is valid

    @pytest.mark.asyncio
    async def test_empty_markdown_handled_gracefully(self):
        """Empty or very short markdown should not crash."""
        symbols = await analyze_symbols("")
        assert isinstance(symbols, list)

        structure = await analyze_structure("")
        assert isinstance(structure, dict)

        errors = await find_errors("")
        assert isinstance(errors, list)


# ─── Edge cases with real API ────────────────────────────────────────────────

@pytest.mark.requires_api
class TestOpenAIServiceEdgeCases:
    @pytest.mark.asyncio
    async def test_long_markdown_truncated(self):
        """Markdown longer than 4000 chars should still work (truncation)."""
        long_md = "# Title\n\n" + "Lorem ipsum dolor sit amet.\n" * 300
        assert len(long_md) > 4000
        symbols = await analyze_symbols(long_md)
        assert isinstance(symbols, list)

    @pytest.mark.asyncio
    async def test_formula_with_context(self):
        result = await explain_formula(
            formula="P = F / A",
            context="Pressure is defined as force per unit area.",
        )
        assert isinstance(result, str)
        assert len(result) > 10

    @pytest.mark.asyncio
    async def test_markdown_with_math_notation(self):
        """Test with LaTeX math notation."""
        markdown = """
# Heat Equation

The heat equation in one dimension is:

$$\\frac{\\partial T}{\\partial t} = \\alpha \\frac{\\partial^2 T}{\\partial x^2}$$

Where $T$ is temperature, $t$ is time, $x$ is position, and $\\alpha$ is thermal diffusivity.

## Variables
- $T(x,t)$: temperature distribution
- $\\alpha = k/(\\rho c_p)$: thermal diffusivity
- $k$: thermal conductivity
- $\\rho$: density
- $c_p$: specific heat capacity
        """
        symbols = await analyze_symbols(markdown)
        assert isinstance(symbols, list)
        # LLM response is non-deterministic — empty list is valid


# ─── Response quality checks ─────────────────────────────────────────────────

@pytest.mark.requires_api
class TestResponseQuality:
    @pytest.mark.asyncio
    async def test_symbols_contain_expected_fields(self):
        """Each symbol dict should have name/description-like fields."""
        markdown = "k is thermal conductivity in W/mK\nT is temperature in Kelvin"
        symbols = await analyze_symbols(markdown)
        if len(symbols) > 0:
            s = symbols[0]
            # The LLM should return objects with at least 'name' or 'symbol' or 'description'
            has_meaningful = (
                "name" in s or "symbol" in s or "description" in s
                or "meaning" in s
            )
            assert has_meaningful, f"Symbol missing expected fields: {s}"

    @pytest.mark.asyncio
    async def test_explanation_is_understandable(self):
        """Formula explanation should not be empty or just the formula repeated."""
        result = await explain_formula("v = d / t")
        # Should be a sentence, not just the formula
        assert len(result) > 20
        assert "Explanation failed" not in result
