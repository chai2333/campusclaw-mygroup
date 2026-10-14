def run(c):
    path = "/api/agents/" + str(c.data["agent"]["id"])
    for enabled in (False, True):
        a = c.json("PUT", path, "teacher", json={**c.data["agent_input"], "enabled": enabled})
        c.data["agent"] = a
        row = c.one("SELECT * FROM tutor_agents WHERE id=%s", (a["id"],))
        assert a["version"] == row["version"] and bool(row["enabled"]) == enabled
        if not enabled:
            assert a["id"] not in {
                x["id"] for x in c.json("GET", "/api/agents", "student")["agents"]
            }
            assert c.denied("GET", path, "student", 404) == c.denied(
                "GET", "/api/agents/99999999", "student", 404
            )
            c.denied("POST", "/api/conversations", "student", 404, json={"agent_id": a["id"]})
    before = c.json("GET", path, "teacher")
    for change in [
        {"skill_ids": [c.data["other_skill"]["id"]]},
        {"material_ids": [c.data["foreign_material"]["id"]]},
    ]:
        c.denied("PUT", path, "teacher", 404, json={**c.data["agent_input"], **change})
        assert c.json("GET", path, "teacher") == before
    c.denied("PUT", path, "student", 403, json=c.data["agent_input"])
