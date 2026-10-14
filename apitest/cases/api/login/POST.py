def run(c):
    for actor in c.users:
        c.login(actor)
    before = c.one("SELECT COUNT(*) AS n FROM sessions")["n"]
    c.denied(
        "POST",
        "/api/login",
        status=401,
        json={"username": c.credentials["student"][0], "password": "incorrect-password"},
    )
    c.denied("POST", "/api/login", status=400, json={"username": "", "password": ""})
    c.denied(
        "POST",
        "/api/login",
        status=400,
        data="not-json",
        headers={"Content-Type": "application/json"},
    )
    assert c.one("SELECT COUNT(*) AS n FROM sessions")["n"] == before
