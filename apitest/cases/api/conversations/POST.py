def run(c):
    c.denied("POST", "/api/conversations", "student", 404, json={})
    c.denied(
        "POST", "/api/conversations", "student", 404, json={"agent_id": c.data["other_agent"]["id"]}
    )
    c.denied("POST", "/api/conversations", json={"agent_id": c.data["agent"]["id"]})
    cid = c.conversation()
    c.data["conversation"] = cid
    row = c.one("SELECT * FROM conversations WHERE id=%s", (cid,))
    assert (
        row["agent_id"] == c.data["agent"]["id"]
        and row["owner_user_id"] == c.users["student"]["id"]
        and row["title"] == "新的提问"
    )
    c.data["teacher_conversation"] = c.conversation("teacher")
    # Teacher and student can each own a private conversation.
    assert (
        c.one(
            "SELECT owner_user_id FROM conversations WHERE id=%s", (c.data["teacher_conversation"],)
        )["owner_user_id"]
        == c.users["teacher"]["id"]
    )
