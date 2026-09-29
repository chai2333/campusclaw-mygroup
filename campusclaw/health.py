from __future__ import annotations

import os
import sqlite3
from pathlib import Path

from flask import Blueprint, current_app, jsonify

from .db import get_db

bp = Blueprint("health", __name__)


@bp.get("/health")
def health():
    try:
        get_db().execute("SELECT COUNT(*) FROM classes").fetchone()
        upload_dir = Path(current_app.config["UPLOAD_DIR"])
        upload_dir.mkdir(parents=True, exist_ok=True)
        if not os.access(upload_dir, os.R_OK | os.W_OK | os.X_OK):
            raise OSError("upload storage is unavailable")
    except (OSError, sqlite3.Error):
        current_app.logger.exception("health_check result=unhealthy")
        return jsonify({"status": "unhealthy"}), 503
    return jsonify({"status": "ok"}), 200
