"""Integration tests for home endpoints."""

import io
from datetime import datetime, timezone

from app.models import Todo, ProblemFile


class TestUploadProblem:
    def test_upload_pdf(self, auth_client, project, mocker):
        mocker.patch("PyPDF2.PdfReader", return_value=mocker.Mock(
            pages=[mocker.Mock(extract_text=lambda: "PDF content page 1")]
        ))
        file_content = b"%PDF-1.4 fake pdf content"
        response = auth_client.post(
            f"/api/home/{project.id}/upload",
            files={"file": ("test.pdf", io.BytesIO(file_content), "application/pdf")},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["filename"] == "test.pdf"
        assert data["file_type"] == "pdf"

    def test_upload_text(self, auth_client, project):
        file_content = b"This is a test problem file."
        response = auth_client.post(
            f"/api/home/{project.id}/upload",
            files={"file": ("test.txt", io.BytesIO(file_content), "text/plain")},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["filename"] == "test.txt"
        assert data["file_type"] == "text"
        assert "This is a test problem file." in data["extracted_text"]

    def test_upload_project_not_found(self, auth_client):
        file_content = b"test"
        response = auth_client.post(
            "/api/home/nonexistent/upload",
            files={"file": ("test.txt", io.BytesIO(file_content), "text/plain")},
        )
        assert response.status_code == 404

    def test_upload_not_member(self, auth_client, db, project, test_user):
        from app.models import Team, TeamMember
        other_user = __import__("tests.conftest", fromlist=["create_test_user"]).create_test_user(
            db, email="other@example.com", password="pass123"
        )
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other123")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        other_project = __import__("app.models", fromlist=["Project"]).Project(
            team_id=other_team.id, name="Other Project"
        )
        db.add(other_project)
        db.commit()
        db.refresh(other_project)

        file_content = b"test"
        response = auth_client.post(
            f"/api/home/{other_project.id}/upload",
            files={"file": ("test.txt", io.BytesIO(file_content), "text/plain")},
        )
        assert response.status_code == 403


class TestListProblems:
    def test_list_problems(self, auth_client, project, db):
        pf = ProblemFile(
            project_id=project.id,
            filename="problem.pdf",
            file_path="/tmp/problem.pdf",
            file_type="pdf",
        )
        db.add(pf)
        db.commit()

        response = auth_client.get(f"/api/home/{project.id}/problems")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["filename"] == "problem.pdf"

    def test_list_problems_empty(self, auth_client, project):
        response = auth_client.get(f"/api/home/{project.id}/problems")
        assert response.status_code == 200
        assert response.json() == []

    def test_list_problems_not_member(self, auth_client, db, project):
        from tests.conftest import create_test_user
        other_user = create_test_user(db, email="other5@example.com", password="pass123")
        from app.models import Team, TeamMember
        other_team = Team(name="Other Team", owner_id=other_user.id, invite_code="other5")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        other_project = __import__("app.models", fromlist=["Project"]).Project(
            team_id=other_team.id, name="Other Project"
        )
        db.add(other_project)
        db.commit()
        db.refresh(other_project)

        response = auth_client.get(f"/api/home/{other_project.id}/problems")
        assert response.status_code == 403


class TestDeleteProblem:
    def test_delete_problem_success(self, auth_client, project, db):
        pf = ProblemFile(
            project_id=project.id,
            filename="to-delete.pdf",
            file_path="uploads/test_delete.pdf",
            file_type="pdf",
        )
        db.add(pf)
        db.commit()
        db.refresh(pf)

        response = auth_client.delete(f"/api/home/{project.id}/problems/{pf.id}")
        assert response.status_code == 204

        remaining = db.query(ProblemFile).filter(ProblemFile.id == pf.id).first()
        assert remaining is None

    def test_delete_problem_not_found(self, auth_client, project):
        response = auth_client.delete(f"/api/home/{project.id}/problems/nonexistent")
        assert response.status_code == 404

    def test_delete_problem_not_member(self, auth_client, db, project):
        from tests.conftest import create_test_user
        other_user = create_test_user(db, email="other_del@example.com", password="pass123")
        from app.models import Team, TeamMember
        other_team = Team(name="Other Team Del", owner_id=other_user.id, invite_code="otherdel")
        db.add(other_team)
        db.commit()
        db.refresh(other_team)
        other_project = __import__("app.models", fromlist=["Project"]).Project(
            team_id=other_team.id, name="Other Project Del"
        )
        db.add(other_project)
        db.commit()
        db.refresh(other_project)

        pf = ProblemFile(
            project_id=other_project.id,
            filename="private.pdf",
            file_path="uploads/private.pdf",
            file_type="pdf",
        )
        db.add(pf)
        db.commit()
        db.refresh(pf)

        response = auth_client.delete(f"/api/home/{other_project.id}/problems/{pf.id}")
        assert response.status_code == 403

    def test_delete_problem_project_not_found(self, auth_client):
        response = auth_client.delete("/api/home/nonexistent/problems/someid")
        assert response.status_code == 404


class TestCreateTodo:
    def test_create_todo(self, auth_client, project):
        response = auth_client.post(
            f"/api/home/{project.id}/todos",
            params={"content": "Buy groceries", "is_team_todo": False},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["content"] == "Buy groceries"
        assert data["completed"] is False

    def test_create_todo_with_due_date(self, auth_client, project):
        due = datetime(2026, 5, 1, 12, 0, 0, tzinfo=timezone.utc)
        response = auth_client.post(
            f"/api/home/{project.id}/todos",
            params={"content": "Deadline task", "due_date": due.isoformat()},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["content"] == "Deadline task"

    def test_create_todo_project_not_found(self, auth_client):
        response = auth_client.post(
            "/api/home/nonexistent/todos",
            params={"content": "Task"},
        )
        assert response.status_code == 404


class TestListTodos:
    def test_list_todos(self, auth_client, project, db, test_user):
        todo = Todo(
            project_id=project.id,
            user_id=test_user.id,
            content="Task 1",
            is_team_todo=False,
        )
        db.add(todo)
        db.commit()

        response = auth_client.get(f"/api/home/{project.id}/todos")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["content"] == "Task 1"

    def test_list_todos_empty(self, auth_client, project):
        response = auth_client.get(f"/api/home/{project.id}/todos")
        assert response.status_code == 200
        assert response.json() == []


class TestToggleTodo:
    def test_toggle_todo(self, auth_client, project, db, test_user):
        todo = Todo(
            project_id=project.id,
            user_id=test_user.id,
            content="Task to toggle",
            completed=False,
        )
        db.add(todo)
        db.commit()
        db.refresh(todo)

        response = auth_client.put(f"/api/home/{project.id}/todos/{todo.id}")
        assert response.status_code == 200
        assert response.json()["completed"] is True

        response = auth_client.put(f"/api/home/{project.id}/todos/{todo.id}")
        assert response.status_code == 200
        assert response.json()["completed"] is False

    def test_toggle_todo_not_found(self, auth_client, project):
        response = auth_client.put(f"/api/home/{project.id}/todos/nonexistent")
        assert response.status_code == 404


class TestGetProgress:
    def test_get_progress(self, auth_client, project, db, test_user):
        todo1 = Todo(project_id=project.id, user_id=test_user.id, content="Done", completed=True)
        todo2 = Todo(project_id=project.id, user_id=test_user.id, content="Not done", completed=False)
        db.add_all([todo1, todo2])
        db.commit()

        response = auth_client.get(f"/api/home/{project.id}/progress")
        assert response.status_code == 200
        data = response.json()
        assert data["total_todos"] == 2
        assert data["completed_todos"] == 1
        assert data["completion_rate"] == 50.0

    def test_get_progress_empty(self, auth_client, project):
        response = auth_client.get(f"/api/home/{project.id}/progress")
        assert response.status_code == 200
        data = response.json()
        assert data["total_todos"] == 0
        assert data["completion_rate"] == 0
