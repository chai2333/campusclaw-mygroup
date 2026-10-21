from common.mcp import route


def run(c):
    c.json("POST", route(c, "/tools/approve"), "teacher", json={"names": ["formula_hint"]})
    assert c.one(
        "SELECT approved FROM mcp_tools WHERE server_id=%s AND name=%s",
        (c.data["mcp_server"]["id"], "formula_hint"),
    )["approved"]
    c.denied("POST", route(c, "/tools/approve"), "teacher", 400, json={"names": ["absent"]})
    c.denied(
        "POST", route(c, "/tools/approve"), "other_teacher", 404, json={"names": ["formula_hint"]}
    )
    c.denied("POST", route(c, "/tools/approve"), "student", 403, json={"names": ["formula_hint"]})
