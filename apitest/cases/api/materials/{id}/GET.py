def run(c):
    result = c.json("GET", "/api/materials/" + c.data["material"]["id"], "student")
    assert result["content"] == c.material_text and result["class_id"] == c.classes["a"]
    foreign = c.denied("GET", "/api/materials/" + c.data["foreign_material"]["id"], "student", 404)
    missing = c.denied("GET", "/api/materials/does-not-exist", "student", 404)
    assert foreign == missing
