"""Integration tests for model version endpoints."""

from datetime import datetime, timezone

from app.models import ModelSnapshot


class TestCommitModel:
    def test_commit_success(self, auth_client, project, provider_binding, mocker):
        project.model_data_page_id = "page_123"

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(
            return_value={"blocks": [{"type": "paragraph", "content": "Version 1"}]}
        )
        mocker.patch("app.api.model_version.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model_version.get_cached_page", return_value=None)

        response = auth_client.post(
            f"/api/model-version/{project.id}/commit",
            params={"message": "Initial commit"},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["message"] == "Initial commit"
        assert "id" in data

    def test_commit_no_model_page(self, auth_client, project):
        response = auth_client.post(
            f"/api/model-version/{project.id}/commit",
            params={"message": "Commit"},
        )
        assert response.status_code == 400
        assert "No model page linked" in response.json()["detail"]

    def test_commit_provider_not_bound(self, auth_client, project, db):
        project.model_data_page_id = "page_123"
        db.commit()
        from app.models import ProviderBinding
        db.query(ProviderBinding).delete()
        db.commit()

        response = auth_client.post(
            f"/api/model-version/{project.id}/commit",
            params={"message": "Commit"},
        )
        assert response.status_code == 400
        assert "Please bind a document provider first" in response.json()["detail"]

    def test_commit_project_not_found(self, auth_client):
        response = auth_client.post(
            "/api/model-version/nonexistent/commit",
            params={"message": "Commit"},
        )
        assert response.status_code == 404


class TestListCommits:
    def test_list_commits(self, auth_client, project, db, test_user):
        snapshot = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="First",
            notion_page_id="page_1",
            snapshot_content="# First",
        )
        db.add(snapshot)
        db.commit()

        response = auth_client.get(f"/api/model-version/{project.id}/commits")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["commit_message"] == "First"
        assert data[0]["user_email"] == test_user.email

    def test_list_commits_empty(self, auth_client, project):
        response = auth_client.get(f"/api/model-version/{project.id}/commits")
        assert response.status_code == 200
        assert response.json() == []

    def test_list_commits_ordered(self, auth_client, project, db, test_user):
        s1 = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Older",
            notion_page_id="page_1",
            snapshot_content="old",
            created_at=datetime(2026, 1, 1, tzinfo=timezone.utc),
        )
        s2 = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Newer",
            notion_page_id="page_1",
            snapshot_content="new",
            created_at=datetime(2026, 2, 1, tzinfo=timezone.utc),
        )
        db.add_all([s1, s2])
        db.commit()

        response = auth_client.get(f"/api/model-version/{project.id}/commits")
        data = response.json()
        assert data[0]["commit_message"] == "Newer"
        assert data[1]["commit_message"] == "Older"


class TestDiffCommits:
    def test_diff_commits(self, auth_client, project, db, test_user):
        base = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Base",
            notion_page_id="page_1",
            snapshot_content="line1\nline2",
        )
        compare = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Compare",
            notion_page_id="page_1",
            snapshot_content="line1\nmodified",
        )
        db.add_all([base, compare])
        db.commit()
        db.refresh(base)
        db.refresh(compare)

        response = auth_client.get(
            f"/api/model-version/{project.id}/diff",
            params={"base_id": base.id, "compare_id": compare.id},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["base"]["message"] == "Base"
        assert data["compare"]["message"] == "Compare"
        assert "diff" in data
        assert "modified" in data["diff"]

    def test_diff_snapshot_not_found(self, auth_client, project):
        response = auth_client.get(
            f"/api/model-version/{project.id}/diff",
            params={"base_id": "nonexistent", "compare_id": "nonexistent"},
        )
        assert response.status_code == 404


class TestRollbackModel:
    def test_rollback_preview(self, auth_client, project, provider_binding, db, test_user, mocker):
        project.model_data_page_id = "page_123"
        snapshot = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Target",
            notion_page_id="page_123",
            snapshot_content="line1\nrollback",
        )
        db.add(snapshot)
        db.commit()
        db.refresh(snapshot)

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(
            return_value={"blocks": [{"type": "paragraph", "content": "line1\ncurrent"}]}
        )
        mock_provider.update_page_content = mocker.AsyncMock()
        mocker.patch("app.api.model_version.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model_version.get_cached_page", return_value=None)

        response = auth_client.get(
            f"/api/model-version/{project.id}/rollback-preview",
            params={"snapshot_id": snapshot.id},
        )

        assert response.status_code == 200
        data = response.json()
        assert data["snapshot"]["id"] == snapshot.id
        assert data["can_write"] is True
        assert "rollback" in data["diff"]

    def test_rollback_success(self, auth_client, project, provider_binding, db, test_user, mocker):
        project.model_data_page_id = "page_123"
        snapshot = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Target",
            notion_page_id="page_target",
            snapshot_content="# Target",
        )
        db.add(snapshot)
        db.commit()
        db.refresh(snapshot)

        mock_provider = mocker.MagicMock()
        mock_provider.fetch_page_content = mocker.AsyncMock(
            return_value={"blocks": [{"type": "paragraph", "content": "Current"}]}
        )
        mock_provider.update_page_content = mocker.AsyncMock(
            return_value={"page_id": "page_123", "blocks": [{"type": "paragraph", "content": "Target"}]}
        )
        mocker.patch("app.api.model_version.get_provider", return_value=mock_provider)
        mocker.patch("app.api.model_version.get_cached_page", return_value=None)

        response = auth_client.post(
            f"/api/model-version/{project.id}/rollback",
            params={"snapshot_id": snapshot.id},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "rollback_applied"
        assert data["snapshot_id"] == snapshot.id
        assert "backup_id" in data
        mock_provider.update_page_content.assert_awaited_once_with(
            "page_123",
            {"markdown": "# Target"},
            {"access_token": "notion_token_123", "_token": auth_client.headers["Authorization"].split(" ", 1)[1]},
        )

    def test_rollback_unsupported_provider(self, auth_client, project, provider_binding, db, test_user, mocker):
        from app.services.notion_provider import NotionProvider

        project.model_data_page_id = "page_123"
        snapshot = ModelSnapshot(
            project_id=project.id,
            user_id=test_user.id,
            commit_message="Target",
            notion_page_id="page_123",
            snapshot_content="# Target",
        )
        db.add(snapshot)
        db.commit()
        db.refresh(snapshot)

        mocker.patch("app.api.model_version.get_provider", return_value=NotionProvider())

        response = auth_client.post(
            f"/api/model-version/{project.id}/rollback",
            params={"snapshot_id": snapshot.id},
        )

        assert response.status_code == 400
        assert "does not support rollback writeback" in response.json()["detail"]

    def test_rollback_snapshot_not_found(self, auth_client, project):
        response = auth_client.post(
            f"/api/model-version/{project.id}/rollback",
            params={"snapshot_id": "nonexistent"},
        )
        assert response.status_code == 404
