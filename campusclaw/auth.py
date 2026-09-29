from __future__ import annotations

import secrets
import time
from functools import wraps
from typing import Any, Callable, TypeVar, cast

from argon2.exceptions import InvalidHashError, VerifyMismatchError
from flask import (
    Blueprint,
    Response,
    current_app,
    g,
    jsonify,
    redirect,
    render_template,
    request,
    url_for,
)

from .db import get_db, password_hasher

bp = Blueprint("auth", __name__)
View = TypeVar("View", bound=Callable[..., Any])


@bp.before_app_request
def load_logged_in_user() -> None:
    g.user = None
    session_id = request.cookies.get(current_app.config["SESSION_COOKIE_NAME"])
    if not session_id or request.path == "/health" or request.path.startswith("/static/"):
        return
    g.user = (
        get_db()
        .execute(
            """SELECT u.id, u.username, u.role, u.class_id
            FROM sessions AS s JOIN users AS u ON u.id = s.user_id
            WHERE s.id = ? AND s.expires_at > ?""",
            (session_id, int(time.time())),
        )
        .fetchone()
    )


@bp.after_app_request
def prevent_auth_caching(response):
    if request.path.startswith("/api/") or request.path in {"/login", "/materials"}:
        response.headers["Cache-Control"] = "no-store"
    return response


def _is_api_request() -> bool:
    return request.path.startswith("/api/")


def login_required() -> Callable[[View], View]:
    def decorator(view: View) -> View:
        @wraps(view)
        def wrapped(*args: Any, **kwargs: Any):
            if g.user is None:
                if _is_api_request():
                    return jsonify({"error": "authentication_required"}), 401
                return redirect(url_for("auth.login", next=request.path))
            if g.user["class_id"] is None:
                if _is_api_request():
                    return jsonify({"error": "class_membership_required"}), 403
                return Response("Class membership required", status=403)
            return view(*args, **kwargs)

        return cast(View, wrapped)

    return decorator


def teacher_required(view: View) -> View:
    @wraps(view)
    @login_required()
    def wrapped(*args: Any, **kwargs: Any):
        if g.user["role"] != "teacher":
            return jsonify({"error": "teacher_role_required"}), 403
        return view(*args, **kwargs)

    return cast(View, wrapped)


@bp.get("/login")
def login():
    return render_template("login.html")


@bp.post("/api/login")
def login_api():
    data = request.get_json(silent=True)
    if not isinstance(data, dict) or not all(
        isinstance(data.get(key), str) and data[key].strip() for key in ("username", "password")
    ):
        return jsonify(error="invalid_login_request"), 400
    db = get_db()
    user = db.execute(
        "SELECT id, password_hash FROM users WHERE username = ?", (data["username"].strip(),)
    ).fetchone()
    valid = False
    if user is not None:
        try:
            valid = password_hasher.verify(user["password_hash"], data["password"])
        except (VerifyMismatchError, InvalidHashError):
            pass
    if not valid:
        return jsonify(error="invalid_credentials"), 401
    now = int(time.time())
    session_id = secrets.token_urlsafe(32)
    ttl = current_app.config["SESSION_TTL_SECONDS"]
    with db:
        db.execute(
            "DELETE FROM sessions WHERE expires_at <= ? OR id = ?",
            (
                now,
                request.cookies.get(current_app.config["SESSION_COOKIE_NAME"], ""),
            ),
        )
        db.execute(
            "INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
            (session_id, user["id"], now, now + ttl),
        )
    response = jsonify(status="ok")
    response.set_cookie(
        current_app.config["SESSION_COOKIE_NAME"],
        session_id,
        max_age=ttl,
        httponly=True,
        samesite="Lax",
        secure=current_app.config["SESSION_COOKIE_SECURE"],
        path="/",
    )
    return response


@bp.get("/api/me")
def me():
    if g.user is None:
        return jsonify(error="authentication_required"), 401
    return jsonify(dict(g.user))


@bp.post("/api/logout")
def logout():
    db = get_db()
    with db:
        db.execute(
            "DELETE FROM sessions WHERE id = ?",
            (request.cookies.get(current_app.config["SESSION_COOKIE_NAME"], ""),),
        )
    response = jsonify(status="ok")
    response.delete_cookie(
        current_app.config["SESSION_COOKIE_NAME"],
        path="/",
        httponly=True,
        samesite="Lax",
        secure=current_app.config["SESSION_COOKIE_SECURE"],
    )
    return response
