def run(c):
    r = c.request("GET", "/api/materials/" + c.data["material"]["id"] + "/file", "student")
    assert r.status_code == 200 and r.content.decode("utf-8") == c.material_text
    assert "attachment" in r.headers["Content-Disposition"]
    assert c.denied(
        "GET", "/api/materials/" + c.data["foreign_material"]["id"] + "/file", "student", 404
    ) == c.denied("GET", "/api/materials/unknown/file", "student", 404)
