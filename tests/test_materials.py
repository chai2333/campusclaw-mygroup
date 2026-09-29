from __future__ import annotations

import io

from campusclaw.db import get_db


def _login(client, username, password):
    return client.post("/api/login", json={"username": username, "password": password})


def test_student_upload_is_403_without_side_effects(app, client, upload_file):
    _login(client, "a-student", "student-test-password")
    response = upload_file(client)
    assert response.status_code == 403
    with app.app_context():
        db = get_db()
        assert db.execute("SELECT COUNT(*) FROM materials").fetchone()[0] == 0
        assert db.execute("SELECT COUNT(*) FROM knowledge_chunks").fetchone()[0] == 0
    assert list((__import__("pathlib").Path(app.config["UPLOAD_DIR"])).iterdir()) == []


def test_role_spoof_does_not_allow_student_upload(client, upload_file):
    _login(client, "a-student", "student-test-password")
    response = upload_file(client, role="teacher")
    assert response.status_code == 403


def test_teacher_upload_is_ready_and_visible_to_same_class(app, client, upload_file):
    _login(client, "a-teacher", "teacher-test-password")
    response = upload_file(client, filename="lesson.md", content=b"# Lesson\n\nClass A content")
    assert response.status_code == 201
    material = response.get_json()
    listing = client.get("/api/materials")
    assert listing.status_code == 200
    assert listing.get_json()["materials"] == [material]
    with app.app_context():
        db = get_db()
        row = db.execute("SELECT class_id, status, storage_key FROM materials").fetchone()
        assert row["class_id"] == 1
        assert row["status"] == "ready"
        assert row["storage_key"] != "lesson.md"
        assert db.execute("SELECT COUNT(*) FROM knowledge_chunks").fetchone()[0] == 2


def test_other_class_cannot_list_or_read_material(client, upload_file):
    _login(client, "a-teacher", "teacher-test-password")
    material_id = upload_file(client).get_json()["id"]
    client.post("/api/logout")
    _login(client, "b-student", "student-test-password")
    listing = client.get("/api/materials")
    assert listing.get_json() == {"materials": []}
    detail = client.get(f"/api/materials/{material_id}")
    assert detail.status_code == 404
    assert b"lesson.txt" not in detail.data


def test_explicit_cross_class_parameters_are_403(client, upload_file):
    _login(client, "a-teacher", "teacher-test-password")
    assert client.get("/api/materials?class_id=2").status_code == 403
    before = client.get("/api/materials").get_json()
    response = upload_file(client, class_id="2")
    assert response.status_code == 403
    assert client.get("/api/materials").get_json() == before


def test_invalid_files_are_rejected_before_records_exist(app, client, upload_file):
    _login(client, "a-teacher", "teacher-test-password")
    assert upload_file(client, filename="malware.exe", content=b"hello").status_code == 400
    assert upload_file(client, filename="binary.txt", content=b"a\x00b").status_code == 400
    assert upload_file(client, filename="large.txt", content=b"x" * 4097).status_code == 400
    with app.app_context():
        assert get_db().execute("SELECT COUNT(*) FROM materials").fetchone()[0] == 0


def test_processing_failure_marks_failed_and_cleans_file(app, client, monkeypatch, upload_file):
    _login(client, "a-teacher", "teacher-test-password")

    def fail_insert(*_args, **_kwargs):
        raise RuntimeError("simulated database failure with no secrets")

    monkeypatch.setattr("campusclaw.materials.insert_knowledge_chunks", fail_insert)
    response = upload_file(client)
    assert response.status_code == 422
    with app.app_context():
        db = get_db()
        row = db.execute("SELECT status, error FROM materials").fetchone()
        assert dict(row) == {"status": "failed", "error": "material_processing_failed"}
        assert db.execute("SELECT COUNT(*) FROM knowledge_chunks").fetchone()[0] == 0
    from pathlib import Path

    assert list(Path(app.config["UPLOAD_DIR"]).iterdir()) == []


def test_materials_page_hides_upload_from_students(client, upload_file):
    _login(client, "a-student", "student-test-password")
    student_page = client.get("/materials")
    assert b'id="teacher-upload"' not in student_page.data
    client.post("/api/logout")
    _login(client, "a-teacher", "teacher-test-password")
    teacher_page = client.get("/materials")
    assert b'id="teacher-upload"' in teacher_page.data


def test_failed_material_is_not_listed(app, client):
    _login(client, "a-teacher", "teacher-test-password")
    with app.app_context():
        db = get_db()
        db.execute(
            """
            INSERT INTO materials
                (id, class_id, uploader_id, original_filename, storage_key, status, error)
            VALUES ('failed-id', 1, 1, 'failed.txt', 'failed-key.txt', 'failed', 'parse_failed')
            """
        )
        db.commit()
    response = client.get("/api/materials")
    assert response.get_json() == {"materials": []}


def test_upload_requires_multipart_file(client):
    _login(client, "a-teacher", "teacher-test-password")
    response = client.post("/api/materials", data={"file": (io.BytesIO(b""), "")})
    assert response.status_code == 400
