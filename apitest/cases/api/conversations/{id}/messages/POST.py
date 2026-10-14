from common.sse import events, read_turn


def run(c):
    cid = c.data["conversation"]
    path = f"/api/conversations/{cid}/messages"
    for body in (
        {"text": ""},
        {"text": "字"},
        {"text": "x" * 501},
        {"text": "问题", "class_id": c.classes["b"]},
    ):
        c.denied("POST", path, "student", 400, json=body)
    c.denied(
        "POST",
        path,
        "student",
        400,
        data='{"text":"问题"} {}',
        headers={"Content-Type": "application/json"},
    )
    assert c.query("SELECT * FROM conversation_messages WHERE conversation_id=%s", (cid,)) == []
    c.denied("POST", path, json={"text": "问题"})
    for actor in ("peer", "other", "teacher"):
        c.denied("POST", path, actor, 404, json={"text": "问题"})
    guided, frames, row = read_turn(
        c, cid, c.question, overrides={"agent_id": c.data["other_agent"]["id"]}
    )
    assert frames[0][1]["skill"]["id"] == c.data["skill"]["id"]
    assert frames[0][1]["citations"] and all(
        x["material_id"] == c.data["material"]["id"] for x in frames[0][1]["citations"]
    )
    assert row["skill_enabled"] == 1 and row["skill_name"] == c.data["skill"]["name"]
    assert (
        c.one("SELECT agent_id FROM conversations WHERE id=%s", (cid,))["agent_id"]
        == c.data["agent"]["id"]
    )
    assert (
        c.json("GET", f"/api/conversations/{cid}", "student")["messages"][-1]["content"] == guided
    )
    deltas = [f for f in frames if f[0] == "delta"]
    assert len(deltas) > 1 and deltas[0][2] < frames[-1][2]
    if not c.mock:
        import re

        assert not re.search(r"(?<![0-9])12\s*(?:米|m\b)", guided), "引导回答直接给出最终数值"
    else:
        assert "请你" in guided and "12米" not in guided and frames[-1][2] - deltas[0][2] > 0.05
    skillpath = "/api/skills/" + str(c.data["skill"]["id"])
    c.json(
        "PUT",
        skillpath,
        "teacher",
        json={"source_text": c.data["skill_source"].replace("启用：是", "启用：否")},
    )
    direct, offframes, off = read_turn(c, cid, c.question)
    assert offframes[0][1]["skill"] is None and off["skill_enabled"] == 0 and direct != guided
    if c.mock:
        assert "12米" in direct
    c.json("PUT", skillpath, "teacher", json={"source_text": c.data["skill_source"]})
    agentpath = "/api/agents/" + str(c.data["agent"]["id"])
    edited = c.json(
        "PUT",
        agentpath,
        "teacher",
        json={**c.data["agent_input"], "prompt": "你是角色二。 APITEST_ROLE_V2", "enabled": False},
    )
    _, _, newrow = read_turn(c, cid, c.question)
    assert newrow["prompt_version"] == edited["version"]
    c.denied("POST", "/api/conversations", "student", 404, json={"agent_id": edited["id"]})
    c.json("PUT", agentpath, "teacher", json=c.data["agent_input"])
    empty = c.json(
        "POST", "/api/conversations", "student", 201, json={"agent_id": c.data["empty_agent"]["id"]}
    )["id"]
    text, frames, _ = read_turn(c, empty, "量子跃迁验收专属知识")
    assert text and frames[0][1]["citations"] == [] and "[" not in text
    # Unbound same-class material and zero-score skill never enter the turn.
    text, frames, _ = read_turn(c, cid, "量子跃迁验收专属知识")
    assert frames[0][1]["citations"] == [] and frames[0][1]["skill"] is None
    read_turn(c, c.data["teacher_conversation"], c.question, actor="teacher")
    # Empty material binding: no retrieval dependency is needed, and cross-chunk labels are stripped.
    if c.mock:
        text, _, _ = read_turn(c, empty, "MOCK_STRIP")
        assert "[12]" not in text and "[3]" not in text
        for trigger, code in [
            ("MOCK_FAIL", "model_unavailable"),
            ("MOCK_FINAL 题", "guided_violation"),
        ]:
            _, frames, _ = read_turn(c, c.conversation(), trigger, terminal="error")
            assert frames[-1][1]["code"] == code
        slow = c.conversation()
        response = c.request(
            "POST",
            f"/api/conversations/{slow}/messages",
            "student",
            json={"text": "MOCK_SLOW"},
            stream=True,
        )
        stream = events(response)
        assert next(stream)[0] == "meta"
        assert next(stream)[0] == "delta"
        c.denied(
            "POST", f"/api/conversations/{slow}/messages", "student", 409, json={"text": "新的问题"}
        )
        response.close()
        c.wait_terminal(slow)
        assert (
            c.one(
                "SELECT status FROM conversation_messages WHERE conversation_id=%s AND role=%s",
                (slow, "assistant"),
            )["status"]
            == "cancelled"
        )
        read_turn(c, slow, c.question)
