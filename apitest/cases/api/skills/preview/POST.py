def run(c):
    source = (
        "---\nname: shared-writing-review\ndescription: Review English essays\n"
        "license: MIT\nallowed-tools: Bash Read\nmetadata:\n  author: example\n---\n\n"
        "Explain grammar and preserve meaning. See references/style.md."
    )
    count = c.one("SELECT COUNT(*) AS n FROM skills WHERE class_id=%s", (c.classes["a"],))["n"]
    preview = c.json("POST", "/api/skills/preview", "teacher", json={"source_text": source})
    assert preview["format"] == "agent-skills" and not preview["ready"]
    assert not preview["skill"]["enabled"] and preview["skill"]["tools"] == []
    assert len(preview["warnings"]) >= 3
    assert (
        c.one("SELECT COUNT(*) AS n FROM skills WHERE class_id=%s", (c.classes["a"],))["n"] == count
    )
    c.denied("POST", "/api/skills", "teacher", 400, json={"source_text": source})
    configured = c.json(
        "POST",
        "/api/skills/preview",
        "teacher",
        json={
            "source_text": source,
            "name": "共享英语作文批改",
            "trigger_text": "作文,essay",
            "enabled": True,
        },
    )
    assert configured["ready"] and configured["skill"]["enabled"]
    saved = c.json(
        "POST",
        "/api/skills",
        "teacher",
        201,
        json={"source_text": configured["skill"]["source_text"]},
    )
    c.data["portable_skill"] = saved
    row = c.one("SELECT * FROM skills WHERE id=%s", (saved["id"],))
    assert row["description"] == saved["description"] and row["source_text"] == saved["source_text"]
    assert row["class_id"] == c.classes["a"] and "author: example" in row["source_text"]
    disabled = c.json(
        "POST",
        "/api/skills/preview",
        "teacher",
        json={"source_text": saved["source_text"], "enabled": False},
    )
    updated = c.json(
        "PUT",
        f"/api/skills/{saved['id']}",
        "teacher",
        json={"source_text": disabled["skill"]["source_text"]},
    )
    assert (
        not updated["enabled"]
        and not c.one("SELECT enabled FROM skills WHERE id=%s", (saved["id"],))["enabled"]
    )
    for bad in [
        "---\nname: x\nname: y\ndescription: x\n---\nBody",
        "---\nname: x\ndescription: &x hello\nlicense: *x\n---\nBody",
        "x" * 16001,
    ]:
        c.denied("POST", "/api/skills/preview", "teacher", 400, json={"source_text": bad})
    c.denied("POST", "/api/skills/preview", "student", 403, json={"source_text": source})
    c.denied("POST", "/api/skills/preview", json={"source_text": source})
