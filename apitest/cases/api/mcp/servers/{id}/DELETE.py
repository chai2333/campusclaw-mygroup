from common.mcp import route, turn


def run(c):
    c.denied("DELETE", route(c), "other_teacher", 404)
    c.denied("DELETE", route(c), "student", 403)
    before = c.one("SELECT COUNT(*) AS n FROM mcp_tool_calls WHERE class_id=%s", (c.classes["a"],))[
        "n"
    ]
    c.json("DELETE", route(c), "teacher")
    assert c.query("SELECT * FROM mcp_servers WHERE id=%s", (c.data["mcp_server"]["id"],)) == []
    _text, calls, _row = turn(c, "工具练习：删除服务器后继续给提示。")
    assert calls and all(v["status"] == "denied" for v in calls)
    after = c.one("SELECT COUNT(*) AS n FROM mcp_tool_calls WHERE class_id=%s", (c.classes["a"],))[
        "n"
    ]
    assert after == before
    c.denied("POST", route(c, "/connect"), "teacher", 404)
