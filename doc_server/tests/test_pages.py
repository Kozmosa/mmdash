from fastapi.testclient import TestClient

from doc_server.api import pages
from doc_server.main import app


def _client(tmp_path, monkeypatch, api_key=""):
    monkeypatch.setattr(pages.settings, "DATA_DIR", str(tmp_path), raising=False)
    monkeypatch.setattr(pages.settings, "API_KEY", api_key, raising=False)
    return TestClient(app)


def test_create_page_without_api_key(tmp_path, monkeypatch):
    client = _client(tmp_path, monkeypatch)

    response = client.post("/api/pages", json={"title": "Model", "content": "# Title"})

    assert response.status_code == 200
    data = response.json()
    assert data["title"] == "Model"
    assert data["blocks"] == [{"type": "heading_1", "content": "Title"}]


def test_update_page_title_and_content(tmp_path, monkeypatch):
    client = _client(tmp_path, monkeypatch, api_key="secret")
    headers = {"X-API-Key": "secret"}
    created = client.post("/api/pages", headers=headers, json={"title": "Model", "content": "Old"})
    page_id = created.json()["page_id"]

    response = client.put(
        f"/api/pages/{page_id}",
        headers=headers,
        json={"title": "Updated", "content": "1. Step"},
    )

    assert response.status_code == 200
    data = response.json()
    assert data["title"] == "Updated"
    assert data["content"] == "1. Step"
    assert data["blocks"] == [{"type": "numbered_list_item", "content": "Step"}]


def test_markdown_parser_covers_model_page_blocks(tmp_path, monkeypatch):
    client = _client(tmp_path, monkeypatch)
    markdown = "\n".join([
        "# Title",
        "## Section",
        "### Subsection",
        "Body",
        "- Bullet",
        "1. Numbered",
        "```python",
        "print('hi')",
        "```",
        "$$ E = mc^2 $$",
        "> Quote",
        "---",
    ])

    response = client.post("/api/pages", json={"title": "Model", "content": markdown})

    assert response.status_code == 200
    block_types = [block["type"] for block in response.json()["blocks"]]
    assert block_types == [
        "heading_1",
        "heading_2",
        "heading_3",
        "paragraph",
        "bulleted_list_item",
        "numbered_list_item",
        "code",
        "equation",
        "quote",
        "divider",
    ]
