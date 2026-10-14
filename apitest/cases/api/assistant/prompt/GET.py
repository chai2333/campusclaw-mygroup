def run(c):
    result = c.json("GET", "/api/assistant/prompt", "teacher")
    row = c.one("SELECT * FROM assistant_prompts WHERE class_id=%s", (c.classes["a"],))
    assert (
        result["configured"]
        and result["content"] == row["body"]
        and result["version"] == row["version"]
    )
    assert c.json("GET", "/api/assistant/prompt", "other_teacher") == {"configured": False}
    c.denied("GET", "/api/assistant/prompt", "student", 403)
    c.denied("GET", "/api/assistant/prompt")
