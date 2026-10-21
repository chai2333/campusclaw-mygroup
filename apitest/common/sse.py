"""Read real HTTP SSE bytes; never buffer the whole response before inspection."""

import json
import time


def events(response):
    assert response.status_code == 200
    assert "text/event-stream" in response.headers.get("Content-Type", "")
    assert "no-transform" in response.headers.get("Cache-Control", "")
    event, data = None, []
    for line in response.iter_lines(chunk_size=1, decode_unicode=False):
        line = line.decode("utf-8")
        if not line:
            if event is not None:
                assert len(data) == 1, "each frame must carry one JSON data line"
                yield event, json.loads(data[0]), time.monotonic()
            event, data = None, []
        elif line.startswith("event: "):
            assert event is None
            event = line[7:]
        elif line.startswith("data: "):
            data.append(line[6:])
        elif not line.startswith(":"):
            raise AssertionError("unexpected SSE line")
    assert event is None and not data, "incomplete SSE frame"


def read_turn(context, cid, question, terminal="done", actor="student", overrides=None):
    with context.request(
        "POST",
        f"/api/conversations/{cid}/messages",
        actor,
        json={"text": question, **(overrides or {})},
        stream=True,
    ) as response:
        frames = list(events(response))
    names = [e[0] for e in frames]
    assert names[0] == "meta"
    assert names[-1] == terminal and names.count(terminal) == 1
    assert not ({"done", "error"} - {terminal}).intersection(names)
    phase = 0
    for name, payload, _ in frames[1:]:
        if name == "delta":
            phase = 1
            assert payload["text"]
        elif name == "meta":
            assert phase == 0 and payload["conversation_id"] == cid
            assert payload["assistant_message_id"] == frames[0][1]["assistant_message_id"]
        else:
            assert name == terminal
    start = frames[0][1]
    assert start["conversation_id"] == cid
    text = "".join(p["text"] for e, p, _ in frames if e == "delta")
    row = context.one(
        "SELECT * FROM conversation_messages WHERE id=%s", (start["assistant_message_id"],)
    )
    assert row["conversation_id"] == cid and row["content"] == text
    question_row = context.one(
        "SELECT * FROM conversation_messages WHERE id=%s", (start["user_message_id"],)
    )
    assert question_row["content"] == question.strip() and question_row["role"] == "user"
    assert row["sequence_no"] == question_row["sequence_no"] + 1
    assert row["completed_at"] is not None
    context.wait_terminal(cid)
    assert frames[0][1]["status"] == "ok"
    citations = frames[0][1]["citations"]
    selected = frames[0][1]["skill"]
    assert (selected["name"] if selected else None) == row["skill_name"]
    if terminal == "done":
        assert row["status"] == "completed" and not row["error_code"]
        assert frames[-1][1]["assistant_message_id"] == row["id"]
        assert frames[-1][1]["citations"] == citations
        stored = context.query(
            "SELECT * FROM message_citations WHERE assistant_message_id=%s ORDER BY ordinal",
            (row["id"],),
        )
        assert len(stored) == len(citations)
        for actual, db in zip(citations, stored):
            for key in (
                "ordinal",
                "material_id",
                "chunk_id",
                "chunk_index",
                "char_start",
                "char_end",
            ):
                assert actual[key] == db[key]
            assert (
                actual["material_title"] == db["title_snapshot"]
                and actual["excerpt"] == db["excerpt_snapshot"]
            )
            source = context.one(
                "SELECT class_id FROM knowledge_chunks WHERE id=%s", (actual["chunk_id"],)
            )
            assert source["class_id"] == context.users[actor]["class_id"]
    else:
        assert row["status"] == "failed" and row["error_code"] == frames[-1][1]["code"]
        assert (
            context.query(
                "SELECT * FROM message_citations WHERE assistant_message_id=%s", (row["id"],)
            )
            == []
        )
    meta_calls = [p["tool_calls"] for e, p, _ in frames if e == "meta" and "tool_calls" in p]
    stored_calls = row.get("tool_calls_json")
    stored_calls = json.loads(stored_calls) if isinstance(stored_calls, str) else stored_calls
    assert (meta_calls[-1] if meta_calls else None) == stored_calls
    return text, frames, row
