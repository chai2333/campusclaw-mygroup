"""Shared helpers; test data lives only in the configured *_apitest database."""

import base64
import hashlib
import hmac
import os
import time
import uuid

import pymysql
import requests


class Context:
    def __init__(self):
        self.base = os.environ.get("APITEST_BASE_URL", "http://localhost:8080").rstrip("/")
        name = os.environ.get("APITEST_DB_NAME", "")
        if not name.endswith("_apitest"):
            raise ValueError(
                "APITEST_DB_NAME must end with _apitest; use --compose for an isolated stack"
            )
        self.db = pymysql.connect(
            host=os.environ.get("APITEST_DB_HOST", "127.0.0.1"),
            port=int(os.environ.get("APITEST_DB_PORT", "3306")),
            user=os.environ.get("APITEST_DB_USER", "campusclaw"),
            password=os.environ["APITEST_DB_PASSWORD"],
            database=name,
            charset="utf8mb4",
            autocommit=True,
            cursorclass=pymysql.cursors.DictCursor,
        )
        self.secret = os.environ["SESSION_SECRET"]
        self.prefix = "apitest-" + uuid.uuid4().hex[:12]
        self.tokens, self.users, self.classes, self.data = {}, {}, {}, {}
        self.contract = None
        self.credentials = {}
        self.mock = os.environ.get("APITEST_PROVIDER", "real") == "mock"
        self.material_text = "匀速运动解题资料：路程等于速度乘以时间，公式 s=v*t。先确认速度和时间的单位，再代入公式，由学生完成最后计算。"
        self.question = "匀速运动中速度为3米每秒，持续4秒，如何计算路程？"

    def query(self, sql, args=()):
        with self.db.cursor() as cur:
            cur.execute(sql, args)
            return list(cur.fetchall())

    def execute(self, sql, args=()):
        with self.db.cursor() as cur:
            cur.execute(sql, args)
            return cur.lastrowid

    def one(self, sql, args=()):
        rows = self.query(sql, args)
        assert len(rows) == 1, "expected exactly one database record"
        return rows[0]

    def prepare(self):
        for group in ("a", "b"):
            self.classes[group] = self.execute(
                "INSERT INTO classes(slug,name) VALUES(%s,%s)",
                (self.prefix + "-" + group, "接口验收" + group),
            )
        for label, group, role in [
            ("teacher", "a", "teacher"),
            ("student", "a", "student"),
            ("peer", "a", "student"),
            ("other", "b", "student"),
            ("other_teacher", "b", "teacher"),
        ]:
            source = "a-" + role
            row = self.one("SELECT password_hash FROM users WHERE username=%s", (source,))
            username = self.prefix + "-" + label
            uid = self.execute(
                "INSERT INTO users(username,password_hash,role,class_id) VALUES(%s,%s,%s,%s)",
                (username, row["password_hash"], role, self.classes[group]),
            )
            self.users[label] = {
                "id": uid,
                "username": username,
                "role": role,
                "class_id": self.classes[group],
            }
            self.credentials[label] = (username, os.environ["SEED_" + role.upper() + "_PASSWORD"])
            self.execute(
                "INSERT IGNORE INTO class_skills(class_id,skill_key,enabled) VALUES(%s,'guided_problem_solving',1)",
                (self.classes[group],),
            )

    def session_key(self, token):
        return hmac.new(self.secret.encode(), token.encode(), hashlib.sha256).hexdigest()

    def request(self, method, path, actor=None, **kw):
        headers = dict(kw.pop("headers", {}))
        if actor is not None:
            if actor not in self.tokens:
                self.login(actor)
            headers["Authorization"] = "Bearer " + self.tokens[actor]
        return requests.request(method, self.base + path, headers=headers, timeout=(10, 90), **kw)

    def json(self, method, path, actor=None, status=200, **kw):
        response = self.request(method, path, actor, **kw)
        assert response.status_code == status, (
            f"{method} {path}: expected {status}, got {response.status_code}"
        )
        assert "application/json" in response.headers.get("Content-Type", ""), (
            "response must be JSON"
        )
        value = response.json()
        if self.contract is not None and path.startswith("/api/"):
            from jsonschema import RefResolver
            from openapi_schema_validator import OAS30Validator

            canonical = path.split("?")[0]
            canonical = __import__("re").sub(
                r"/api/mcp/servers/[^/]+", "/api/mcp/servers/{id}", canonical
            )
            canonical = __import__("re").sub(
                r"/api/materials/[^/]+", "/api/materials/{id}", canonical
            )
            canonical = __import__("re").sub(
                r"/api/conversations/[^/]+", "/api/conversations/{id}", canonical
            )
            canonical = __import__("re").sub(r"/api/agents/[^/]+", "/api/agents/{id}", canonical)
            canonical = __import__("re").sub(r"/api/skills/[0-9]+", "/api/skills/{id}", canonical)
            schema = self.contract["paths"][canonical][method.lower()]["responses"][str(status)][
                "content"
            ]["application/json"]["schema"]
            OAS30Validator(schema, resolver=RefResolver.from_schema(self.contract)).validate(value)
        return value

    def login(self, actor):
        username, password = self.credentials[actor]
        result = self.json("POST", "/api/login", json={"username": username, "password": password})
        assert result["token_type"] == "Bearer" and result["expires_in"] > 0
        token = result["access_token"]
        assert len(base64.urlsafe_b64decode(token + "=")) == 32
        row = self.one("SELECT * FROM sessions WHERE id=%s", (self.session_key(token),))
        assert row["user_id"] == self.users[actor]["id"] and row["expires_at"] > row["created_at"]
        assert row["expires_at"] - row["created_at"] == result["expires_in"]
        self.tokens[actor] = token
        return result

    def denied(self, method, path, actor=None, status=401, **kw):
        result = self.json(method, path, actor, status, **kw)
        assert set(result).issubset({"error", "message"}) and isinstance(result["error"], str)
        return result

    def upload(self, actor="teacher", text=None):
        result = self.json(
            "POST",
            "/api/materials",
            actor,
            201,
            files={
                "file": ("数学验收资料.txt", (text or self.material_text).encode(), "text/plain")
            },
            data={"title": self.prefix + "-解题资料"},
        )
        row = self.one("SELECT * FROM materials WHERE id=%s", (result["id"],))
        assert (
            row["class_id"] == self.users[actor]["class_id"]
            and row["uploader_id"] == self.users[actor]["id"]
        )
        body = self.one(
            "SELECT body_text FROM knowledge_entries WHERE material_id=%s", (result["id"],)
        )
        assert body["body_text"] == (text or self.material_text)
        return result

    def conversation(self, actor="student"):
        agent = (
            self.data["agent"]
            if self.users[actor]["class_id"] == self.classes["a"]
            else self.data["other_agent"]
        )
        return self.json("POST", "/api/conversations", actor, 201, json={"agent_id": agent["id"]})[
            "id"
        ]

    def check_hits(self, hits, actor="student"):
        for hit in hits:
            row = self.one(
                "SELECT c.*,m.title FROM knowledge_chunks c JOIN materials m ON m.id=c.material_id WHERE c.id=%s",
                (hit["chunk_id"],),
            )
            assert row["class_id"] == self.users[actor]["class_id"]
            for key in ("material_id", "chunk_index", "char_start", "char_end"):
                assert hit[key] == row[key], "retrieval metadata differs from database"
            assert hit["excerpt"] == row["chunk_text"] and hit["material_title"] == row["title"]

    def wait_terminal(self, cid):
        for _ in range(100):
            row = self.one("SELECT active_message_id FROM conversations WHERE id=%s", (cid,))
            if row["active_message_id"] is None:
                return
            time.sleep(0.1)
        raise AssertionError("conversation lock was not cleared")

    def cleanup(self):
        # Only this run's classes/users; no existing class or settings are touched.
        for cid in self.classes.values():
            self.execute("DELETE FROM mcp_tool_changes WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM mcp_servers WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM conversations WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM tutor_agents WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM skills WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM materials WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM assistant_prompts WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM class_skills WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM users WHERE class_id=%s", (cid,))
            self.execute("DELETE FROM classes WHERE id=%s", (cid,))
        self.db.close()
