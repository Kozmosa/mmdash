def test_status_requires_auth(client):
    response = client.get("/api/im/status")
    assert response.status_code == 401


def test_status_returns_providers(auth_client):
    response = auth_client.get("/api/im/status")
    assert response.status_code == 200
    data = response.json()
    assert "providers" in data
    assert isinstance(data["providers"], list)


def test_user_binding_crud(auth_client, test_user):
    # Create
    response = auth_client.post(
        "/api/im/user-binding",
        json={"provider_type": "feishu_cli", "im_user_id": "u_test123", "enabled": True},
    )
    assert response.status_code == 200

    # Read
    response = auth_client.get("/api/im/user-binding")
    assert response.status_code == 200
    data = response.json()
    assert data["binding"]["im_user_id"] == "u_test123"
    assert data["binding"]["enabled"] is True

    # Update
    response = auth_client.post(
        "/api/im/user-binding",
        json={"provider_type": "feishu_cli", "im_user_id": "u_updated", "enabled": False},
    )
    assert response.status_code == 200
    data = auth_client.get("/api/im/user-binding").json()
    assert data["binding"]["im_user_id"] == "u_updated"
    assert data["binding"]["enabled"] is False


def test_user_binding_starts_null(auth_client, test_user):
    """User without a binding gets null."""
    response = auth_client.get("/api/im/user-binding")
    assert response.status_code == 200
    assert response.json()["binding"] is None


def test_project_binding_crud(auth_client, test_user, team, project):
    response = auth_client.post(
        f"/api/im/project-binding/{project.id}",
        json={"provider_type": "feishu_cli", "im_chat_id": "oc_test456", "enabled": True},
    )
    assert response.status_code == 200

    response = auth_client.get(f"/api/im/project-binding/{project.id}")
    assert response.status_code == 200
    data = response.json()
    assert data["binding"]["im_chat_id"] == "oc_test456"

    # Update
    response = auth_client.post(
        f"/api/im/project-binding/{project.id}",
        json={"provider_type": "feishu_cli", "im_chat_id": "oc_updated", "enabled": True},
    )
    assert response.status_code == 200
    data = auth_client.get(f"/api/im/project-binding/{project.id}").json()
    assert data["binding"]["im_chat_id"] == "oc_updated"


def test_project_binding_nonexistent_project(auth_client):
    response = auth_client.get("/api/im/project-binding/nonexistent")
    assert response.status_code == 404
