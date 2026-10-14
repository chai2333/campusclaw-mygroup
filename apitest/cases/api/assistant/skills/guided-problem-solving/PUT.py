def run(c):
    path = "/api/assistant/skills/guided-problem-solving"
    for enabled in (False, True):
        result = c.json("PUT", path, "teacher", json={"enabled": enabled})
        row = c.one("SELECT * FROM class_skills WHERE class_id=%s", (c.classes["a"],))
        assert result["enabled"] == bool(row["enabled"]) == enabled
        assert result["updated_by"] == row["updated_by"] == c.users["teacher"]["id"]
    for body in ({}, {"enabled": "true"}, {"enabled": False, "class_id": c.classes["b"]}):
        c.denied("PUT", path, "teacher", 400, json=body)
    c.denied("PUT", path, "student", 403, json={"enabled": False})
    c.denied("PUT", path, json={"enabled": False})
    assert (
        c.one("SELECT enabled FROM class_skills WHERE class_id=%s", (c.classes["a"],))["enabled"]
        == 1
    )
