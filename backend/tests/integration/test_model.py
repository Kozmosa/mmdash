"""Integration tests for model endpoints."""

import pytest


class TestGetModelContent:
    def test_get_content_success(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(
            return_value={"blocks": [{"type": "paragraph", "content": "Hello"}]}
        )
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        response = auth_client.get(f"/api/model/{project.id}/content")
        assert response.status_code == 200
        data = response.json()
        assert data["page_id"] == "page_123"
        assert data["markdown"] == "Hello"

    def test_get_content_project_not_found(self, auth_client):
        response = auth_client.get("/api/model/nonexistent/content")
        assert response.status_code == 404

    def test_get_content_no_model_page(self, auth_client, project):
        response = auth_client.get(f"/api/model/{project.id}/content")
        assert response.status_code == 400
        assert "No model page linked" in response.json()["detail"]

    def test_get_content_provider_not_bound(self, auth_client, project, db):
        project.model_data_page_id = "page_123"
        db.commit()
        # Remove provider binding
        from app.models import ProviderBinding
        db.query(ProviderBinding).delete()
        db.commit()

        response = auth_client.get(f"/api/model/{project.id}/content")
        assert response.status_code == 400
        assert "Please bind a document provider first" in response.json()["detail"]

    def test_get_content_fallback_cache(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"
        db = __import__("sqlalchemy.orm", fromlist=["Session"]).Session.object_session(project)
        db.commit()

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(side_effect=Exception("Provider error"))
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)

        mocker.patch(
            "app.api.model.get_cached_page",
            return_value={"blocks": [{"type": "heading_1", "content": "Cached"}]},
        )

        response = auth_client.get(f"/api/model/{project.id}/content")
        assert response.status_code == 200
        data = response.json()
        assert data["from_cache"] is True
        assert data["markdown"] == "# Cached"

    def test_get_content_no_fallback(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"
        db = __import__("sqlalchemy.orm", fromlist=["Session"]).Session.object_session(project)
        db.commit()

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(side_effect=Exception("Provider error"))
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)

        mocker.patch(
            "app.api.model.get_cached_page",
            return_value=None,
        )

        response = auth_client.get(f"/api/model/{project.id}/content")
        assert response.status_code == 500


class TestExportMarkdown:
    def test_export_markdown(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(
            return_value={"blocks": [{"type": "paragraph", "content": "Content"}]}
        )
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        response = auth_client.get(f"/api/model/{project.id}/export/md")
        assert response.status_code == 200
        assert response.headers["content-type"] == "text/markdown; charset=utf-8"
        assert "attachment" in response.headers["content-disposition"]
        assert response.text == "Content"


class TestLinkModelPage:
    def test_link_page(self, auth_client, project):
        response = auth_client.post(f"/api/model/{project.id}/link", params={"page_id": "new_page_123"})
        assert response.status_code == 200
        assert response.json()["status"] == "linked"
        assert response.json()["model_data_page_id"] == "new_page_123"

    def test_link_page_project_not_found(self, auth_client):
        response = auth_client.post("/api/model/nonexistent/link", params={"page_id": "page"})
        assert response.status_code == 404


class TestCreateAndUpdateModelPage:
    def test_create_page_binds_project(self, auth_client, project, provider_binding, mocker):
        provider_binding.provider_type = "local_file"
        provider_binding.credentials = '{"api_key": "secret"}'
        db = __import__("sqlalchemy.orm", fromlist=["Session"]).Session.object_session(project)
        db.commit()

        mock_provider = mocker.MagicMock()
        mock_provider.create_page = mocker.AsyncMock(return_value={"page_id": "page_local", "title": "Local Model"})
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)

        response = auth_client.post(
            f"/api/model/{project.id}/create-page",
            json={"title": "Local Model"},
        )

        assert response.status_code == 200
        assert response.json()["page_id"] == "page_local"
        db.refresh(project)
        assert project.model_data_page_id == "page_local"

    def test_update_content_with_local_file_provider(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_local"
        provider_binding.provider_type = "local_file"
        provider_binding.credentials = '{"api_key": "secret"}'
        db = __import__("sqlalchemy.orm", fromlist=["Session"]).Session.object_session(project)
        db.commit()

        mock_provider = mocker.MagicMock()
        mock_provider.update_page_content = mocker.AsyncMock(return_value={
            "page_id": "page_local",
            "title": "Local Model",
            "blocks": [{"type": "numbered_list_item", "content": "Step"}],
        })
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)

        response = auth_client.post(
            f"/api/model/{project.id}/content",
            json={"markdown": "1. Step"},
        )

        assert response.status_code == 200
        assert response.json()["markdown"] == "1. Step"
        mock_provider.update_page_content.assert_awaited_once()


class TestAnalyzeSymbols:
    def test_analyze_symbols(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(return_value={"blocks": []})
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        mocker.patch(
            "app.api.model.analyze_symbols",
            return_value=[{"name": "x", "type": "variable"}],
        )

        response = auth_client.get(f"/api/model/{project.id}/analyze/symbols")
        assert response.status_code == 200
        data = response.json()
        assert data["symbols"] == [{"name": "x", "type": "variable"}]
        assert "disclaimer" in data


class TestAnalyzeStructure:
    def test_analyze_structure(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(return_value={"blocks": []})
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        mocker.patch(
            "app.api.model.analyze_structure",
            return_value={"sections": ["Intro", "Method"]},
        )

        response = auth_client.get(f"/api/model/{project.id}/analyze/structure")
        assert response.status_code == 200
        data = response.json()
        assert data["structure"] == {"sections": ["Intro", "Method"]}


class TestExplainFormula:
    def test_explain_formula(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(return_value={"blocks": []})
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        mocker.patch(
            "app.api.model.explain_formula",
            return_value="Energy equals mass times speed of light squared.",
        )

        response = auth_client.post(
            f"/api/model/{project.id}/analyze/formula",
            params={"formula": "E = mc^2"},
        )
        assert response.status_code == 200
        data = response.json()
        assert "Energy equals mass" in data["explanation"]


class TestFindErrors:
    def test_find_errors(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(return_value={"blocks": []})
        mocker.patch("app.api.model.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model.get_cached_page", return_value=None)

        mocker.patch(
            "app.api.model.find_errors",
            return_value=[{"line": 1, "message": "Division by zero"}],
        )

        response = auth_client.get(f"/api/model/{project.id}/analyze/errors")
        assert response.status_code == 200
        data = response.json()
        assert len(data["errors"]) == 1
        assert data["errors"][0]["message"] == "Division by zero"
