def run(c):
    c.data["skill_source"] = (
        "名称：数学解题引导\n适用条件：匀速,速度,路程,方程,题,MOCK_FINAL\n资料：数学\n工具：knowledge_search\n启用：是\n\n先接住学生已有的步骤，指出下一步检查什么。请学生自己完成关键计算，不给最终得数。"
    )
    skill = c.json(
        "POST", "/api/skills", "teacher", 201, json={"source_text": c.data["skill_source"]}
    )
    c.data["skill"] = skill
    row = c.one("SELECT * FROM skills WHERE id=%s", (skill["id"],))
    assert (
        row["name"] == skill["name"]
        and row["source_text"] == skill["source_text"]
        and row["class_id"] == c.classes["a"]
    )
    other = c.json(
        "POST", "/api/skills", "other_teacher", 201, json={"source_text": c.data["skill_source"]}
    )
    c.data["other_skill"] = other
    alternate = c.json(
        "POST",
        "/api/skills",
        "teacher",
        201,
        files={
            "file": (
                "技能.txt",
                "名称：检查思路\n适用条件：方程,阅读\n工具：knowledge_search\n启用：是\n\n检查已知条件。".encode(),
                "text/plain",
            )
        },
    )
    c.data["alternate_skill"] = alternate
    c.denied("POST", "/api/skills", "teacher", 400, json={"source_text": c.data["skill_source"]})
    for source in [
        "名称：x\n适用条件：x\n\n说明",
        "名称：x\n名称：y\n适用条件：x\n启用：是\n\n说明",
        "名称：x\n适用条件：x\n工具：shell\n启用：是\n\n说明",
        "名称：x\n适用条件：x\n启用：是\n未知：x\n\n说明",
        "x" * 8001,
    ]:
        result = c.denied("POST", "/api/skills", "teacher", 400, json={"source_text": source})
        assert result["error"] == "invalid_skill"
    c.denied("POST", "/api/skills", "student", 403, json={"source_text": c.data["skill_source"]})
    c.denied("POST", "/api/skills", json={"source_text": c.data["skill_source"]})
