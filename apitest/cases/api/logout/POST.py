def run(c):
    result = c.login("peer")
    token = result["access_token"]
    assert c.json("POST", "/api/logout", "peer") == {"status": "ok"}
    assert c.query("SELECT * FROM sessions WHERE id=%s", (c.session_key(token),)) == []
    c.denied("GET", "/api/me", "peer")
    c.denied("POST", "/api/logout", "peer")
    c.denied("POST", "/api/logout")
