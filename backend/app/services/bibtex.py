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


def generate_bibtex_key(citation: dict) -> str:
    authors = citation.get("authors") or ""
    year = citation.get("year") or ""
    title = citation.get("title") or ""
    first_author = authors.split("and")[0].strip().split(",")[0].strip().lower() if authors else "unknown"
    first_word = title.split()[0].lower() if title else "paper"
    return f"{first_author}{year}{first_word}"


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
            lines.append(f"  {bib_field} = {{{value}}},")

    lines.append("}")
    return "\n".join(lines)


def generate_bibtex_batch(citations: list[dict]) -> str:
    return "\n\n".join(generate_bibtex(c) for c in citations)
