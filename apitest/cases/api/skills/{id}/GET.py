def run(c):
    s = c.json("GET", "/api/skills/" + str(c.data["skill"]["id"]), "student")
    assert s["source_text"] == c.data["skill_source"] and s["tools"] == ["knowledge_search"]
    assert c.denied(
        "GET", "/api/skills/" + str(c.data["other_skill"]["id"]), "student", 404
    ) == c.denied("GET", "/api/skills/999999999", "student", 404)
