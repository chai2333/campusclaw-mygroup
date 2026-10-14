def run(c):
    rows = c.json("GET", "/api/agents", "student")["agents"]
    assert {a["id"] for a in rows} == {c.data["agent"]["id"], c.data["empty_agent"]["id"]}
    assert all(a["enabled"] and a["prompt"] == "" for a in rows)
    c.denied("GET", "/api/agents")
