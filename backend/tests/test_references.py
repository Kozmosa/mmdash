import pytest
from unittest.mock import AsyncMock, MagicMock, patch


def test_create_citation_api(auth_client, project):
    response = auth_client.post(f"/api/references/{project.id}", data={
        "title": "New Paper",
        "authors": '["Wang, X."]',
        "year": "2023",
        "journal": "Science",
    })
    assert response.status_code == 200
    data = response.json()
    assert data["title"] == "New Paper"
    assert data["source"] == "manual"


def test_list_citations(auth_client, project):
    # Create first
    auth_client.post(f"/api/references/{project.id}", data={"title": "Paper 1", "authors": '["A"]'})
    auth_client.post(f"/api/references/{project.id}", data={"title": "Paper 2", "authors": '["B"]'})

    response = auth_client.get(f"/api/references/{project.id}")
    assert response.status_code == 200
    data = response.json()
    assert data["total"] == 2
    assert len(data["items"]) == 2


def test_update_citation(auth_client, project):
    r = auth_client.post(f"/api/references/{project.id}", data={"title": "Old", "authors": '["A"]'})
    cid = r.json()["id"]

    response = auth_client.put(f"/api/references/{project.id}/{cid}", data={"title": "Updated"})
    assert response.status_code == 200
    assert response.json()["title"] == "Updated"


def test_delete_citation(auth_client, project, test_user):
    r = auth_client.post(f"/api/references/{project.id}", data={"title": "To Delete", "authors": '["Test"]'})
    cid = r.json()["id"]

    response = auth_client.delete(f"/api/references/{project.id}/{cid}")
    assert response.status_code == 200

    # Verify deleted
    response = auth_client.get(f"/api/references/{project.id}")
    assert response.json()["total"] == 0


def test_export_bibtex(auth_client, project):
    r = auth_client.post(f"/api/references/{project.id}", data={
        "title": "Export Test",
        "authors": '["Author, A."]',
        "year": "2024",
        "bibtex_key": "test2024",
    })
    cid = r.json()["id"]

    response = auth_client.post(f"/api/references/{project.id}/export", json={"ids": [cid]})
    assert response.status_code == 200
    assert "@article{test2024," in response.text


def test_zotero_config_crud(auth_client, project):
    # Create
    response = auth_client.post(f"/api/references/{project.id}/zotero-config", data={
        "api_key": "secret_key_123",
        "library_id": "12345",
        "library_type": "user",
    })
    assert response.status_code == 200
    data = response.json()
    assert data["library_id"] == "12345"
    assert "****" in data["api_key_masked"]

    # Get
    response = auth_client.get(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 200
    assert response.json()["library_type"] == "user"

    # Delete
    response = auth_client.delete(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 200

    # Get after delete
    response = auth_client.get(f"/api/references/{project.id}/zotero-config")
    assert response.status_code == 404


@pytest.mark.asyncio
async def test_zotero_sync(auth_client, project):
    # Setup config
    auth_client.post(f"/api/references/{project.id}/zotero-config", data={
        "api_key": "test_key",
        "library_id": "12345",
        "library_type": "user",
    })

    def _make_response(items):
        resp = MagicMock()
        resp.status_code = 200
        resp.headers = {"Last-Modified-Version": "42"}
        resp.json.return_value = items
        resp.raise_for_status = MagicMock()
        return resp

    with patch("httpx.AsyncClient") as mock_client:
        mock_instance = AsyncMock()
        mock_instance.__aenter__ = AsyncMock(return_value=mock_instance)
        mock_instance.__aexit__ = AsyncMock(return_value=False)
        # First call returns items, second call returns empty list (end of pagination)
        mock_instance.get.side_effect = [
            _make_response([{
                "key": "ITEM1",
                "version": 10,
                "data": {
                    "itemType": "journalArticle",
                    "title": "Synced Paper",
                    "creators": [{"creatorType": "author", "firstName": "A", "lastName": "B"}],
                    "publicationTitle": "Journal",
                    "date": "2024",
                },
            }]),
            _make_response([]),
        ]
        mock_client.return_value = mock_instance

        response = auth_client.post(f"/api/references/{project.id}/sync")
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "success"

    # Verify citation created
    response = auth_client.get(f"/api/references/{project.id}")
    items = response.json()["items"]
    assert any(c["title"] == "Synced Paper" for c in items)
