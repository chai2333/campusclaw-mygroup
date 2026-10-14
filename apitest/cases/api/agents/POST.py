def run(c):
    c.data["agent_input"] = {
        "name": "数学助手",
        "prompt": "你是本班数学老师。 APITEST_ROLE_V1",
        "enabled": True,
        "skill_ids": [c.data["skill"]["id"], c.data["alternate_skill"]["id"]],
        "material_ids": [c.data["material"]["id"]],
    }
    agent = c.json("POST", "/api/agents", "teacher", 201, json=c.data["agent_input"])
    c.data["agent"] = agent
    row = c.one("SELECT * FROM tutor_agents WHERE id=%s", (agent["id"],))
    assert (
        row["name"] == agent["name"]
        and row["prompt"] == agent["prompt"]
        and row["class_id"] == c.classes["a"]
    )
    assert c.query(
        "SELECT skill_id FROM tutor_agent_skills WHERE agent_id=%s ORDER BY skill_id",
        (agent["id"],),
    ) == [{"skill_id": v} for v in sorted(agent["skill_ids"])]
    foreign = {
        **c.data["agent_input"],
        "skill_ids": [c.data["other_skill"]["id"]],
        "material_ids": [c.data["foreign_material"]["id"]],
    }
    c.data["other_agent"] = c.json("POST", "/api/agents", "other_teacher", 201, json=foreign)
    c.data["empty_agent"] = c.json(
        "POST",
        "/api/agents",
        "teacher",
        201,
        json={
            "name": "自由助手",
            "prompt": "",
            "enabled": True,
            "skill_ids": [],
            "material_ids": [],
        },
    )
    c.denied("POST", "/api/agents", "teacher", 400, json=c.data["agent_input"])
    for body in [
        {**c.data["agent_input"], "name": "x" * 65},
        {**c.data["agent_input"], "name": "another", "prompt": "x" * 2001},
    ]:
        c.denied("POST", "/api/agents", "teacher", 400, json=body)
    c.denied("POST", "/api/agents", "student", 403, json=c.data["agent_input"])
    c.denied("POST", "/api/agents", json=c.data["agent_input"])
