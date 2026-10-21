from common.mcp import direct_protocol, server_body


def run(c):
    direct_protocol()
    body = server_body(c)
    c.data["mcp_server"] = c.json("POST", "/api/mcp/servers", "teacher", 201, json=body)
    c.data["mcp_other"] = c.json(
        "POST", "/api/mcp/servers", "other_teacher", 201, json=server_body(c, c.prefix + "-other")
    )
    row = c.one("SELECT * FROM mcp_servers WHERE id=%s", (c.data["mcp_server"]["id"],))
    assert row["class_id"] == c.classes["a"] and row["connected"] == 0
    c.denied("POST", "/api/mcp/servers", "teacher", 400, json=body)
    c.denied("POST", "/api/mcp/servers", "student", 403, json=body)
    c.denied("POST", "/api/mcp/servers", json=body)
    for wrong in ["file:///tmp/x", "http://127.0.0.1:1", "http://a:b@mcp-demo:8090"]:
        c.denied(
            "POST",
            "/api/mcp/servers",
            "teacher",
            400,
            json={**body, "name": "bad-server", "base_url": wrong},
        )
