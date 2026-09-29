from __future__ import annotations

import io

import pytest

from campusclaw import create_app
from campusclaw.db import get_db, seed_database


@pytest.fixture()
def app(tmp_path):
    app = create_app(
        {
            "TESTING": True,
            "APP_ENV": "testing",
            "SECRET_KEY": "unit-test-session-key",
            "SEED_TEACHER_PASSWORD": "teacher-test-password",
            "SEED_STUDENT_PASSWORD": "student-test-password",
            "DATABASE_PATH": str(tmp_path / "test.sqlite3"),
            "UPLOAD_DIR": str(tmp_path / "uploads"),
            "ALLOWED_EXTENSIONS": "txt,md",
            "MAX_UPLOAD_BYTES": 4096,
        }
    )
    with app.app_context():
        seed_database()
    yield app


@pytest.fixture()
def client(app):
    return app.test_client()


@pytest.fixture()
def runner(app):
    return app.test_cli_runner()


class AuthActions:
    def __init__(self, client):
        self.client = client

    def login(self, username="a-teacher", password="teacher-test-password"):
        return self.client.post(
            "/api/login",
            json={"username": username, "password": password},
            follow_redirects=False,
        )

    def logout(self):
        return self.client.post("/api/logout", follow_redirects=False)


@pytest.fixture()
def auth(client):
    return AuthActions(client)


def upload(client, filename="lesson.txt", content=b"Lesson content", **fields):
    data = {"file": (io.BytesIO(content), filename)}
    data.update(fields)
    return client.post("/api/materials", data=data, content_type="multipart/form-data")


@pytest.fixture()
def upload_file():
    return upload


@pytest.fixture()
def db(app):
    with app.app_context():
        yield get_db()
