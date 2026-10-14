def run(c):
    assert c.json("GET", "/health") == {"status": "ok"}
