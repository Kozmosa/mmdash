"""Integration tests for project endpoints."""

import pytest

from app.models import Project, Team, TeamMember, NotionBinding
from tests.conftest import create_test_user, login_user


class TestCreateProject:
    def test_create_project_success(self, auth_client, team, db, test_user):
        from app.models import ProviderBinding
        binding = ProviderBinding(
            user_id=test_user.id,
            team_id=team.id,
            provider_type="local_file",
            credentials="{}",
        )
        db.add(binding)
        db.commit()
        response = auth_client.post(
            "/api/projects",
            json={
                "name": "New Project",
                "description": "Project description",
                "git_remote_url": "https://github.com/test/repo.git",
            },
            params={"team_id": team.id},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["name"] == "New Project"
        assert data["team_id"] == team.id

    def test_create_project_requires_auth(self, client, team):
        response = client.post(
            "/api/projects",
            json={"name": "New Project"},
            params={"team_id": team.id},
        )
        assert response.status_code == 401

    def test_create_project_team_not_found(self, auth_client):
        response = auth_client.post(
            "/api/projects",
            json={"name": "New Project"},
            params={"team_id": "nonexistent"},
        )
        assert response.status_code == 404

    def test_create_project_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other123")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)

        response = auth_client.post(
            "/api/projects",
            json={"name": "New Project"},
            params={"team_id": other_team.id},
        )
        assert response.status_code == 403

    def test_create_project_no_provider_binding(self, auth_client, team):
        response = auth_client.post(
            "/api/projects",
            json={"name": "New Project"},
            params={"team_id": team.id},
        )
        assert response.status_code == 400
        assert "Please configure a team document provider first" in response.json()["detail"]


class TestListProjects:
    def test_list_projects(self, auth_client, project):
        response = auth_client.get("/api/projects", params={"team_id": project.team_id})
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["id"] == project.id

    def test_list_projects_empty(self, auth_client, team):
        response = auth_client.get("/api/projects", params={"team_id": team.id})
        assert response.status_code == 200
        assert response.json() == []

    def test_list_projects_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other2@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other456")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)

        response = auth_client.get("/api/projects", params={"team_id": other_team.id})
        assert response.status_code == 403


class TestGetProject:
    def test_get_project_success(self, auth_client, project):
        response = auth_client.get(f"/api/projects/{project.id}")
        assert response.status_code == 200
        data = response.json()
        assert data["id"] == project.id
        assert data["name"] == project.name

    def test_get_project_not_found(self, auth_client):
        response = auth_client.get("/api/projects/nonexistent")
        assert response.status_code == 404

    def test_get_project_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other3@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other789")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        other_project = Project(team_id=other_team.id, name="Other Project")
        db.add(other_project)
        db.commit()
        db.refresh(other_project)

        response = auth_client.get(f"/api/projects/{other_project.id}")
        assert response.status_code == 403


class TestUpdateProject:
    def test_update_project_success(self, auth_client, project):
        response = auth_client.put(
            f"/api/projects/{project.id}",
            json={
                "name": "Updated Project",
                "description": "Updated description",
                "git_remote_url": "https://github.com/test/updated.git",
            },
        )
        assert response.status_code == 200
        data = response.json()
        assert data["name"] == "Updated Project"
        assert data["description"] == "Updated description"

    def test_update_project_not_found(self, auth_client):
        response = auth_client.put(
            "/api/projects/nonexistent",
            json={"name": "Updated"},
        )
        assert response.status_code == 404

    def test_update_project_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other4@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="otherabc")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        other_project = Project(team_id=other_team.id, name="Other Project")
        db.add(other_project)
        db.commit()
        db.refresh(other_project)

        response = auth_client.put(
            f"/api/projects/{other_project.id}",
            json={"name": "Updated"},
        )
        assert response.status_code == 403
