from __future__ import annotations

import os
from collections.abc import Mapping
from pathlib import Path
from typing import Any


class ConfigurationError(RuntimeError):
    """Raised when required server-only configuration is missing or invalid."""


def load_config(overrides: Mapping[str, Any] | None = None) -> dict[str, Any]:
    values: dict[str, Any] = {
        "APP_ENV": os.getenv("APP_ENV", "production"),
        "SECRET_KEY": os.getenv("SECRET_KEY"),
        "SEED_TEACHER_PASSWORD": os.getenv("SEED_TEACHER_PASSWORD"),
        "SEED_STUDENT_PASSWORD": os.getenv("SEED_STUDENT_PASSWORD"),
        "DATABASE_PATH": os.getenv("DATABASE_PATH", "instance/campusclaw.sqlite3"),
        "UPLOAD_DIR": os.getenv("UPLOAD_DIR", "instance/uploads"),
        "ALLOWED_EXTENSIONS": os.getenv("ALLOWED_EXTENSIONS", "txt,md"),
        "MAX_UPLOAD_BYTES": os.getenv("MAX_UPLOAD_BYTES", "2097152"),
        "TESTING": False,
        "SESSION_TTL_SECONDS": os.getenv("SESSION_TTL_SECONDS", "28800"),
    }
    if overrides:
        values.update(overrides)

    missing = [
        name
        for name in ("SECRET_KEY", "SEED_TEACHER_PASSWORD", "SEED_STUDENT_PASSWORD")
        if not values.get(name)
    ]
    if missing:
        raise ConfigurationError(
            "Missing required server configuration: " + ", ".join(sorted(missing))
        )

    try:
        max_upload_bytes = int(values["MAX_UPLOAD_BYTES"])
    except (TypeError, ValueError) as exc:
        raise ConfigurationError("MAX_UPLOAD_BYTES must be a positive integer") from exc
    if max_upload_bytes <= 0:
        raise ConfigurationError("MAX_UPLOAD_BYTES must be a positive integer")

    try:
        values["SESSION_TTL_SECONDS"] = int(values["SESSION_TTL_SECONDS"])
        if values["SESSION_TTL_SECONDS"] <= 0:
            raise ValueError
    except (TypeError, ValueError) as exc:
        raise ConfigurationError("SESSION_TTL_SECONDS must be a positive integer") from exc

    extensions = {
        value.strip().lower().lstrip(".")
        for value in str(values["ALLOWED_EXTENSIONS"]).split(",")
        if value.strip()
    }
    if not extensions or not extensions.issubset({"txt", "md"}):
        raise ConfigurationError("ALLOWED_EXTENSIONS may contain only txt and md")

    app_env = str(values["APP_ENV"]).lower()
    values.update(
        {
            "MAX_UPLOAD_BYTES": max_upload_bytes,
            "MAX_CONTENT_LENGTH": max_upload_bytes + 65536,
            "ALLOWED_EXTENSIONS": extensions,
            "DATABASE_PATH": str(Path(str(values["DATABASE_PATH"]))),
            "UPLOAD_DIR": str(Path(str(values["UPLOAD_DIR"]))),
            "SESSION_COOKIE_HTTPONLY": True,
            "SESSION_COOKIE_SAMESITE": "Lax",
            "SESSION_COOKIE_SECURE": app_env not in {"development", "testing"},
        }
    )
    return values
