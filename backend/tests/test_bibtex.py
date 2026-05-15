from app.services.bibtex import generate_bibtex, item_type_to_bibtex


def test_item_type_to_bibtex():
    assert item_type_to_bibtex("journalArticle") == "article"
    assert item_type_to_bibtex("book") == "book"
    assert item_type_to_bibtex("conferencePaper") == "inproceedings"
    assert item_type_to_bibtex("unknown") == "misc"


def test_generate_bibtex():
    citation = {
        "bibtex_type": "article",
        "bibtex_key": "zhang2024test",
        "title": "A Test Paper",
        "authors": "Zhang, S. and Li, M.",
        "journal": "Nature",
        "year": 2024,
        "volume": "10",
        "issue": "2",
        "pages": "100-110",
        "doi": "10.1000/test",
        "url": "https://example.com",
    }
    result = generate_bibtex(citation)
    assert "@article{zhang2024test," in result
    assert "title = {A Test Paper}," in result
    assert "author = {Zhang, S. and Li, M.}," in result
    assert "journal = {Nature}," in result
    assert "year = {2024}," in result
    assert "volume = {10}," in result
    assert "number = {2}," in result
    assert "pages = {100-110}," in result
    assert "doi = {10.1000/test}," in result
    assert "url = {https://example.com}," in result


def test_generate_bibtex_auto_key():
    citation = {
        "bibtex_type": "article",
        "bibtex_key": None,
        "title": "Another Paper",
        "authors": "Wang, X.",
        "year": 2023,
    }
    result = generate_bibtex(citation)
    assert "@article{" in result
