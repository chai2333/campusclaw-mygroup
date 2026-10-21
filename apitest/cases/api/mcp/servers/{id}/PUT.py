from common.mcp import route, server_body


def run(c):
    result = c.json("PUT", route(c), "teacher", json=server_body(c))
    assert not result["connected"] and result["token_configured"]
    absent = c.denied("PUT", "/api/mcp/servers/999999999", "teacher", 404, json=server_body(c))
    foreign = c.denied("PUT", route(c), "other_teacher", 404, json=server_body(c))
    assert absent == foreign
    c.denied("PUT", route(c), "student", 403, json=server_body(c))
