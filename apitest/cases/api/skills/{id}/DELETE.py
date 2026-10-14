def run(c):
    sid = c.data["skill"]["id"]
    path = "/api/skills/" + str(sid)
    c.denied("DELETE", path, "student", 403)
    c.denied("DELETE", "/api/skills/" + str(c.data["other_skill"]["id"]), "teacher", 404)
    assert c.json("DELETE", path, "teacher") == {"status": "ok"}
    assert c.query("SELECT * FROM skills WHERE id=%s", (sid,)) == []
    assert c.query("SELECT * FROM tutor_agent_skills WHERE skill_id=%s", (sid,)) == []
    c.denied("DELETE", path, "teacher", 404)
