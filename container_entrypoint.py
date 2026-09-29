from __future__ import annotations

import os

from campusclaw import create_app
from campusclaw.db import seed_database

app = create_app()
with app.app_context():
    seed_database()

os.execvp(
    "gunicorn",
    ["gunicorn", "--bind", "0.0.0.0:8000", "--workers", "2", "wsgi:app"],
)
