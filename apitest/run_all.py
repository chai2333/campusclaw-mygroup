#!/usr/bin/env python3
"""python apitest/run_all.py --compose: isolated stack + all API cases + HTML."""

import argparse
import html
import importlib.util
import json
import os
import re
import secrets
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
ORDER = [
    "health/GET.py",
    "ready/GET.py",
    "openapi.json/GET.py",
    "swagger/GET.py",
    "api/login/POST.py",
    "api/me/GET.py",
    "api/materials/POST.py",
    "api/materials/GET.py",
    "api/materials/{id}/GET.py",
    "api/materials/{id}/file/GET.py",
    "api/materials/{id}/reindex/POST.py",
    "api/retrieval/search/POST.py",
    "api/ask/POST.py",
    "api/assistant/prompt/PUT.py",
    "api/assistant/prompt/GET.py",
    "api/assistant/skills/guided-problem-solving/PUT.py",
    "api/assistant/skills/guided-problem-solving/GET.py",
    "api/skills/POST.py",
    "api/skills/GET.py",
    "api/skills/{id}/GET.py",
    "api/skills/{id}/PUT.py",
    "api/agents/POST.py",
    "api/agents/GET.py",
    "api/agents/{id}/GET.py",
    "api/agents/{id}/PUT.py",
    "api/conversations/POST.py",
    "api/conversations/GET.py",
    "api/conversations/{id}/GET.py",
    "api/conversations/{id}/messages/POST.py",
    "api/skills/{id}/DELETE.py",
    "api/logout/POST.py",
]


def redact(value):
    value = str(value)
    for key, val in os.environ.items():
        if any(word in key for word in ("PASSWORD", "SECRET", "TOKEN", "API_KEY")) and val:
            value = value.replace(val, "[REDACTED]")
    value = re.sub(r"Bearer\s+[^\s\"\']+", "Bearer [REDACTED]", value)
    value = re.sub(
        r"(?<![A-Za-z0-9_-])[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])", "[REDACTED_TOKEN]", value
    )
    return value


def report(rows, mode):
    directory = ROOT / "reports"
    directory.mkdir(exist_ok=True)
    parts = [
        '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>CampusClaw 接口测试报告</title>',
        "<style>body{font:16px system-ui;max-width:1100px;margin:32px auto;padding:0 16px}table{border-collapse:collapse;width:100%}td,th{padding:12px;border:1px solid #ddd;text-align:left}.PASS{color:#087443}.FAIL{color:#b42318}pre{white-space:pre-wrap}</style>",
        "<h1>第5课接口验收</h1>",
        f"<p>模型环境：{html.escape(mode)}；通过 {sum(r[1] == 'PASS' for r in rows)}/{len(rows)}。模拟模型测试不等于真实模型行为验收。</p>",
        "<table><tr><th>接口脚本</th><th>结果</th><th>用时</th><th>说明</th></tr>",
    ]
    for name, state, duration, detail in rows:
        parts.append(
            f'<tr><td>{html.escape(name)}</td><td class="{state}">{state}</td><td>{duration:.2f}s</td><td><pre>{html.escape(redact(detail))}</pre></td></tr>'
        )
    parts.append("</table></html>")
    path = directory / "report.html"
    path.write_text("".join(parts), encoding="utf-8")
    (directory / "results.json").write_text(
        json.dumps(
            [{"case": n, "status": s, "duration": d, "detail": redact(e)} for n, s, d, e in rows],
            ensure_ascii=False,
            indent=2,
        )
        + "\n"
    )
    print("HTML report:", path)


def compose(keep=False, real=False):
    # Runtime secrets are generated only into an ignored local .env file.
    directory = ROOT / "reports"
    directory.mkdir(exist_ok=True)
    envpath = directory / ".env"
    if not envpath.exists():
        fd = os.open(envpath, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as f:
            for key in (
                "SESSION_SECRET",
                "DB_PASSWORD",
                "MYSQL_ROOT_PASSWORD",
                "SEED_TEACHER_PASSWORD",
                "SEED_STUDENT_PASSWORD",
                "DIALOGUE_API_KEY",
            ):
                f.write(key + "=" + secrets.token_hex(24) + "\n")
    command = [
        "docker",
        "compose",
        "--project-name",
        "campusclaw-lesson5-apitest",
        "--env-file",
        str(envpath),
        "-f",
        str(ROOT.parent / "compose.apitest.yaml"),
    ]
    if real:
        command += ["-f", str(ROOT.parent / "compose.apitest.real.yaml")]
    result_code = 1
    try:
        subprocess.run(
            command
            + ["up", "-d", "--build", "--force-recreate", "db", "model", "qdrant", "api", "web"],
            check=True,
        )
        subprocess.run(command + ["build", "runner"], check=True)
        result = subprocess.run(command + ["run", "--rm", "runner"], check=False)
        result_code = result.returncode
    except (OSError, subprocess.CalledProcessError) as exc:
        report(
            [("compose setup", "FAIL", 0, type(exc).__name__ + ": " + str(exc))],
            "real" if real else "mock",
        )
        result_code = 1
    finally:
        if not keep:
            # Only this dedicated stack's containers and volumes, never the app stack.
            cleaned = subprocess.run(
                command + ["down", "--volumes", "--remove-orphans"], check=False
            )
            if cleaned.returncode:
                results = ROOT / "reports" / "results.json"
                prior = json.loads(results.read_text()) if results.exists() else []
                rows = [(r["case"], r["status"], r["duration"], r["detail"]) for r in prior]
                rows.append(("compose cleanup", "FAIL", 0, "Dedicated test stack cleanup failed"))
                report(rows, "real" if real else "mock")
                result_code = 1
    return result_code


def run():
    rows = []
    context = None
    try:
        from common.context import Context
        from dotenv import load_dotenv

        load_dotenv(ROOT.parent / ".env", override=False)
        for _ in range(90):
            try:
                import requests

                if (
                    requests.get(
                        os.environ.get("APITEST_BASE_URL", "http://localhost:8080") + "/ready",
                        timeout=2,
                    ).status_code
                    == 200
                ):
                    break
            except requests.RequestException:
                pass
            time.sleep(1)
        else:
            raise RuntimeError("API did not become ready")
        context = Context()
        context.prepare()
        discovered = {str(p.relative_to(ROOT / "cases")) for p in (ROOT / "cases").rglob("*.py")}
        assert discovered == set(ORDER), "runner manifest must include every endpoint script"
        for index, name in enumerate(ORDER):
            began = time.monotonic()
            try:
                spec = importlib.util.spec_from_file_location(
                    "api_case_" + str(index), ROOT / "cases" / name
                )
                module = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(module)
                module.run(context)
                state, detail = "PASS", "接口内容、权限与适用数据库断言通过"
            except Exception as exc:  # noqa: BLE001 -- convert every case/setup/cleanup failure into the report
                state, detail = "FAIL", type(exc).__name__ + ": " + str(exc)
                import traceback

                location = traceback.extract_tb(exc.__traceback__)[-1]
                detail += f" ({Path(location.filename).name}:{location.lineno}: {location.line})"
            rows.append((name, state, time.monotonic() - began, detail))
            print(state, name, redact(detail) if state == "FAIL" else "")
    except Exception as exc:  # noqa: BLE001 -- convert every case/setup/cleanup failure into the report
        rows.append(("setup", "FAIL", 0, type(exc).__name__ + ": " + str(exc)))
    finally:
        if context is not None:
            try:
                context.cleanup()
            except Exception as exc:  # noqa: BLE001 -- cleanup errors must appear in the report
                rows.append(("cleanup", "FAIL", 0, str(exc)))
        report(rows, os.environ.get("APITEST_PROVIDER", "real"))
    return int(any(r[1] == "FAIL" for r in rows))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--compose", action="store_true")
    parser.add_argument(
        "--real-model",
        action="store_true",
        help="Use real dialogue configuration from the local test .env",
    )
    parser.add_argument(
        "--keep-stack", action="store_true", help="Keep the dedicated stack for browser inspection"
    )
    args = parser.parse_args()
    sys.exit(compose(args.keep_stack, args.real_model) if args.compose else run())
