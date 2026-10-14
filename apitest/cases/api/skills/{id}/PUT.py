def run(c):
    path = "/api/skills/" + str(c.data["skill"]["id"])
    for enabled in ("否", "是"):
        source = c.data["skill_source"].replace("启用：是", "启用：" + enabled)
        s = c.json("PUT", path, "teacher", json={"source_text": source})
        assert s["enabled"] == (enabled == "是")
        assert (
            bool(c.one("SELECT enabled FROM skills WHERE id=%s", (s["id"],))["enabled"])
            == s["enabled"]
        )
    c.denied("PUT", path, "student", 403, json={"source_text": c.data["skill_source"]})
    c.denied(
        "PUT",
        "/api/skills/" + str(c.data["other_skill"]["id"]),
        "teacher",
        404,
        json={"source_text": c.data["skill_source"]},
    )
