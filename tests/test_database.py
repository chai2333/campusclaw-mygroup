from __future__ import annotations

import sqlite3

import pytest
from argon2 import PasswordHasher

from campusclaw.db import password_hasher, seed_database


def test_schema_constraints_and_indexes(db):
    columns = {row["name"] for row in db.execute("PRAGMA table_info(users)")}
    assert "password_hash" in columns
    assert "password" not in columns

    indexes = {row["name"] for row in db.execute("PRAGMA index_list(materials)")}
    assert "idx_materials_class_status" in indexes

    with pytest.raises(sqlite3.IntegrityError):
        db.execute(
            "INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
            ("invalid", "hash", "admin", 1),
        )


def test_argon2_uses_unique_salts_and_verifies():
    first = password_hasher.hash("same-password")
    second = password_hasher.hash("same-password")
    assert first != second
    assert first.startswith("$argon2id$")
    assert password_hasher.verify(first, "same-password") is True


def test_seed_is_idempotent_and_keeps_hashes(app):
    with app.app_context():
        from campusclaw.db import get_db

        db = get_db()
        before = {
            row["username"]: row["password_hash"]
            for row in db.execute("SELECT username, password_hash FROM users")
        }
        seed_database()
        after = {
            row["username"]: row["password_hash"]
            for row in db.execute("SELECT username, password_hash FROM users")
        }
        assert before == after
        assert set(after) == {"a-teacher", "a-student", "b-teacher", "b-student"}
        assert db.execute("SELECT COUNT(*) FROM classes").fetchone()[0] == 2


def test_seeded_passwords_are_hashes(db):
    rows = db.execute("SELECT username, password_hash, role, class_id FROM users").fetchall()
    hasher = PasswordHasher()
    assert len(rows) == 4
    for row in rows:
        assert row["password_hash"].startswith("$argon2id$")
        password = "teacher-test-password" if row["role"] == "teacher" else "student-test-password"
        assert hasher.verify(row["password_hash"], password)
        assert row["class_id"] is not None


def test_database_file_does_not_contain_seed_plaintext(app):
    database_bytes = __import__("pathlib").Path(app.config["DATABASE_PATH"]).read_bytes()
    assert b"teacher-test-password" not in database_bytes
    assert b"student-test-password" not in database_bytes
