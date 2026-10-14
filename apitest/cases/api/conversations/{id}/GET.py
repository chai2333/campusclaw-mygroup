def run(c):
    path = "/api/conversations/" + c.data["conversation"]
    result = c.json("GET", path, "student")
    assert result["messages"] == [] and result["conversation"]["agent_id"] == c.data["agent"]["id"]
    missing = c.denied("GET", "/api/conversations/unknown", "student", 404)
    for actor in ("peer", "other", "teacher"):
        assert c.denied("GET", path, actor, 404) == missing
    c.denied("GET", path)
