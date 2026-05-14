"""Integration tests for home endpoints."""

import io
import os
from datetime import datetime, timezone
from tempfile import TemporaryDirectory

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
        assert len(data) == 1
        assert data[0]["filename"] == "test.pdf"
        assert data[0]["file_type"] == "pdf"

    def test_upload_text(self, auth_client, project):
        file_content = b"This is a test problem file."
        response = auth_client.post(
            f"/api/home/{project.id}/upload",
            files={"file": ("test.txt", io.BytesIO(file_content), "text/plain")},
        )
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["filename"] == "test.txt"
        assert data[0]["file_type"] == "text"
        assert "This is a test problem file." in data[0]["extracted_text"]

    def test_upload_multiple_files(self, auth_client, project, mocker):
        mocker.patch("PyPDF2.PdfReader", return_value=mocker.Mock(
            pages=[mocker.Mock(extract_text=lambda: "PDF content page 1")]
        ))
        response = auth_client.post(
            f"/api/home/{project.id}/upload",
            files=[
                ("files", ("test.pdf", io.BytesIO(b"%PDF-1.4 fake pdf content"), "application/pdf")),
                ("files", ("test.txt", io.BytesIO(b"hello from txt"), "text/plain")),
            ],
        )
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 2
        assert {item["filename"] for item in data} == {"test.pdf", "test.txt"}

    def test_upload_requires_at_least_one_file(self, auth_client, project):
        response = auth_client.post(f"/api/home/{project.id}/upload")
        assert response.status_code == 400
        assert response.json()["detail"] == "No files provided"

    def test_upload_rejects_unsupported_extension(self, auth_client, project):
        response = auth_client.post(
            f"/api/home/{project.id}/upload",
            files={"file": ("test.csv", io.BytesIO(b"a,b\n1,2"), "text/csv")},
        )
        assert response.status_code == 400
        assert "Only PDF and TXT files are supported" in response.json()["detail"]

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


class TestDownloadProblem:
    def test_download_problem_success(self, auth_client, project, db):
        with TemporaryDirectory() as tmpdir:
            file_path = os.path.join(tmpdir, "problem.pdf")
            with open(file_path, "wb") as handle:
                handle.write(b"%PDF-1.4 fake pdf content")

            pf = ProblemFile(
                project_id=project.id,
                filename="problem.pdf",
                file_path=file_path,
                file_type="pdf",
            )
            db.add(pf)
            db.commit()
            db.refresh(pf)

            response = auth_client.get(f"/api/home/{project.id}/problems/{pf.id}/download")
            assert response.status_code == 200
            assert response.content.startswith(b"%PDF-1.4")

    def test_download_problem_missing_on_disk(self, auth_client, project, db):
        pf = ProblemFile(
            project_id=project.id,
            filename="missing.pdf",
            file_path="/nonexistent/problem.pdf",
            file_type="pdf",
        )
        db.add(pf)
        db.commit()
        db.refresh(pf)

        response = auth_client.get(f"/api/home/{project.id}/problems/{pf.id}/download")
        assert response.status_code == 404
        assert response.json()["detail"] == "File not found on disk"

    def test_download_after_delete_returns_not_found(self, auth_client, project, db):
        with TemporaryDirectory() as tmpdir:
            file_path = os.path.join(tmpdir, "problem.txt")
            with open(file_path, "w", encoding="utf-8") as handle:
                handle.write("hello")

            pf = ProblemFile(
                project_id=project.id,
                filename="problem.txt",
                file_path=file_path,
                file_type="text",
            )
            db.add(pf)
            db.commit()
            db.refresh(pf)

            delete_response = auth_client.delete(f"/api/home/{project.id}/problems/{pf.id}")
            assert delete_response.status_code == 204

            response = auth_client.get(f"/api/home/{project.id}/problems/{pf.id}/download")
            assert response.status_code == 404
            assert response.json()["detail"] == "Problem file not found"


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
