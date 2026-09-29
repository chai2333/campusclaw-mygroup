from __future__ import annotations

import mimetypes
import uuid
from pathlib import Path
from typing import Any

from flask import Blueprint, current_app, g, jsonify, render_template, request
from werkzeug.datastructures import FileStorage

from .auth import login_required, teacher_required
from .db import get_db, insert_knowledge_chunks

bp = Blueprint("materials", __name__)


class UploadValidationError(ValueError):
    pass


def _json_material(row: Any) -> dict[str, Any]:
    return {
        "id": row["id"],
        "original_filename": row["original_filename"],
        "uploader": row["uploader"],
        "created_at": row["created_at"],
    }


def list_ready_materials(class_id: int):
    return (
        get_db()
        .execute(
            """
        SELECT m.id, m.original_filename, m.created_at, u.username AS uploader
        FROM materials AS m
        JOIN users AS u ON u.id = m.uploader_id
        WHERE m.class_id = ? AND m.status = 'ready'
        ORDER BY m.created_at DESC, m.id DESC
        """,
            (class_id,),
        )
        .fetchall()
    )


def _extension(filename: str) -> str:
    return Path(filename).suffix.lower().lstrip(".")


def validate_upload(file: FileStorage) -> tuple[bytes, str]:
    if not file or not file.filename:
        raise UploadValidationError("A file is required.")
    extension = _extension(file.filename)
    if extension not in current_app.config["ALLOWED_EXTENSIONS"]:
        raise UploadValidationError("Unsupported file type.")

    declared_type = (file.mimetype or "").lower()
    allowed_mime = {
        "txt": {"text/plain", "application/octet-stream"},
        "md": {"text/markdown", "text/plain", "application/octet-stream"},
    }
    if declared_type not in allowed_mime[extension]:
        raise UploadValidationError("File content type does not match its extension.")

    data = file.read(current_app.config["MAX_UPLOAD_BYTES"] + 1)
    if len(data) > current_app.config["MAX_UPLOAD_BYTES"]:
        raise UploadValidationError("File exceeds the configured size limit.")
    if not data or b"\x00" in data:
        raise UploadValidationError("File content is empty or not plain text.")
    try:
        data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise UploadValidationError("File content must be UTF-8 text.") from exc

    guessed_type, _encoding = mimetypes.guess_type(file.filename)
    if guessed_type and guessed_type not in allowed_mime[extension]:
        raise UploadValidationError("File signature and extension are inconsistent.")
    return data, extension


def parse_text(data: bytes, chunk_size: int = 1000) -> list[str]:
    text = data.decode("utf-8").strip()
    if not text:
        raise UploadValidationError("File has no usable text.")
    paragraphs = [part.strip() for part in text.replace("\r\n", "\n").split("\n\n") if part.strip()]
    chunks: list[str] = []
    for paragraph in paragraphs:
        for start in range(0, len(paragraph), chunk_size):
            chunks.append(paragraph[start : start + chunk_size])
    return chunks


@bp.get("/api/materials")
@login_required()
def materials_api():
    requested_class = request.args.get("class_id", type=int)
    if requested_class is not None and requested_class != g.user["class_id"]:
        return jsonify({"error": "cross_class_access_denied"}), 403
    rows = list_ready_materials(g.user["class_id"])
    return jsonify({"materials": [_json_material(row) for row in rows]})


@bp.get("/api/materials/<material_id>")
@login_required()
def material_api(material_id: str):
    row = (
        get_db()
        .execute(
            """
        SELECT m.id, m.original_filename, m.created_at, u.username AS uploader
        FROM materials AS m
        JOIN users AS u ON u.id = m.uploader_id
        WHERE m.id = ? AND m.class_id = ? AND m.status = 'ready'
        """,
            (material_id, g.user["class_id"]),
        )
        .fetchone()
    )
    if row is None:
        return jsonify({"error": "material_not_found"}), 404
    return jsonify(_json_material(row))


@bp.post("/api/materials")
@teacher_required
def upload_material():
    requested_class = request.form.get("class_id", type=int)
    if requested_class is not None and requested_class != g.user["class_id"]:
        return jsonify({"error": "cross_class_access_denied"}), 403
    file = request.files.get("file")
    try:
        data, extension = validate_upload(file)
    except UploadValidationError as exc:
        return jsonify({"error": "invalid_upload", "message": str(exc)}), 400

    material_id = str(uuid.uuid4())
    storage_key = f"{uuid.uuid4().hex}.{extension}"
    stored_path = Path(current_app.config["UPLOAD_DIR"]) / storage_key
    db = get_db()
    try:
        stored_path.write_bytes(data)
        db.execute(
            """
            INSERT INTO materials
                (id, class_id, uploader_id, original_filename, storage_key, status)
            VALUES (?, ?, ?, ?, ?, 'processing')
            """,
            (material_id, g.user["class_id"], g.user["id"], file.filename, storage_key),
        )
        db.commit()
    except Exception:
        db.rollback()
        stored_path.unlink(missing_ok=True)
        current_app.logger.exception(
            "material_upload user_id=%s class_id=%s result=storage_failed",
            g.user["id"],
            g.user["class_id"],
        )
        return jsonify({"error": "material_storage_failed"}), 500

    try:
        chunks = parse_text(data)
        db.execute("BEGIN")
        insert_knowledge_chunks(db, material_id, g.user["class_id"], chunks)
        db.execute(
            "UPDATE materials SET status = 'ready', error = NULL WHERE id = ?", (material_id,)
        )
        db.commit()
    except Exception:
        db.rollback()
        db.execute("DELETE FROM knowledge_chunks WHERE material_id = ?", (material_id,))
        db.execute(
            "UPDATE materials SET status = 'failed', error = ? WHERE id = ?",
            ("material_processing_failed", material_id),
        )
        db.commit()
        stored_path.unlink(missing_ok=True)
        current_app.logger.exception(
            "material_upload user_id=%s class_id=%s result=failed",
            g.user["id"],
            g.user["class_id"],
        )
        return jsonify({"error": "material_processing_failed"}), 422

    current_app.logger.info(
        "material_upload user_id=%s class_id=%s material_id=%s result=ready",
        g.user["id"],
        g.user["class_id"],
        material_id,
    )
    row = db.execute(
        """
        SELECT m.id, m.original_filename, m.created_at, u.username AS uploader
        FROM materials AS m JOIN users AS u ON u.id = m.uploader_id
        WHERE m.id = ? AND m.class_id = ?
        """,
        (material_id, g.user["class_id"]),
    ).fetchone()
    return jsonify(_json_material(row)), 201


@bp.get("/materials")
@login_required()
def materials_page():
    return render_template(
        "materials.html",
        materials=list_ready_materials(g.user["class_id"]),
    )
