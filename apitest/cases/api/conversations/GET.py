def run(c):
    assert any(
        x["id"] == c.data["conversation"] and x["agent_id"] == c.data["agent"]["id"]
        for x in c.json("GET", "/api/conversations", "student")["conversations"]
    )
    assert c.json("GET", "/api/conversations", "peer")["conversations"] == []
    teacher = c.json("GET", "/api/conversations", "teacher")["conversations"]
    assert teacher and all(x["id"] != c.data["conversation"] for x in teacher)
    c.denied("GET", "/api/conversations")
