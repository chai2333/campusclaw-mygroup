def run(c):
    for actor, user in c.users.items():
        assert c.json("GET", "/api/me", actor) == user
    c.denied("GET", "/api/me")
