from common.mcp import route, server_body


def run(c):
    for _ in range(2):
        result = c.json("POST", route(c, "/connect"), "teacher")
        assert result["connected"]
        assert (
            c.one("SELECT COUNT(*) AS n FROM mcp_tools WHERE server_id=%s", (result["id"],))["n"]
            == 2
        )
    c.json(
        "PUT",
        route(c),
        "teacher",
        json={
            **server_body(c),
            "use_env_token": False,
            "auth_token": "test-wrong-mcp-token-123456",
        },
    )
    c.denied("POST", route(c, "/connect"), "teacher", 502)
    row = c.one(
        "SELECT connected,last_error FROM mcp_servers WHERE id=%s", (c.data["mcp_server"]["id"],)
    )
    assert not row["connected"] and row["last_error"]
    assert (
        c.query("SELECT * FROM mcp_tools WHERE server_id=%s", (c.data["mcp_server"]["id"],)) == []
    )
    c.json("PUT", route(c), "teacher", json=server_body(c))
    c.json("POST", route(c, "/connect"), "teacher")
    c.denied("POST", route(c, "/connect"), "other_teacher", 404)
    c.denied("POST", route(c, "/connect"), "student", 403)
