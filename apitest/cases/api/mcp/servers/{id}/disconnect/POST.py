from common.mcp import check_audit, route, turn


def run(c):
    c.json("POST", route(c, "/disconnect"), "teacher")
    assert c.json("GET", route(c, "/tools"), "teacher")["tools"] == []
    _text, calls, row = turn(c, "工具练习：请使用公式提示。")
    assert calls and all(v["status"] == "denied" for v in calls)
    check_audit(c, row, calls)
    c.denied(
        "PUT",
        "/api/skills/" + str(c.data["mcp_skill"]["id"]),
        "teacher",
        400,
        json={"source_text": c.data["mcp_skill"]["source_text"]},
    )
    c.denied("POST", route(c, "/disconnect"), "other_teacher", 404)
    c.denied("POST", route(c, "/disconnect"), "student", 403)
