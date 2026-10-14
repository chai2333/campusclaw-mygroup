def run(c):
    path = "/api/materials/" + c.data["material"]["id"] + "/reindex"
    result = c.json("POST", path, "teacher", json={"strategy": "auto"})
    rows = c.query(
        "SELECT * FROM knowledge_chunks WHERE material_id=%s", (c.data["material"]["id"],)
    )
    assert result["ready_chunks"] == len(rows) > 0 and result["failed_chunks"] == 0
    assert all(x["index_status"] == "ready" for x in rows)
    c.denied("POST", path, "student", 403, json={})
    c.denied(
        "POST",
        "/api/materials/" + c.data["foreign_material"]["id"] + "/reindex",
        "teacher",
        404,
        json={},
    )
