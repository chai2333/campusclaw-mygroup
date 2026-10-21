"""Real English review with one portable skill shared by two isolated agents."""

import argparse
import hashlib
import json
from pathlib import Path

from common.context import Context
from common.sse import read_turn


def main(external_file=None):
    c = Context()
    records = []
    try:
        c.prepare()
        source = Path("/work/skills/english-writing-review/SKILL.md").read_text()
        skill = c.json("POST", "/api/skills", "teacher", 201, json={"source_text": source})
        exported = c.json("GET", f"/api/skills/{skill['id']}/export", "teacher")
        parsed = c.json(
            "POST", "/api/skills/preview", "teacher", json={"source_text": exported["source_text"]}
        )
        assert parsed["skill"]["description"] == skill["description"]
        question = "请批改英语作文，解释必须修改和可选优化，不要给没有依据的考试分数：Yesterday, she go to school and buy a apple. I am agree with her idea."
        for name in ["英语写作助手", "综合学习助手"]:
            agent = c.json(
                "POST",
                "/api/agents",
                "teacher",
                201,
                json={
                    "name": name,
                    "prompt": "用中文解释，保留英文例句，帮助学生改进写作。",
                    "enabled": True,
                    "skill_ids": [skill["id"]],
                    "material_ids": [],
                },
            )
            cid = c.json(
                "POST", "/api/conversations", "student", 201, json={"agent_id": agent["id"]}
            )["id"]
            text, frames, row = read_turn(c, cid, question)
            assert row["skill_id"] == skill["id"] and frames[0][1]["skill"]["id"] == skill["id"]
            assert not frames[0][1]["citations"] and "tool_calls" not in frames[0][1]
            # These are unambiguous corrections in this fixed sample; retain
            # the full answer for manual assessment of explanation quality.
            lowered = text.lower()
            assert (
                "went" in lowered
                and "bought" in lowered
                and "an apple" in lowered
                and "agree" in lowered
            )
            assert (
                "i agree with her idea that" not in lowered
                and "went to school to buy" not in lowered
            )
            records.append(
                {"case": name, "status": "PASS", "skill_name": row["skill_name"], "answer": text}
            )
        foreign = {
            "name": "外班助手",
            "enabled": True,
            "prompt": "",
            "skill_ids": [skill["id"]],
            "material_ids": [],
        }
        c.denied("POST", "/api/agents", "other_teacher", 404, json=foreign)
        if external_file:
            external = Path(external_file).read_text()
            preview = c.json(
                "POST",
                "/api/skills/preview",
                "teacher",
                json={
                    "source_text": external,
                    "name": "第三方前端设计",
                    "trigger_text": "前端设计",
                    "enabled": True,
                },
            )
            assert preview["ready"] and preview["skill"]["tools"] == []
            imported = c.json(
                "POST",
                "/api/skills",
                "teacher",
                201,
                json={"source_text": preview["skill"]["source_text"]},
            )
            agent = c.json(
                "POST",
                "/api/agents",
                "teacher",
                201,
                json={
                    "name": "第三方技能演示",
                    "enabled": True,
                    "prompt": "用简洁中文回应设计问题，不声称已经修改文件或运行浏览器。",
                    "skill_ids": [imported["id"]],
                    "material_ids": [],
                },
            )
            cid = c.json(
                "POST", "/api/conversations", "student", 201, json={"agent_id": agent["id"]}
            )["id"]
            text, frames, row = read_turn(
                c,
                cid,
                "前端设计：为校园摄影社设计作品展示页，目标是让新同学浏览照片并报名。请只给简短设计方案，含配色、字体、布局，不写代码。",
            )
            assert (
                row["skill_id"] == imported["id"]
                and frames[0][1]["skill"]["id"] == imported["id"]
                and text
            )
            records.append(
                {
                    "case": "third-party frontend-design",
                    "status": "PASS",
                    "source": "https://github.com/anthropics/skills/tree/main/skills/frontend-design",
                    "sha256": hashlib.sha256(external.encode()).hexdigest(),
                    "answer": text,
                }
            )
    finally:
        c.cleanup()
        Path("/work/apitest/reports/portable-real-model.json").write_text(
            json.dumps(records, ensure_ascii=False, indent=2) + "\n"
        )
    print("Portable real-model samples:", len(records), "PASS")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--external-file")
    main(parser.parse_args().external_file)
