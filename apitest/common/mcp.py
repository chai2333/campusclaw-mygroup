"""Exercise the actual Compose MCP service and native-model orchestration."""

import json
import os

import requests

from common.sse import read_turn


def server_body(c, name=None):
    return {
        "name": name or c.prefix + "-tools",
        "base_url": "http://mcp-demo:8090",
        "use_env_token": True,
    }


def route(c, suffix=""):
    return "/api/mcp/servers/" + str(c.data["mcp_server"]["id"]) + suffix


def direct_protocol():
    base = os.environ.get("APITEST_MCP_BASE_URL", "http://mcp-demo:8090")
    assert requests.get(base + "/health", timeout=5).json() == {"status": "ok"}
    headers = {"Authorization": "Bearer " + os.environ["MCP_AUTH_TOKEN"]}
    body = {"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}
    for auth in [{}, {"Authorization": "Bearer wrong-token"}]:
        assert requests.post(base + "/mcp", json=body, headers=auth, timeout=5).status_code == 401
    result = requests.post(base + "/mcp", json=body, headers=headers, timeout=5).json()
    assert {t["name"] for t in result["result"]["tools"]} == {"formula_hint", "unit_convert"}
    for name, args in [
        ("formula_hint", {"query": "匀速路程公式如何用"}),
        ("unit_convert", {"from_unit": "km", "to_unit": "m"}),
    ]:
        result = requests.post(
            base + "/mcp",
            json={**body, "method": "tools/call", "params": {"name": name, "arguments": args}},
            headers=headers,
            timeout=5,
        ).json()
        assert not result["result"]["isError"] and result["result"]["content"][0]["text"]
    invalid = requests.post(base + "/mcp", data=b"{", headers=headers, timeout=5).json()
    assert invalid["error"]["code"] == -32700
    unknown = requests.post(
        base + "/mcp", json={**body, "method": "unknown"}, headers=headers, timeout=5
    ).json()
    assert unknown["error"]["code"] == -32601


def setup_chat(c):
    name = c.data["mcp_server"]["name"]
    source = f"名称：工具解题引导\n适用条件：工具练习,MCP_\n工具：formula_hint@{name},unit_convert@{name}\n启用：是\n\n需要单位换算时先调用unit_convert，再调用formula_hint取得公式提示。使用工具返回的提示指导学生，不给最终得数。"
    skill = c.json("POST", "/api/skills", "teacher", 201, json={"source_text": source})
    c.data["mcp_skill"] = skill
    agent = c.json(
        "POST",
        "/api/agents",
        "teacher",
        201,
        json={
            "name": c.prefix + "工具助手",
            "prompt": "你是学习助手。学生要求用工具时根据发现声明选择工具，先取提示再回答。",
            "enabled": True,
            "skill_ids": [skill["id"]],
            "material_ids": [],
        },
    )
    c.data["mcp_agent"] = agent
    cid = c.json("POST", "/api/conversations", "student", 201, json={"agent_id": agent["id"]})["id"]
    c.data["mcp_conversation"] = cid
    for tool in [
        "Formula_hint@" + name,
        "formula-hint@" + name,
        "absent@" + name,
        "formula_hint@" + c.prefix + "-other",
        "formula_hint",
    ]:
        bad = source.replace("formula_hint@" + name + ",unit_convert@" + name, tool).replace(
            "工具解题引导", "无效工具技能"
        )
        error = c.denied("POST", "/api/skills", "teacher", 400, json={"source_text": bad})
        assert error["error"] == "invalid_skill" and error.get("message")


def turn(c, question):
    text, frames, row = read_turn(c, c.data["mcp_conversation"], question)
    metas = [p for e, p, _ in frames if e == "meta"]
    assert "tool_calls" not in metas[0]
    calls = metas[-1].get("tool_calls", [])
    for p in metas:
        assert p["skill"]["name"] == "工具解题引导"
    if calls:
        assert json.loads(row["tool_calls_json"]) == calls
        history = c.json("GET", "/api/conversations/" + c.data["mcp_conversation"], "student")
        assert history["messages"][-1]["tool_calls"] == calls
    assert os.environ["MCP_AUTH_TOKEN"] not in json.dumps(frames, ensure_ascii=False)
    return text, calls, row


def check_audit(c, row, calls):
    rows = c.query(
        "SELECT * FROM mcp_tool_calls WHERE assistant_message_id=%s ORDER BY id", (row["id"],)
    )
    assert len(rows) == len(calls)
    for db, value in zip(rows, calls):
        assert (
            db["tool_ref"] == value["ref"]
            and db["status"] == value["status"]
            and db["text"] == value["text"]
            and db["round_no"] == value["round"]
        )
        assert (
            db["class_id"] == c.classes["a"]
            and db["user_id"] == c.users["student"]["id"]
            and db["conversation_id"] == c.data["mcp_conversation"]
        )
