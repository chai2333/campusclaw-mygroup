def run(c):
    path = f"/api/skills/{c.data['skill']['id']}/export"
    file = c.json("GET", path, "teacher")
    assert file["filename"] == "SKILL.md" and file["source_text"].startswith("---\n")
    preview = c.json(
        "POST", "/api/skills/preview", "teacher", json={"source_text": file["source_text"]}
    )
    for key in ["name", "description", "trigger_text", "subject_text", "enabled", "tools"]:
        assert preview["skill"][key] == c.data["skill"][key], f"round trip lost {key}"
    foreign = c.json("GET", f"/api/skills/{c.data['portable_skill']['id']}/export", "teacher")
    assert (
        "license: MIT" in foreign["source_text"]
        and "allowed-tools: Bash Read" in foreign["source_text"]
    )
    assert foreign["directory"] == "shared-writing-review"
    c.denied("GET", path, "student", 403)
    c.denied("GET", path)
    unknown = c.denied("GET", "/api/skills/999999999/export", "teacher", 404)
    other = c.denied("GET", f"/api/skills/{c.data['other_skill']['id']}/export", "teacher", 404)
    assert unknown == other
