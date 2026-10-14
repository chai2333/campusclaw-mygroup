def run(c):
    result = c.json("POST", "/api/ask", "student", json={"question": "匀速运动的路程公式是什么？"})
    assert result["answer"] and result["hits"] and result["citations"]
    c.check_hits(result["hits"])
    assert all(x["material_id"] == c.data["material"]["id"] for x in result["citations"])
    missing = c.json("POST", "/api/ask", "student", json={"question": "量子跃迁验收专属知识"})
    assert missing == {"answer": "资料中未找到相关内容", "hits": [], "citations": []}
    c.denied("POST", "/api/ask", "student", 400, json={"question": ""})
    c.denied("POST", "/api/ask", json={"question": "匀速运动"})
