"""Integration tests for auth endpoints."""

import pytest
from jose import jwt

from app.core.config import get_settings
from tests.conftest import create_test_user, login_user


settings = get_settings()


class TestRegister:
    def test_register_success(self, client):
        response = client.post(
            "/api/auth/register",
            json={
                "email": "newuser@example.com",
                "password": "password123",
                "display_name": "New User",
            },
        )
        assert response.status_code == 200
        data = response.json()
        assert "access_token" in data
        assert data["token_type"] == "bearer"

    def test_register_duplicate_email(self, client, test_user):
        response = client.post(
            "/api/auth/register",
            json={
                "email": test_user.email,
                "password": "password123",
                "display_name": "Another User",
            },
        )
        assert response.status_code == 400
        assert response.json()["detail"] == "Email already registered"

    def test_register_without_display_name(self, client):
        response = client.post(
            "/api/auth/register",
            json={
                "email": "nodisplay@example.com",
                "password": "password123",
            },
        )
        assert response.status_code == 200
        data = response.json()
        assert "access_token" in data

    def test_register_invalid_email(self, client):
        response = client.post(
            "/api/auth/register",
            json={
                "email": "not-an-email",
                "password": "password123",
            },
        )
        assert response.status_code == 422

    def test_register_missing_password(self, client):
        response = client.post(
            "/api/auth/register",
            json={"email": "missing@example.com"},
        )
        assert response.status_code == 422


class TestLogin:
    def test_login_success(self, client, test_user):
        response = client.post(
            "/api/auth/login",
            data={"username": test_user.email, "password": "testpass123"},
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )
        assert response.status_code == 200
        data = response.json()
        assert "access_token" in data
        assert data["token_type"] == "bearer"

        # Verify JWT payload
        payload = jwt.decode(data["access_token"], settings.SECRET_KEY, algorithms=[settings.ALGORITHM])
        assert payload["sub"] == test_user.id

    def test_login_wrong_password(self, client, test_user):
        response = client.post(
            "/api/auth/login",
            data={"username": test_user.email, "password": "wrongpassword"},
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )
        assert response.status_code == 400
        assert "Incorrect username/email or password" in response.json()["detail"]

    def test_login_nonexistent_user(self, client):
        response = client.post(
            "/api/auth/login",
            data={"username": "nobody@example.com", "password": "password123"},
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )
        assert response.status_code == 400
        assert "Incorrect username/email or password" in response.json()["detail"]

    def test_login_missing_fields(self, client):
        response = client.post(
            "/api/auth/login",
            data={},
            headers={"Content-Type": "application/x-www-form-urlencoded"},
        )
        assert response.status_code == 422


class TestMe:
    def test_me_success(self, auth_client, test_user):
        response = auth_client.get("/api/auth/me")
        assert response.status_code == 200
        data = response.json()
        assert data["id"] == test_user.id
        assert data["email"] == test_user.email
        assert data["display_name"] == test_user.display_name
        assert "created_at" in data

    def test_me_no_auth(self, client):
        response = client.get("/api/auth/me")
        assert response.status_code == 401

    def test_me_invalid_token(self, client):
        response = client.get("/api/auth/me", headers={"Authorization": "Bearer invalid_token"})
        assert response.status_code == 401


class TestNotionOAuth:
    def test_notion_url_requires_auth(self, client):
        response = client.get("/api/auth/notion/url")
        assert response.status_code == 401

    def test_notion_url_returns_url(self, auth_client):
        response = auth_client.get("/api/auth/notion/url")
        assert response.status_code == 200
        data = response.json()
        assert "auth_url" in data
        assert "api.notion.com" in data["auth_url"]

    def test_notion_callback_requires_auth(self, client):
        response = client.post("/api/auth/notion/callback", json={"code": "test_code"})
        assert response.status_code == 401

    def test_notion_callback_invalid_code(self, auth_client, mocker):
        mock_provider = mocker.MagicMock()
        mock_provider.exchange_auth_code = mocker.AsyncMock(side_effect=Exception("Invalid code"))
        mocker.patch("app.api.auth.get_provider", return_value=mock_provider)

        response = auth_client.post("/api/auth/notion/callback", json={"code": "invalid"})
        assert response.status_code == 400
        assert "Provider auth failed" in response.json()["detail"]

    def test_notion_callback_success(self, auth_client, test_user, db, mocker):
        mock_provider = mocker.MagicMock()
        mock_provider.exchange_auth_code = mocker.AsyncMock(
            return_value={"access_token": "token_123", "workspace_id": "ws_123", "workspace_name": "Test WS"}
        )
        mocker.patch("app.api.auth.get_provider", return_value=mock_provider)

        response = auth_client.post("/api/auth/notion/callback", json={"code": "valid_code"})
        assert response.status_code == 200
        assert response.json()["status"] == "success"
