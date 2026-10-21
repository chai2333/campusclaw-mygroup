from common.mcp import route


def run(c):
    data = c.json("GET", route(c, "/tools"), "teacher")["tools"]
    assert {t["name"] for t in data} == {"formula_hint", "unit_convert"}
    for t in data:
        assert t["description"] and t["inputSchema"]["type"] == "object" and t["approved"]
    c.denied("GET", route(c, "/tools"), "other_teacher", 404)
    c.denied("GET", route(c, "/tools"), "student", 403)
