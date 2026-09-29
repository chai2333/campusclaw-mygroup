from __future__ import annotations

import re
import sqlite3

import pytest

from campusclaw import create_app
from campusclaw.db import get_db, init_schema, password_hasher


def token(client):
    return client.get_cookie("session").value


def test_teacher_and_student_can_login(auth, client):
    for username, password, role, class_id in (
        ("a-teacher", "teacher-test-password", "teacher", 1),
        ("b-student", "student-test-password", "student", 2),
    ):
        assert auth.login(username, password).status_code == 200
        identity = client.get("/api/me")
        assert identity.status_code == 200
        assert identity.json == {
            "id": identity.json["id"],
            "username": username,
            "role": role,
            "class_id": class_id,
        }
        auth.logout()


@pytest.mark.parametrize(
    "payload",
    [
        {},
        [],
        None,
        {"username": 1, "password": []},
        {"username": "a-teacher"},
        {"username": "", "password": "x"},
    ],
)
def test_invalid_json_fields(client, app, payload):
    response = client.post("/api/login", json=payload)
    assert response.status_code == 400
    with app.app_context():
        assert get_db().execute("SELECT count(*) FROM sessions").fetchone()[0] == 0


def test_invalid_credentials_and_non_json(client, app, caplog):
    for username in ("a-teacher", "missing"):
        response = client.post("/api/login", json={"username": username, "password": "wrong"})
        assert response.status_code == 401
        assert "wrong" not in response.text
    assert client.post("/api/login", data={"username": "a-teacher"}).status_code == 400
    assert client.post("/api/login", data="{", content_type="application/json").status_code == 400
    assert client.post("/login", data={}).status_code == 405
    assert "wrong" not in caplog.text
    with app.app_context():
        assert get_db().execute("SELECT count(*) FROM sessions").fetchone()[0] == 0


def test_rotation_logout_replay_and_expiry(auth, client, app, monkeypatch):
    auth.login()
    old = token(client)
    auth.login()
    new = token(client)
    assert old != new
    client.set_cookie("session", old)
    assert client.get("/api/me").status_code == 401
    client.set_cookie("session", new)
    assert client.get("/api/me").status_code == 200
    response = auth.logout()
    assert response.status_code == 200
    assert "Max-Age=0" in response.headers["Set-Cookie"]
    client.set_cookie("session", new)
    assert client.get("/api/me").status_code == 401
    assert client.get("/api/materials").status_code == 401
    auth.login()
    expiring = token(client)
    with app.app_context():
        expiry = (
            get_db()
            .execute("SELECT expires_at FROM sessions WHERE id=?", (expiring,))
            .fetchone()[0]
        )
    monkeypatch.setattr("campusclaw.auth.time.time", lambda: expiry)
    assert client.get("/api/me").status_code == 401
    assert client.get("/api/materials").status_code == 401
    auth.login()
    with app.app_context():
        assert (
            get_db().execute("SELECT id FROM sessions WHERE id=?", (expiring,)).fetchone() is None
        )


def test_cookie_and_session_data(auth, client, app, caplog):
    ids = set()
    for _ in range(5):
        response = auth.login()
        cookie = response.headers["Set-Cookie"]
        assert "HttpOnly" in cookie and "SameSite=Lax" in cookie
        assert re.fullmatch(r"[A-Za-z0-9_-]{43}", token(client))
        ids.add(token(client))
        for secret in ("teacher-test-password", "unit-test-session-key", "class_id", "user_id"):
            assert secret not in cookie + response.text + caplog.text
    assert len(ids) == 5  # CSPRNG is secrets.token_urlsafe(32); sample checks catch reuse.
    with app.app_context():
        row = get_db().execute("SELECT * FROM sessions").fetchone()
        assert set(row.keys()) == {"id", "user_id", "created_at", "expires_at"}
        assert row["expires_at"] - row["created_at"] == 28800
    assert all(value not in caplog.text for value in ids)
    app.config["SESSION_COOKIE_SECURE"] = True
    assert "Secure" in auth.login().headers["Set-Cookie"]


def test_database_role_class_changes_and_spoofing(auth, client, app, upload_file):
    auth.login()
    with app.app_context():
        db = get_db()
        db.execute("UPDATE users SET role='student', class_id=2 WHERE username='a-teacher'")
        db.commit()
    client.set_cookie("role", "teacher")
    client.set_cookie("class_id", "1")
    identity = client.get("/api/me?role=teacher&class_id=1").json
    assert identity["role"] == "student" and identity["class_id"] == 2
    assert upload_file(client, role="teacher", class_id="1").status_code == 403
    client.set_cookie("session", '{"user_id":1,"role":"teacher","class_id":1}')
    assert client.get("/api/me").status_code == 401


def test_protected_page_and_public_health(client):
    assert "/login" in client.get("/materials").headers["Location"]
    for path in ("/api/me", "/api/materials"):
        response = client.get(path)
        assert response.status_code == 401
        assert response.json == {"error": "authentication_required"}
    client.set_cookie("session", "invalid")
    assert client.get("/health").status_code == 200


def test_missing_class_membership_is_rejected(app, client):
    with app.app_context():
        db = get_db()
        db.execute(
            "INSERT INTO users (username, password_hash, role) VALUES (?, ?, 'teacher')",
            ("no-class", password_hasher.hash("no-class-password")),
        )
        db.commit()
    client.post("/api/login", json={"username": "no-class", "password": "no-class-password"})
    assert client.get("/api/me").status_code == 200
    assert client.get("/api/materials").status_code == 403


def test_session_migration_preserves_business_data(app, auth, client, upload_file):
    auth.login()
    material = upload_file(client).json
    with app.app_context():
        db = get_db()
        users = [tuple(row) for row in db.execute("SELECT * FROM users")]
        db.execute("DROP TABLE sessions")  # Simulate pre-migration database in isolated fixture.
        init_schema(db)
        init_schema(db)
        assert [tuple(row) for row in db.execute("SELECT * FROM users")] == users
        assert db.execute("SELECT id FROM materials").fetchone()[0] == material["id"]
        assert "idx_sessions_expires" in {
            row["name"] for row in db.execute("PRAGMA index_list(sessions)")
        }
        with pytest.raises(sqlite3.IntegrityError):
            db.execute("INSERT INTO sessions VALUES (?, ?, ?, ?)", ("bad", 999, 1, 2))
        db.rollback()
    assert client.get("/api/me").status_code == 401
    auth.login()
    other_app = create_app({**app.config, "ALLOWED_EXTENSIONS": "txt,md"})
    other_client = other_app.test_client()
    other_client.set_cookie("session", token(client))
    assert other_client.get("/api/me").status_code == 200
    assert other_client.get("/api/materials").json["materials"] == [material]
    auth.logout()
    assert other_client.get("/api/me").status_code == 401


def test_same_class_student_sees_teacher_upload(auth, client, upload_file):
    auth.login()
    material = upload_file(client).json
    auth.login("a-student", "student-test-password")
    assert client.get("/api/materials").json["materials"] == [material]
    assert upload_file(client).status_code == 403
