"""Deterministic OpenAI-compatible provider, only wired into the test stack."""

import json
import re
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # Never log request payloads or Authorization.

    def do_GET(self):
        self.send_response(200 if self.path == "/health" else 404)
        self.end_headers()

    def do_POST(self):
        if self.path == "/v1/embeddings":
            payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            inputs = payload["input"]
            inputs = [inputs] if isinstance(inputs, str) else inputs
            values = []
            for index, text in enumerate(inputs):
                if "匀速运动" in text or "路程" in text:
                    vector = [1.0, 0.0, 0.0, 0.0]
                elif "量子跃迁" in text:
                    vector = [0.0, 1.0, 0.0, 0.0]
                elif "课程导览" in text:
                    vector = [0.0, 0.0, 0.0, 1.0]
                else:
                    vector = [0.0, 0.0, 1.0, 0.0]
                values.append({"index": index, "embedding": vector + [0.0] * 12})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"data": values}).encode())
            return
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        messages = payload["messages"]
        system = messages[0]["content"]
        question = messages[-1]["content"]
        current = question
        if "【本次提问】" in question:
            current = question.split("【本次提问】\n", 1)[1].split("\n\n【资料依据", 1)[0]
        if current.startswith("MOCK_FAIL"):
            self.send_error(503)
            return
        evidence = re.findall(
            r"\[(\d+)\] 材料标题：(.*?)\n位置：(.*?)\n<evidence>\n(.*?)\n</evidence>",
            question,
            re.DOTALL,
        )
        if current.startswith("MOCK_FINAL"):
            text = "最终答案是 12米。"
        elif current.startswith("MOCK_BAD_CITATION"):
            text = "请检查这一步。[99]"
        elif current.startswith("MOCK_SLOW"):
            text = "请你先分析题意，再整理已知条件。" * 12
        elif current.startswith("MOCK_STRIP"):
            text = "没有材料也继续回答。参考[12]与[3]，请你先整理条件。"
        elif "【资料依据" in question:
            if "<evidence>" in system:
                self.send_error(422)
                return
            marker = "角色二" if "APITEST_ROLE_V2" in system else "角色一"
            citation = ""
            if evidence:
                _, title, location, _ = evidence[0]
                citation = f"参照《{title}》{location}。[1]"
            if "【解题引导硬性要求】" in system:
                text = (
                    marker
                    + "："
                    + citation
                    + "先检查速度和时间的单位，再代入路程公式 s=v*t。请你完成最后计算，不必现在给出得数。"
                )
            else:
                text = marker + "：" + citation + "路程等于速度乘以时间，代入3×4得到12米。"
        else:
            evidence = json.loads(question)["evidence"][0]
            text = evidence["text"] + " [1]"
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        try:
            for i in range(0, len(text), 9):
                time.sleep(0.12 if current.startswith("MOCK_SLOW") else 0.025)
                data = {"choices": [{"delta": {"content": text[i : i + 9]}}]}
                self.wfile.write(
                    ("data: " + json.dumps(data, ensure_ascii=False) + "\n\n").encode()
                )
                self.wfile.flush()
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
