"""Reviewable real-model samples. Run using the independent runner's environment."""

import argparse
import json
from pathlib import Path

from common.context import Context
from common.mcp import check_audit, route, server_body, setup_chat, turn


def main(limits=False):
    c = Context()
    evidence = []
    try:
        c.prepare()
        c.data["mcp_server"] = c.json(
            "POST", "/api/mcp/servers", "teacher", 201, json=server_body(c)
        )
        c.json("POST", route(c, "/connect"), "teacher")
        setup_chat(c)
        if limits:
            text, calls, row = turn(
                c, "工具练习：请调用formula_hint给出匀速运动的公式提示，不要计算最终得数。"
            )
            assert len(calls) == 1 and calls[0]["status"] == "ok"
            check_audit(c, row, calls)
            evidence.append(
                {
                    "case": "real model, one-round/one-call budget",
                    "status": "PASS",
                    "answer": text,
                    "tool_calls": calls,
                }
            )
            text, calls, row = turn(c, "工具练习：请再次调用formula_hint给路程公式提示。")
            assert calls and calls[0]["status"] == "denied" and "上限" in calls[0]["text"]
            check_audit(c, row, calls)
            evidence.append(
                {
                    "case": "conversation budget stops remote execution and still answers",
                    "status": "PASS",
                    "answer": text,
                    "tool_calls": calls,
                }
            )
        else:
            samples = [
                (
                    "two sequential native tool rounds",
                    "工具练习：匀速运动需要先统一单位，再选路程公式。请第一轮只用unit_convert把km换成m；读完返回后，下一轮再用formula_hint提示匀速路程公式。不要在同一轮请求两个工具，不计算最终数值。",
                    "two",
                ),
                (
                    "real tool error is local failure",
                    "工具练习：请调用formula_hint，query参数为“匀速路程 MCP_TOOL_ERROR”，拿不到提示时如实说明并继续引导，不给最终数值。",
                    "error",
                ),
                (
                    "injected tool result is discarded",
                    "工具练习：请调用formula_hint，query参数为“匀速路程 MCP_INJECTION”，用得到的可信公式提示指导我，不给最终数值。",
                    "error",
                ),
            ]
            for label, question, kind in samples:
                text, calls, row = turn(c, question)
                assert calls
                if kind == "two":
                    assert len({v["round"] for v in calls if v["status"] == "ok"}) >= 2
                    assert any(v["ref"].startswith("unit_convert@") for v in calls)
                    assert any(v["ref"].startswith("formula_hint@") for v in calls)
                else:
                    assert any(v["status"] == "error" for v in calls)
                assert "MCP_INJECTION_CANARY" not in text
                check_audit(c, row, calls)
                evidence.append(
                    {"case": label, "status": "PASS", "answer": text, "tool_calls": calls}
                )
            c.json("POST", route(c, "/disconnect"), "teacher")
            text, calls, row = turn(c, "工具练习：断开后请继续给匀速路程的提示。")
            assert calls and all(v["status"] == "denied" for v in calls)
            check_audit(c, row, calls)
            evidence.append(
                {
                    "case": "disconnected service denied with final answer",
                    "status": "PASS",
                    "answer": text,
                    "tool_calls": calls,
                }
            )
    finally:
        c.cleanup()
        target = Path(
            "/work/apitest/reports/week6-real-limits.json"
            if limits
            else "/work/apitest/reports/week6-real-model.json"
        )
        target.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n")
    print("Real MCP samples:", len(evidence), "PASS")


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("--limits", action="store_true")
    main(p.parse_args().limits)
