from __future__ import annotations

from collections.abc import Mapping
from pathlib import Path
from typing import Any

from flask import Flask, redirect, url_for

from .config import load_config


def create_app(test_config: Mapping[str, Any] | None = None) -> Flask:
    app = Flask(__name__, instance_relative_config=True)
    app.config.from_mapping(load_config(test_config))

    Path(app.config["DATABASE_PATH"]).parent.mkdir(parents=True, exist_ok=True)
    Path(app.config["UPLOAD_DIR"]).mkdir(parents=True, exist_ok=True)

    from . import auth, db, health, materials

    db.init_app(app)
    app.register_blueprint(health.bp)
    app.register_blueprint(auth.bp)
    app.register_blueprint(materials.bp)

    @app.get("/")
    @auth.login_required()
    def index():
        return redirect(url_for("materials.materials_page"))

    return app
