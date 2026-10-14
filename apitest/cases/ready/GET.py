def run(c):
    assert c.json("GET", "/ready") == {"status": "ready"}
