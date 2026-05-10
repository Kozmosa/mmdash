"""Integration tests for team endpoints."""

import pytest

from app.models import Team, TeamMember
from tests.conftest import create_test_user, login_user


class TestCreateTeam:
    def test_create_team_success(self, auth_client):
        response = auth_client.post("/api/teams", json={"name": "My Team"})
        assert response.status_code == 200
        data = response.json()
        assert data["name"] == "My Team"
        assert "invite_code" in data
        assert "id" in data

    def test_create_team_requires_auth(self, client):
        response = client.post("/api/teams", json={"name": "My Team"})
        assert response.status_code == 401

    def test_create_team_empty_name(self, auth_client):
        response = auth_client.post("/api/teams", json={"name": ""})
        assert response.status_code == 200  # Pydantic allows empty string


class TestListTeams:
    def test_list_teams(self, auth_client, team):
        response = auth_client.get("/api/teams")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["id"] == team.id

    def test_list_teams_empty(self, auth_client):
        response = auth_client.get("/api/teams")
        assert response.status_code == 200
        assert response.json() == []

    def test_list_teams_requires_auth(self, client):
        response = client.get("/api/teams")
        assert response.status_code == 401


class TestGetTeam:
    def test_get_team_success(self, auth_client, team):
        response = auth_client.get(f"/api/teams/{team.id}")
        assert response.status_code == 200
        data = response.json()
        assert data["id"] == team.id
        assert data["name"] == team.name

    def test_get_team_not_found(self, auth_client):
        response = auth_client.get("/api/teams/nonexistent")
        assert response.status_code == 404

    def test_get_team_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other123")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        response = auth_client.get(f"/api/teams/{other_team.id}")
        assert response.status_code == 403


class TestJoinTeam:
    def test_join_team_success(self, client, db, team, test_user):
        other_user = create_test_user(db, email="joiner@example.com", password="pass123")
        token = login_user(client, other_user.email, "pass123")
        response = client.post(
            "/api/teams/join",
            json={"invite_code": team.invite_code},
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 200
        assert response.json()["status"] == "joined"
        assert response.json()["team_id"] == team.id

    def test_join_team_invalid_code(self, auth_client):
        response = auth_client.post("/api/teams/join", json={"invite_code": "invalid"})
        assert response.status_code == 404

    def test_join_team_already_member(self, auth_client, team):
        response = auth_client.post("/api/teams/join", json={"invite_code": team.invite_code})
        assert response.status_code == 400
        assert "Already a member" in response.json()["detail"]


class TestListMembers:
    def test_list_members(self, auth_client, team, test_user):
        response = auth_client.get(f"/api/teams/{team.id}/members")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["user_id"] == test_user.id
        assert data[0]["role"] == "owner"
        assert data[0]["user_email"] == test_user.email

    def test_list_members_not_member(self, auth_client, db):
        other_user = create_test_user(db, email="other2@example.com", password="pass123")
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other456")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        response = auth_client.get(f"/api/teams/{other_team.id}/members")
        assert response.status_code == 403


class TestRemoveMember:
    def test_remove_member_by_owner(self, client, db, team, test_user):
        joiner = create_test_user(db, email="joiner2@example.com", password="pass123")
        member = TeamMember(team_id=team.id, user_id=joiner.id, role="member")
        db.add(member)
        db.commit()

        token = login_user(client, test_user.email, "testpass123")
        response = client.delete(
            f"/api/teams/{team.id}/members/{joiner.id}",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 200
        assert response.json()["status"] == "removed"

    def test_admin_can_remove_member_but_not_owner(self, client, db, team, test_user):
        admin_user = create_test_user(db, email="admin@example.com", password="pass123")
        target_user = create_test_user(db, email="target@example.com", password="pass123")
        admin_member = TeamMember(team_id=team.id, user_id=admin_user.id, role="admin")
        target_member = TeamMember(team_id=team.id, user_id=target_user.id, role="member")
        db.add_all([admin_member, target_member])
        db.commit()

        token = login_user(client, admin_user.email, "pass123")
        response = client.delete(
            f"/api/teams/{team.id}/members/{target_user.id}",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 200
        assert response.json()["status"] == "removed"

        response = client.delete(
            f"/api/teams/{team.id}/members/{test_user.id}",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 403

    def test_remove_member_not_owner(self, client, db, team):
        joiner = create_test_user(db, email="joiner3@example.com", password="pass123")
        member = TeamMember(team_id=team.id, user_id=joiner.id, role="member")
        db.add(member)
        db.commit()

        token = login_user(client, joiner.email, "pass123")
        response = client.delete(
            f"/api/teams/{team.id}/members/{joiner.id}",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 403

    def test_remove_member_not_found(self, auth_client, team):
        response = auth_client.delete(f"/api/teams/{team.id}/members/nonexistent")
        assert response.status_code == 404


class TestMemberRoles:
    def test_owner_can_promote_member_to_admin(self, client, db, team, test_user):
        joiner = create_test_user(db, email="promote@example.com", password="pass123")
        member = TeamMember(team_id=team.id, user_id=joiner.id, role="member")
        db.add(member)
        db.commit()

        token = login_user(client, test_user.email, "testpass123")
        response = client.put(
            f"/api/teams/{team.id}/members/{joiner.id}/role",
            params={"role": "admin"},
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 200
        assert response.json()["role"] == "admin"


class TestDeleteTeam:
    def test_delete_team_by_owner(self, auth_client, team):
        response = auth_client.delete(f"/api/teams/{team.id}")
        assert response.status_code == 200
        assert response.json()["status"] == "deleted"

    def test_delete_team_not_owner(self, client, db, team, test_user):
        joiner = create_test_user(db, email="joiner4@example.com", password="pass123")
        member = TeamMember(team_id=team.id, user_id=joiner.id, role="member")
        db.add(member)
        db.commit()

        token = login_user(client, joiner.email, "pass123")
        response = client.delete(
            f"/api/teams/{team.id}",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 403

    def test_delete_team_not_found(self, auth_client):
        response = auth_client.delete("/api/teams/nonexistent")
        assert response.status_code == 404
