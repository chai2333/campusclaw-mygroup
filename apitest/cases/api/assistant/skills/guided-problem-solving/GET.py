def run(c):
    result = c.json("GET", "/api/assistant/skills/guided-problem-solving", "teacher")
    assert result["skill_key"] == "guided_problem_solving" and result["enabled"]
    row = c.one("SELECT * FROM class_skills WHERE class_id=%s", (c.classes["a"],))
    assert result["updated_by"] == row["updated_by"]
    c.denied("GET", "/api/assistant/skills/guided-problem-solving", "student", 403)
    c.denied("GET", "/api/assistant/skills/guided-problem-solving")
