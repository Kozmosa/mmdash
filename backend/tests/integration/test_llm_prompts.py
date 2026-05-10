"""Integration tests for team LLM prompt settings."""

from tests.conftest import create_test_user, login_user
from app.models import TeamMember


class TestLLMPromptSettings:
    def test_owner_can_get_and_update_prompts(self, auth_client, team):
        response = auth_client.get(f"/api/llm/prompts", params={"team_id": team.id})
        assert response.status_code == 200
        data = response.json()
        assert data["team_id"] == team.id
        assert "symbols" in data["prompts"]

        updated = {
            "symbols": "custom symbols prompt {content}",
            "structure": "custom structure prompt {content}",
            "formula": "custom formula prompt {formula} {context}",
            "errors": "custom errors prompt {content}",
        }
        response = auth_client.put("/api/llm/prompts", json={"team_id": team.id, "prompts": updated})
        assert response.status_code == 200
        data = response.json()
        assert data["prompts"]["symbols"] == "custom symbols prompt {content}"

    def test_member_cannot_view_prompts(self, client, db, team):
        member = create_test_user(db, email="member@example.com", password="pass123")
        db.add(TeamMember(team_id=team.id, user_id=member.id, role="member"))
        db.commit()

        token = login_user(client, member.email, "pass123")
        response = client.get(
            "/api/llm/prompts",
            params={"team_id": team.id},
            headers={"Authorization": f"Bearer {token}"},
        )
        assert response.status_code == 403
