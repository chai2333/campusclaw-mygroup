from __future__ import annotations

import sqlite3
from collections.abc import Iterable
from pathlib import Path

import click
from argon2 import PasswordHasher
from flask import Flask, current_app, g

password_hasher = PasswordHasher()


def get_db() -> sqlite3.Connection:
    if "db" not in g:
        database_path = Path(current_app.config["DATABASE_PATH"])
        database_path.parent.mkdir(parents=True, exist_ok=True)
        connection = sqlite3.connect(database_path)
        connection.row_factory = sqlite3.Row
        connection.execute("PRAGMA foreign_keys = ON")
        g.db = connection
    return g.db


def close_db(_error: BaseException | None = None) -> None:
    connection = g.pop("db", None)
    if connection is not None:
        connection.close()


def init_schema(connection: sqlite3.Connection | None = None) -> None:
    db = connection or get_db()
    schema_path = Path(__file__).with_name("schema.sql")
    db.executescript(schema_path.read_text(encoding="utf-8"))
    db.commit()


def _ensure_user(
    db: sqlite3.Connection,
    username: str,
    password: str,
    role: str,
    class_id: int,
) -> None:
    existing = db.execute("SELECT id FROM users WHERE username = ?", (username,)).fetchone()
    if existing is None:
        db.execute(
            "INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
            (username, password_hasher.hash(password), role, class_id),
        )


def seed_database(connection: sqlite3.Connection | None = None) -> None:
    db = connection or get_db()
    init_schema(db)
    db.executemany(
        "INSERT OR IGNORE INTO classes (slug, name) VALUES (?, ?)",
        (("class-a", "A 班"), ("class-b", "B 班")),
    )
    classes = {
        row["slug"]: row["id"]
        for row in db.execute("SELECT id, slug FROM classes WHERE slug IN ('class-a', 'class-b')")
    }
    teacher_password = current_app.config["SEED_TEACHER_PASSWORD"]
    student_password = current_app.config["SEED_STUDENT_PASSWORD"]
    for slug, prefix in (("class-a", "a"), ("class-b", "b")):
        _ensure_user(db, f"{prefix}-teacher", teacher_password, "teacher", classes[slug])
        _ensure_user(db, f"{prefix}-student", student_password, "student", classes[slug])
    db.commit()


def insert_knowledge_chunks(
    db: sqlite3.Connection,
    material_id: str,
    class_id: int,
    chunks: Iterable[str],
) -> None:
    db.executemany(
        """
        INSERT INTO knowledge_chunks (material_id, class_id, chunk_index, content)
        VALUES (?, ?, ?, ?)
        """,
        ((material_id, class_id, index, chunk) for index, chunk in enumerate(chunks)),
    )


@click.command("init-db")
def init_db_command() -> None:
    seed_database()
    click.echo("Initialized database and seed data.")


def init_app(app: Flask) -> None:
    app.teardown_appcontext(close_db)
    app.cli.add_command(init_db_command)
