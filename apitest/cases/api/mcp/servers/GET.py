import json
import os


def run(c):
    data = c.json("GET", "/api/mcp/servers", "teacher")
    assert [s["id"] for s in data["servers"]] == [c.data["mcp_server"]["id"]]
    assert data["servers"][0]["token_configured"]
    assert (
        os.environ["MCP_AUTH_TOKEN"] not in json.dumps(data)
        and "auth_token" not in json.dumps(data)
        and "token_cipher" not in json.dumps(data)
    )
    c.denied("GET", "/api/mcp/servers", "student", 403)
    c.denied("GET", "/api/mcp/servers")
