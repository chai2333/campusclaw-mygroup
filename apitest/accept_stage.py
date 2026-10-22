"""Stage acceptance: actual HTTP payloads to an isolated mock LLM + real MCP.

Run in compose.apitest.yaml's runner after the API suite. Captures test data
only, never Authorization headers. Does not contact an external model.
"""
import json
from pathlib import Path

import requests

from common.context import Context
from common.sse import read_turn

MODEL = "http://model:8090"
PROMPT = "STAGE_ACCEPT_PROMPT：你是数学学习助手，先检查单位，再给下一步提示。"
BODY = "先判断学生已有步骤，提示统一单位，再提示路程公式。请学生自行完成关键计算。SKILL_BODY_END"
QUESTION = "速度每小时3公里，运动2分钟，路程怎么求？给我提示。"


def traces():
    r = requests.get(MODEL + "/test/llm-traces", timeout=5)
    r.raise_for_status()
    return r.json()


def main():
    c = Context()
    assert c.mock, "this evidence capture requires the isolated mock provider"
    c.prepare()
    evidence = {"environment": "isolated mock LLM over actual HTTP, real same-repo MCP and MySQL/Qdrant", "scenarios": []}
    try:
        # More than four source chunks prove that retrieval sends a subset.
        text = "\n\n".join(f"第{i}节 匀速运动：路程等于速度乘以时间，先统一单位。" * 30 for i in range(12))
        material = c.upload(text=text)
        chunks = c.one("SELECT COUNT(*) AS n FROM knowledge_chunks WHERE material_id=%s", (material["id"],))["n"]
        assert chunks > 4
        server = c.json("POST", "/api/mcp/servers", "teacher", 201, json={"name": c.prefix+"-tools", "base_url": "http://mcp-demo:8090", "use_env_token": True})
        c.json("POST", f"/api/mcp/servers/{server['id']}/connect", "teacher")
        refs = [f"formula_hint@{server['name']}", f"unit_convert@{server['name']}"]
        source = "名称：阶段数学解题引导\n适用条件：速度,路程\n工具：knowledge_search\n启用：是\n\n" + BODY
        skill = c.json("POST", "/api/skills", "teacher", 201, json={"source_text": source})
        agent = c.json("POST", "/api/agents", "teacher", 201, json={"name":c.prefix+"验收应用", "prompt":PROMPT, "enabled":True, "skill_ids":[skill["id"]], "material_ids":[material["id"]]})

        def run_case(name, policy, tools, use_skill=True, question=QUESTION):
            nonlocal skill
            draft = c.json("POST", "/api/skills/preview", "teacher", json={"source_text":skill["source_text"], "tools":tools, "tool_policy":policy})
            skill = c.json("PUT", f"/api/skills/{skill['id']}", "teacher", json={"source_text":draft["skill"]["source_text"]})
            loaded = c.json("GET", f"/api/skills/{skill['id']}", "teacher")
            assert loaded["tool_policy"] == policy and loaded["tools"] == tools
            file = c.json("GET", f"/api/skills/{skill['id']}/export", "teacher")
            assert "campusclaw-tool-policy: " + policy in file["source_text"]
            c.json("PUT", f"/api/agents/{agent['id']}", "teacher", json={"name":agent["name"], "prompt":PROMPT, "enabled":True, "skill_ids":[skill["id"]] if use_skill else [], "material_ids":[material["id"]]})
            cid = c.json("POST", "/api/conversations", "student", 201, json={"agent_id":agent["id"]})["id"]
            previous = traces()
            before = previous[-1]["id"] if previous else 0
            answer, frames, row = read_turn(c, cid, question)
            exchanges = [e for e in traces() if e["id"] > before]
            assert exchanges and exchanges[-1]["request"]["stream"]
            for exchange in exchanges:
                req = exchange["request"]
                assert PROMPT in req["messages"][0]["content"]
                assert (BODY in req["messages"][0]["content"]) == use_skill
                assert "工具策略：" not in req["messages"][0]["content"]
                assert text not in json.dumps(req, ensure_ascii=False)
                user = next(m["content"] for m in req["messages"] if "【本次提问】" in m["content"])
                assert 0 < user.count("<evidence>") <= 4 < chunks
                assert len(req.get("tools", [])) == (2 if not req["stream"] else 0)
            calls = json.loads(row["tool_calls_json"] or "[]")
            final = exchanges[-1]["request"]
            assert "tool_choice" not in final and "tools" not in final
            if policy == "required":
                assert exchanges[0]["request"]["tool_choice"] == "required"
                assert any(call["status"]=="ok" for call in calls)
                assert any(m["role"]=="tool" and m.get("tool_call_id") for m in final["messages"])
                assert all("tool_choice" not in e["request"] for e in exchanges[1:])
            elif tools == ["knowledge_search"]:
                assert len(exchanges)==1 and not calls
            else:
                assert len(exchanges)==2 and not calls and "tool_choice" not in exchanges[0]["request"]
            assert answer == exchanges[-1]["response"]["assembled_text"]
            evidence["scenarios"].append({"name":name, "skill_selected":use_skill, "tool_policy":policy, "source_chunk_count":chunks, "source_character_count":len(text), "exchanges":exchanges, "tool_calls":calls, "answer":answer})

        run_case("prompt-and-retrieval-without-skill", "auto", ["knowledge_search"], False)
        run_case("full-selected-skill-body", "auto", ["knowledge_search"])
        run_case("auto-model-skips-tools", "auto", ["knowledge_search"]+refs, question=QUESTION+" MCP_SKIP")
        run_case("required-tool-call-with-natural-question", "required", ["knowledge_search"]+refs)
        # Required without a MCP binding must not be saved.
        c.denied("POST", "/api/skills/preview", "teacher", 400, json={"source_text":skill["source_text"], "tools":["knowledge_search"], "tool_policy":"required"})
        raw = json.dumps(evidence, ensure_ascii=False, indent=2)+"\n"
        import os
        for key, value in os.environ.items():
            if value and any(x in key for x in ("PASSWORD", "SECRET", "TOKEN", "API_KEY")):
                assert value not in raw, "credential found in evidence"
        path = Path(__file__).parent / "reports" / "stage-llm-trace.json"
        path.write_text(raw)
        print("Stage acceptance: 4/4 scenarios PASS; safe request/response evidence:", path)
    finally:
        c.cleanup()


if __name__ == "__main__":
    main()
