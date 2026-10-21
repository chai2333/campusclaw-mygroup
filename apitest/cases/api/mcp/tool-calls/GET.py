from common.mcp import check_audit, setup_chat, turn


def run(c):
    setup_chat(c)
    text, calls, row = turn(
        c,
        "工具练习 MCP_TWO_STEP：先用unit_convert把km换成m，再用formula_hint提示匀速运动路程公式，不给最终得数。",
    )
    assert calls and all(v["status"] == "ok" for v in calls)
    if c.mock:
        assert len(calls) == 2 and [v["round"] for v in calls] == [1, 2]
    check_audit(c, row, calls)
    if c.mock:
        for marker, status in [
            ("MCP_BAD_ARGS", "error"),
            ("MCP_TOOL_ERROR", "error"),
            ("MCP_INJECTION", "error"),
        ]:
            text, values, result = turn(
                c, "工具练习 " + marker + "：请用formula_hint给匀速路程提示。"
            )
            assert values and values[0]["status"] == status and "MCP_INJECTION_CANARY" not in text
            check_audit(c, result, values)
        text, values, result = turn(c, "工具练习 MCP_LOOP：请重复使用formula_hint给提示。")
        assert 1 <= len(values) <= 3
        check_audit(c, result, values)
    data = c.json("GET", "/api/mcp/tool-calls", "teacher")["tool_calls"]
    assert data and all(v["user_id"] == c.users["student"]["id"] for v in data)
    assert c.json("GET", "/api/mcp/tool-calls", "other_teacher")["tool_calls"] == []
    c.denied("GET", "/api/mcp/tool-calls", "student", 403)
