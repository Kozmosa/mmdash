import pytest
from app.services.zotero_sync import parse_zotero_item, format_authors, extract_year


def test_format_authors():
    creators = [
        {"creatorType": "author", "firstName": "John", "lastName": "Smith"},
        {"creatorType": "author", "firstName": "Jane", "lastName": "Doe"},
    ]
    assert format_authors(creators) == "Smith, John and Doe, Jane"


def test_format_authors_editor():
    creators = [
        {"creatorType": "editor", "firstName": "John", "lastName": "Smith"},
    ]
    assert format_authors(creators) == ""


def test_extract_year():
    assert extract_year("2024") == 2024
    assert extract_year("2024-05") == 2024
    assert extract_year("May 2024") == 2024
    assert extract_year("invalid") is None


def test_parse_zotero_item():
    item = {
        "key": "ABC123",
        "version": 42,
        "data": {
            "itemType": "journalArticle",
            "title": "Test Title",
            "creators": [{"creatorType": "author", "firstName": "X", "lastName": "Y"}],
            "publicationTitle": "Test Journal",
            "date": "2024",
            "volume": "10",
            "issue": "2",
            "pages": "1-10",
            "DOI": "10.1000/test",
            "url": "https://example.com",
            "abstractNote": "Abstract text",
        },
    }
    result = parse_zotero_item(item)
    assert result["zotero_item_key"] == "ABC123"
    assert result["zotero_version"] == 42
    assert result["title"] == "Test Title"
    assert result["bibtex_type"] == "article"
    assert result["journal"] == "Test Journal"
    assert result["year"] == 2024
    assert result["source"] == "zotero"
