ITEM_TYPE_MAP = {
    "journalArticle": "article",
    "book": "book",
    "bookSection": "incollection",
    "conferencePaper": "inproceedings",
    "thesis": "phdthesis",
    "report": "techreport",
    "webpage": "misc",
    "newspaperArticle": "article",
    "magazineArticle": "article",
}


def item_type_to_bibtex(item_type: str) -> str:
    return ITEM_TYPE_MAP.get(item_type, "misc")


import re


def _sanitize_key(text: str) -> str:
    """Keep only ASCII alphanumeric chars for valid BibTeX keys."""
    sanitized = re.sub(r"[^a-zA-Z0-9]", "", text)
    return sanitized or "x"


def generate_bibtex_key(citation: dict) -> str:
    authors = citation.get("authors") or ""
    year = citation.get("year") or ""
    title = citation.get("title") or ""
    first_author = authors.split("and")[0].strip().split(",")[0].strip().lower() if authors else "unknown"
    first_word = title.split()[0].lower() if title else "paper"
    return f"{_sanitize_key(first_author)}{year}{_sanitize_key(first_word)}"


def _escape_bibtex_value(value: str) -> str:
    """Escape braces inside BibTeX field values."""
    return str(value).replace("{", "\\{").replace("}", "\\}")


def generate_bibtex(citation: dict) -> str:
    bib_type = citation.get("bibtex_type") or "misc"
    key = citation.get("bibtex_key") or generate_bibtex_key(citation)

    lines = [f"@{bib_type}{{{key},"]

    field_map = [
        ("title", "title"),
        ("authors", "author"),
        ("journal", "journal"),
        ("year", "year"),
        ("volume", "volume"),
        ("issue", "number"),
        ("pages", "pages"),
        ("doi", "doi"),
        ("url", "url"),
    ]

    for cite_field, bib_field in field_map:
        value = citation.get(cite_field)
        if value:
            safe = _escape_bibtex_value(value)
            lines.append(f"  {bib_field} = {{{safe}}},")

    lines.append("}")
    return "\n".join(lines)


def generate_bibtex_batch(citations: list[dict]) -> str:
    """Generate BibTeX for a batch, disambiguating duplicate keys."""
    seen_keys: dict[str, int] = {}
    entries = []
    for c in citations:
        raw_key = c.get("bibtex_key") or generate_bibtex_key(c)
        if raw_key in seen_keys:
            seen_keys[raw_key] += 1
            disambiguated = f"{raw_key}{seen_keys[raw_key]}"
        else:
            seen_keys[raw_key] = 0
            disambiguated = raw_key
        c_copy = dict(c)
        c_copy["bibtex_key"] = disambiguated
        entries.append(generate_bibtex(c_copy))
    return "\n\n".join(entries)
