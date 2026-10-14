def run(c):
    rows = c.json("GET", "/api/skills", "student")["skills"]
    assert any(s["id"] == c.data["skill"]["id"] for s in rows)
    for s in rows:
        assert (
            c.one("SELECT class_id FROM skills WHERE id=%s", (s["id"],))["class_id"]
            == c.classes["a"]
        )
    c.denied("GET", "/api/skills")
