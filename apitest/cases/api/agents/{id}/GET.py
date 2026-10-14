def run(c):
    assert c.json("GET", "/api/agents/" + str(c.data["agent"]["id"]), "teacher") == c.data["agent"]
    assert c.denied(
        "GET", "/api/agents/" + str(c.data["other_agent"]["id"]), "student", 404
    ) == c.denied("GET", "/api/agents/99999999", "student", 404)
