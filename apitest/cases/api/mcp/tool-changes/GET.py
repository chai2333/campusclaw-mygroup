def run(c):
    data = c.json("GET", "/api/mcp/tool-changes", "teacher")["changes"]
    rows = c.query(
        "SELECT * FROM mcp_tool_changes WHERE class_id=%s ORDER BY id DESC LIMIT 100",
        (c.classes["a"],),
    )
    assert [v["id"] for v in data] == [v["id"] for v in rows]
    assert c.json("GET", "/api/mcp/tool-changes", "other_teacher")["changes"] == []
    c.denied("GET", "/api/mcp/tool-changes", "student", 403)
