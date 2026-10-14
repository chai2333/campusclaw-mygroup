def run(c):
    path = "/api/assistant/prompt"
    for version in (1, 2):
        body = "你是本班学习助手。以清楚的中文回应学生。 APITEST_ROLE_V" + str(version)
        result = c.json("PUT", path, "teacher", json={"content": body})
        row = c.one("SELECT * FROM assistant_prompts WHERE class_id=%s", (c.classes["a"],))
        assert result["content"] == row["body"] == body
        assert result["version"] == row["version"] == version
        assert result["updated_by"] == row["updated_by"] == c.users["teacher"]["id"]
    c.data["prompt_version"] = 2
    for body in (
        {"content": ""},
        {"content": "x" * 12001},
        {"content": "ok", "class_id": c.classes["b"]},
    ):
        c.denied("PUT", path, "teacher", 400, json=body)
    c.denied("PUT", path, "student", 403, json={"content": "student override"})
    c.denied("PUT", path, json={"content": "unauthenticated override"})
    assert (
        c.one("SELECT version FROM assistant_prompts WHERE class_id=%s", (c.classes["a"],))[
            "version"
        ]
        == 2
    )
    assert c.query("SELECT * FROM assistant_prompts WHERE class_id=%s", (c.classes["b"],)) == []
