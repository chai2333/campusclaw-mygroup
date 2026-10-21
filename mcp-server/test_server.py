"""Standalone protocol acceptance using the real HTTP server handler."""

import json
import os
import threading
import unittest
from http.server import ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from server import Handler, call_tool


class ProtocolTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.old_token = os.environ.get("MCP_AUTH_TOKEN")
        os.environ["MCP_AUTH_TOKEN"] = "standalone-test-credential-123456"
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = "http://127.0.0.1:" + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()
        if cls.old_token is None:
            os.environ.pop("MCP_AUTH_TOKEN", None)
        else:
            os.environ["MCP_AUTH_TOKEN"] = cls.old_token

    def rpc(self, body, token="standalone-test-credential-123456"):
        request = Request(
            self.base + "/mcp",
            data=json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + token},
        )
        with urlopen(request, timeout=5) as response:
            return response.status, json.loads(response.read())

    def test_health_and_authentication(self):
        with urlopen(self.base + "/health", timeout=5) as response:
            self.assertEqual(json.loads(response.read()), {"status": "ok"})
        with self.assertRaises(HTTPError) as exc:
            self.rpc({"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, "wrong")
        self.assertEqual(exc.exception.code, 401)

    def test_handshake_discovery_and_actual_tools(self):
        _, result = self.rpc(
            {
                "jsonrpc": "2.0",
                "id": "hello",
                "method": "initialize",
                "params": {"protocolVersion": "2025-03-26"},
            }
        )
        self.assertEqual(result["id"], "hello")
        self.assertIn("tools", result["result"]["capabilities"])
        _, result = self.rpc({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
        self.assertEqual(
            {t["name"] for t in result["result"]["tools"]}, {"formula_hint", "unit_convert"}
        )
        for name, args in [
            ("formula_hint", {"query": "匀速运动路程是多少"}),
            ("unit_convert", {"from_unit": "h", "to_unit": "s"}),
        ]:
            _, result = self.rpc(
                {
                    "jsonrpc": "2.0",
                    "id": 2,
                    "method": "tools/call",
                    "params": {"name": name, "arguments": args},
                }
            )
            self.assertFalse(result["result"]["isError"])
            self.assertIn(
                "自行" if name == "formula_hint" else "自己", result["result"]["content"][0]["text"]
            )

    def test_invalid_protocol_and_parameters(self):
        for request, code in [
            ([], -32600),
            ({"jsonrpc": "2.0", "id": 1, "method": "unknown"}, -32601),
            ({"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": []}, -32602),
        ]:
            _, result = self.rpc(request)
            self.assertEqual(result["error"]["code"], code)
        _, result = self.rpc(
            {
                "jsonrpc": "2.0",
                "id": 1,
                "method": "tools/call",
                "params": {"name": "unit_convert", "arguments": {"from_unit": "m", "to_unit": "s"}},
            }
        )
        self.assertTrue(result["result"]["isError"])
        with self.assertRaises(ValueError):
            call_tool("formula_hint", {"query": 123})


if __name__ == "__main__":
    unittest.main()
