"""Integration tests for git endpoints."""

import os
import tempfile

import pytest


class TestScanSolvers:
    def test_scan_solvers(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.makedirs(os.path.join(tmpdir, ".git"))
            os.makedirs(os.path.join(tmpdir, "src"))
            with open(os.path.join(tmpdir, "src", "solver.py"), "w") as f:
                f.write("x = 1")
            with open(os.path.join(tmpdir, "src", "main.cpp"), "w") as f:
                f.write("int main() {}")

            response = auth_client.get(
                f"/api/git/{project.id}/scan",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 200
            data = response.json()
            assert data["repo_path"] == tmpdir
            assert len(data["solvers"]) == 2
            names = [s["name"] for s in data["solvers"]]
            assert "solver.py" in names
            assert "main.cpp" in names

    def test_scan_not_git_repo(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            response = auth_client.get(
                f"/api/git/{project.id}/scan",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 400
            assert "Not a git repository" in response.json()["detail"]

    def test_scan_invalid_path(self, auth_client, project):
        response = auth_client.get(
            f"/api/git/{project.id}/scan",
            params={"repo_path": "/nonexistent/path"},
        )
        assert response.status_code == 400
        assert "Invalid repository path" in response.json()["detail"]


class TestExtractParams:
    def test_extract_params(self, auth_client, project):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".py", delete=False) as f:
            f.write("alpha = 0.5\nbeta = [1, 2, 3]\nuse_cache = True\n")
            tmpfile = f.name

        try:
            response = auth_client.get(
                f"/api/git/{project.id}/params",
                params={"solver_path": tmpfile},
            )
            assert response.status_code == 200
            data = response.json()
            assert data["solver_path"] == tmpfile
            names = [p["name"] for p in data["params"]]
            assert "alpha" in names
            assert "beta" in names
            assert "use_cache" in names
        finally:
            os.unlink(tmpfile)

    def test_extract_params_file_not_found(self, auth_client, project):
        response = auth_client.get(
            f"/api/git/{project.id}/params",
            params={"solver_path": "/nonexistent/solver.py"},
        )
        assert response.status_code == 404


class TestGitLog:
    def test_git_log(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            os.system(f'cd "{tmpdir}" && git init')
            os.system(f'cd "{tmpdir}" && git config user.email "test@test.com"')
            os.system(f'cd "{tmpdir}" && git config user.name "Test"')
            with open(os.path.join(tmpdir, "file.txt"), "w") as f:
                f.write("hello")
            os.system(f'cd "{tmpdir}" && git add . && git commit -m "first commit"')

            response = auth_client.get(
                f"/api/git/{project.id}/log",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 200
            data = response.json()
            assert data["repo_path"] == tmpdir
            assert len(data["commits"]) == 1
            assert "first commit" in data["commits"][0]

    def test_git_log_invalid_repo(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            response = auth_client.get(
                f"/api/git/{project.id}/log",
                params={"repo_path": tmpdir},
            )
            assert response.status_code in (400, 500)


class TestListExperiments:
    def test_list_experiments(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            exp_dir = os.path.join(tmpdir, "results", "20240115_143022_solver_v2")
            os.makedirs(os.path.join(exp_dir, "fig"))
            with open(os.path.join(exp_dir, "log.txt"), "w") as f:
                f.write("log content")
            with open(os.path.join(exp_dir, "analysis.md"), "w") as f:
                f.write("# Analysis")
            with open(os.path.join(exp_dir, "params_snapshot.json"), "w") as f:
                f.write('{"alpha": 0.5}')

            response = auth_client.get(
                f"/api/git/{project.id}/experiments",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 200
            data = response.json()
            assert len(data["experiments"]) == 1
            exp = data["experiments"][0]
            assert exp["solver_name"] == "solver_v2"
            assert exp["structure"]["is_complete"] is True

    def test_list_experiments_no_results_dir(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            response = auth_client.get(
                f"/api/git/{project.id}/experiments",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 200
            assert response.json()["experiments"] == []

    def test_list_experiments_marks_incomplete_structure(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            exp_dir = os.path.join(tmpdir, "results", "20240115_143022_solver_partial")
            os.makedirs(exp_dir)
            with open(os.path.join(exp_dir, "analysis.md"), "w") as f:
                f.write("# Partial")

            response = auth_client.get(
                f"/api/git/{project.id}/experiments",
                params={"repo_path": tmpdir},
            )
            assert response.status_code == 200
            exp = response.json()["experiments"][0]
            assert exp["structure"]["is_complete"] is False
            assert "fig/" in exp["structure"]["missing"]
            assert "log.txt" in exp["structure"]["missing"]
            assert "params_snapshot.json" in exp["structure"]["missing"]


class TestGetExperimentDetail:
    def test_get_experiment_detail(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            exp_dir = os.path.join(tmpdir, "20240115_143022_solver_v2")
            os.makedirs(os.path.join(exp_dir, "fig"))
            with open(os.path.join(exp_dir, "log.txt"), "w") as f:
                f.write("experiment log")
            with open(os.path.join(exp_dir, "analysis.md"), "w") as f:
                f.write("# Analysis\nGreat results")
            with open(os.path.join(exp_dir, "params_snapshot.json"), "w") as f:
                f.write('{"alpha": 0.5}')
            with open(os.path.join(exp_dir, "fig", "chart.png"), "wb") as f:
                f.write(b"\x89PNG")

            response = auth_client.get(
                f"/api/git/{project.id}/experiment",
                params={"experiment_dir": exp_dir},
            )
            assert response.status_code == 200
            data = response.json()
            assert data["log"] == "experiment log"
            assert "Great results" in data["analysis"]
            assert data["params_snapshot"] == {"alpha": 0.5}
            assert "chart.png" in data["fig_files"]

    def test_get_experiment_not_found(self, auth_client, project):
        response = auth_client.get(
            f"/api/git/{project.id}/experiment",
            params={"experiment_dir": "/nonexistent"},
        )
        assert response.status_code == 404

    def test_get_experiment_detail_tolerates_missing_optional_files(self, auth_client, project):
        with tempfile.TemporaryDirectory() as tmpdir:
            exp_dir = os.path.join(tmpdir, "20240115_143022_solver_partial")
            os.makedirs(exp_dir)

            response = auth_client.get(
                f"/api/git/{project.id}/experiment",
                params={"experiment_dir": exp_dir},
            )
            assert response.status_code == 200
            data = response.json()
            assert data["log"] == ""
            assert data["analysis"] == ""
            assert data["params_snapshot"] == {}
            assert data["fig_files"] == []
