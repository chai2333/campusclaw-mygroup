// Command openapi generates operation contracts from source handler annotations.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type record map[string]any

func main() {
	root := flag.String("root", ".", "backend source root")
	flag.Parse()
	raw, err := os.ReadFile(filepath.Join(*root, "internal/apidocs/contracts.json"))
	check(err)
	doc := record{}
	check(json.Unmarshal(raw, &doc))
	paths := record{}
	check(filepath.WalkDir(*root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var op record
		var route, method string
		flush := func() {
			if op == nil {
				return
			}
			responses := op["responses"].(record)
			role, _ := op["x-role"].(string)
			if role == "public" {
				op["security"] = []any{}
			} else {
				responses["401"] = response("authentication_required", "Error")
				responses["403"] = response("角色或班级权限不足", "Error")
			}
			if _, ok := responses["503"]; !ok && role != "public" {
				responses["503"] = response("service_unavailable", "Error")
			}
			op["description"] = "权限：" + role + "。 " + fmt.Sprint(op["summary"])
			if strings.Contains(route, "{id}") {
				op["parameters"] = []any{record{"name": "id", "in": "path", "required": true, "schema": record{"type": "string"}, "description": "资源编号；越权与不存在同形404"}}
			}
			if _, ok := paths[route]; !ok {
				paths[route] = record{}
			}
			methods := paths[route].(record)
			if _, ok := methods[method]; ok {
				panic("duplicate annotated route " + route)
			}
			methods[method] = op
			op = nil
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "// @") {
				if op != nil && !strings.HasPrefix(trimmed, "//") {
					flush()
				}
				continue
			}
			pieces := strings.SplitN(strings.TrimPrefix(trimmed, "// @"), " ", 2)
			if len(pieces) < 2 {
				continue
			}
			key, value := pieces[0], pieces[1]
			if key == "Route" {
				flush()
				parts := strings.Fields(value)
				if len(parts) != 2 {
					return fmt.Errorf("bad @Route in %s", path)
				}
				method = strings.ToLower(parts[0])
				route = parts[1]
				op = record{"responses": record{}, "tags": []string{tag(route)}, "operationId": strings.ReplaceAll(strings.ReplaceAll(method+"_"+route, "/", "_"), "{id}", "id")}
				continue
			}
			if op == nil {
				continue
			}
			switch key {
			case "Summary":
				op["summary"] = value
			case "Role":
				op["x-role"] = value
			case "Deprecated":
				op["deprecated"] = true
			case "Request":
				op["requestBody"] = record{"required": true, "content": record{"application/json": record{"schema": ref(value)}}}
			case "Multipart":
				body, ok := op["requestBody"].(record)
				if !ok {
					body = record{"required": true, "content": record{}}
					op["requestBody"] = body
				}
				body["content"].(record)["multipart/form-data"] = record{"schema": ref("UploadInput")}
			case "Query":
				op["parameters"] = []any{record{"name": value, "in": "query", "schema": record{"type": "string"}}}
			case "Response":
				parts := strings.Fields(value)
				if len(parts) != 2 {
					return fmt.Errorf("bad @Response in %s", path)
				}
				r := response("成功", parts[1])
				if parts[1] == "Stream" {
					r = record{"description": "meta → meta(工具调用累计快照)* → delta* → done/error；无调用时省略tool_calls，meta重复时ID/技能/引用保持一致且均在首字前，done只在提交后发送。流开始前失败使用JSON状态码，之后使用error事件。", "content": record{"text/event-stream": record{"schema": record{"type": "string"}, "example": "event: meta\ndata: {\"status\":\"ok\",\"skill\":null,\"citations\":[],\"conversation_id\":\"c\",\"user_message_id\":\"u\",\"assistant_message_id\":\"a\"}\n\nevent: delta\ndata: {\"text\":\"下一步提示\"}\n\nevent: done\ndata: {\"assistant_message_id\":\"a\",\"status\":\"completed\",\"citations\":[]}\n\n"}}}
					op["x-sse-events"] = record{"meta": ref("StreamMeta"), "delta": ref("StreamDelta"), "done": ref("StreamDone"), "error": ref("StreamError")}
				} else if parts[1] == "Binary" {
					r = record{"description": "下载文件", "content": record{"application/octet-stream": record{"schema": record{"type": "string", "format": "binary"}}}}
				}
				op["responses"].(record)[parts[0]] = r
			case "Error":
				parts := strings.SplitN(value, " ", 2)
				op["responses"].(record)[parts[0]] = response(parts[1], "Error")
			}
		}
		flush()
		return nil
	}))
	doc["paths"] = paths
	encoded, err := json.MarshalIndent(doc, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(*root, "internal/apidocs/openapi.json"), append(encoded, '\n'), 0644))
	fmt.Printf("Generated OpenAPI: %d paths from handler annotations\n", len(paths))
}
func ref(name string) record { return record{"$ref": "#/components/schemas/" + name} }
func response(description, schema string) record {
	return record{"description": description, "content": record{"application/json": record{"schema": ref(schema)}}}
}
func tag(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 1 && parts[0] == "api" {
		return parts[1]
	}
	return "health"
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
