import pytest
from app.models import Citation, ZoteroConfig


def test_create_citation(db, project, test_user):
    citation = Citation(
        project_id=project.id,
        user_id=test_user.id,
        title="Test Paper",
        authors='["Zhang, S."]',
        year=2024,
        source="manual",
    )
    db.add(citation)
    db.commit()
    db.refresh(citation)
    assert citation.id is not None
    assert citation.title == "Test Paper"


def test_create_zotero_config(db, project):
    config = ZoteroConfig(
        project_id=project.id,
        api_key="test_key",
        library_id="12345",
        library_type="user",
    )
    db.add(config)
    db.commit()
    db.refresh(config)
    assert config.project_id == project.id
    assert config.last_sync_status == "idle"
