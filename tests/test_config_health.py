from __future__ import annotations

from pathlib import Path

import pytest

from campusclaw import create_app
from campusclaw.config import ConfigurationError, load_config


def test_missing_server_secret_fails_configuration():
    with pytest.raises(ConfigurationError, match="SECRET_KEY"):
        load_config(
            {
                "SECRET_KEY": None,
                "SEED_TEACHER_PASSWORD": "teacher",
                "SEED_STUDENT_PASSWORD": "student",
            }
        )


def test_invalid_upload_configuration_is_rejected():
    with pytest.raises(ConfigurationError, match="ALLOWED_EXTENSIONS"):
        load_config(
            {
                "SECRET_KEY": "test",
                "SEED_TEACHER_PASSWORD": "teacher",
                "SEED_STUDENT_PASSWORD": "student",
                "ALLOWED_EXTENSIONS": "txt,exe",
            }
        )


def test_health_is_public_and_minimal(client):
    response = client.get("/health")
    assert response.status_code == 200
    assert response.get_json() == {"status": "ok"}
    assert b"sqlite" not in response.data.lower()
    assert b"secret" not in response.data.lower()


def test_health_reports_storage_failure_without_details(app, client, tmp_path):
    blocked_path = tmp_path / "not-a-directory"
    blocked_path.write_text("file", encoding="utf-8")
    app.config["UPLOAD_DIR"] = str(blocked_path)
    response = client.get("/health")
    assert response.status_code == 503
    assert response.get_json() == {"status": "unhealthy"}
    assert str(blocked_path).encode() not in response.data


def test_health_reports_database_failure_without_details(app, client, tmp_path):
    database_directory = tmp_path / "database-directory"
    database_directory.mkdir()
    app.config["DATABASE_PATH"] = str(database_directory)
    response = client.get("/health")
    assert response.status_code == 503
    assert response.get_json() == {"status": "unhealthy"}
    assert str(database_directory).encode() not in response.data


def test_env_example_contains_names_but_no_values():
    lines = Path(".env.example").read_text(encoding="utf-8").splitlines()
    required = {"SECRET_KEY", "SEED_TEACHER_PASSWORD", "SEED_STUDENT_PASSWORD"}
    parsed = dict(line.split("=", 1) for line in lines if line and not line.startswith("#"))
    assert required.issubset(parsed)
    assert all(parsed[name] == "" for name in required)


def test_container_files_keep_secrets_out_of_image():
    dockerfile = Path("Dockerfile").read_text(encoding="utf-8")
    compose = Path("compose.yaml").read_text(encoding="utf-8")
    dockerignore = Path(".dockerignore").read_text(encoding="utf-8").splitlines()
    assert "USER app" in dockerfile
    assert "container_entrypoint.py" in dockerfile
    assert "${SECRET_KEY:?SECRET_KEY is required}" in compose
    assert "GET /health" not in compose  # healthcheck uses the concrete URL, not leaked config
    assert ".env" in dockerignore
    assert "openspec" in dockerignore


def test_non_development_cookie_is_secure(tmp_path):
    app = create_app(
        {
            "TESTING": False,
            "APP_ENV": "production",
            "SECRET_KEY": "test-key",
            "SEED_TEACHER_PASSWORD": "teacher",
            "SEED_STUDENT_PASSWORD": "student",
            "DATABASE_PATH": str(tmp_path / "db.sqlite3"),
            "UPLOAD_DIR": str(tmp_path / "uploads"),
        }
    )
    assert app.config["SESSION_COOKIE_SECURE"] is True
    assert app.config["SESSION_COOKIE_HTTPONLY"] is True
    assert app.config["SESSION_COOKIE_SAMESITE"] == "Lax"
