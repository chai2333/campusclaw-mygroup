def run(c):
    for mode in ("keyword", "vector", "hybrid"):
        result = c.json(
            "POST",
            "/api/retrieval/search",
            "student",
            json={"query": "匀速运动", "mode": mode, "limit": 4, "class_id": c.classes["b"]},
        )
        assert result["mode"] == mode and result["hits"]
        c.check_hits(result["hits"])
    assert (
        c.json(
            "POST",
            "/api/retrieval/search",
            "student",
            json={"query": "量子跃迁验收专属知识", "mode": "keyword"},
        )["hits"]
        == []
    )
    c.denied("POST", "/api/retrieval/search", "student", 400, json={"query": ""})
    c.denied(
        "POST", "/api/retrieval/search", "student", 400, json={"query": "匀速运动", "mode": "bad"}
    )
    c.denied("POST", "/api/retrieval/search", json={"query": "匀速运动"})
