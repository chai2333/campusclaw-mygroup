def run(c):
    rows = c.json("GET", "/api/materials", "student")["materials"]
    assert any(x["id"] == c.data["material"]["id"] for x in rows)
    assert all(x["class_id"] == c.classes["a"] for x in rows)
    assert not any(x["id"] == c.data["foreign_material"]["id"] for x in rows)
    for x in rows:
        assert (
            x["content"]
            == c.one("SELECT body_text FROM knowledge_entries WHERE material_id=%s", (x["id"],))[
                "body_text"
            ]
        )
    c.denied("GET", "/api/materials")
