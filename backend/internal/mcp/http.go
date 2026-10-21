package mcp

import (
	"campusclaw/internal/auth"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

func idOf(r *http.Request) int64 { id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return id }
func fail(w http.ResponseWriter, err error) {
	var input InputError
	switch {
	case errors.Is(err, ErrNotFound):
		auth.Error(w, 404, "not_found")
	case errors.Is(err, ErrRemote):
		auth.JSON(w, 502, map[string]string{"error": "mcp_unavailable", "message": ErrRemote.Error()})
	case errors.Is(err, ErrConflict):
		auth.JSON(w, 409, map[string]string{"error": "configuration_changed", "message": ErrConflict.Error()})
	case errors.As(err, &input):
		auth.JSON(w, 400, map[string]string{"error": "invalid_mcp_configuration", "message": input.Error()})
	default:
		auth.Error(w, 503, "service_unavailable")
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	return true
}

// @Route GET /api/mcp/servers
// @Summary 列出本班MCP服务器，只返回令牌配置布尔值
// @Role teacher
// @Response 200 MCPServers
func (s *Service) Servers(w http.ResponseWriter, r *http.Request) {
	v, e := s.List(r.Context(), *auth.Current(r).ClassID)
	if e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]any{"servers": v})
}

// @Route POST /api/mcp/servers
// @Summary 登记本班MCP服务器（同网服务可使用环境令牌）
// @Role teacher
// @Request MCPServerInput
// @Response 201 MCPServer
// @Error 400 invalid_mcp_configuration / invalid_request_body
func (s *Service) Create(w http.ResponseWriter, r *http.Request) { s.save(w, r, 0, 201) }

// @Route PUT /api/mcp/servers/{id}
// @Summary 修改服务器并断开，auth_token省略时保留旧令牌
// @Role teacher
// @Request MCPServerInput
// @Response 200 MCPServer
// @Error 400 invalid_mcp_configuration / invalid_request_body
// @Error 404 not_found
func (s *Service) Update(w http.ResponseWriter, r *http.Request) {
	if _, e := s.Get(r.Context(), *auth.Current(r).ClassID, idOf(r)); e != nil {
		fail(w, e)
		return
	}
	s.save(w, r, idOf(r), 200)
}
func (s *Service) save(w http.ResponseWriter, r *http.Request, id int64, code int) {
	var in ServerInput
	if !decode(w, r, &in) {
		return
	}
	v, e := s.Save(r.Context(), *auth.Current(r).ClassID, id, in)
	if e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, code, v)
}

// @Route DELETE /api/mcp/servers/{id}
// @Summary 删除本班服务器与缓存，保留审计快照
// @Role teacher
// @Response 200 Status
// @Error 404 not_found
func (s *Service) Remove(w http.ResponseWriter, r *http.Request) {
	if e := s.Delete(r.Context(), *auth.Current(r).ClassID, idOf(r)); e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]string{"status": "ok"})
}

// @Route POST /api/mcp/servers/{id}/connect
// @Summary 握手并发现工具，整体替换缓存；变更工具须重新确认
// @Role teacher
// @Response 200 MCPServer
// @Error 404 not_found
// @Error 409 configuration_changed
// @Error 502 mcp_unavailable
func (s *Service) ConnectHTTP(w http.ResponseWriter, r *http.Request) {
	v, e := s.Connect(r.Context(), *auth.Current(r).ClassID, idOf(r))
	if e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, v)
}

// @Route POST /api/mcp/servers/{id}/disconnect
// @Summary 断开并清空发现缓存，立即停止后续工具执行
// @Role teacher
// @Response 200 Status
// @Error 404 not_found
func (s *Service) DisconnectHTTP(w http.ResponseWriter, r *http.Request) {
	if e := s.Disconnect(r.Context(), *auth.Current(r).ClassID, idOf(r)); e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]string{"status": "ok"})
}

// @Route GET /api/mcp/servers/{id}/tools
// @Summary 查看完整发现说明、参数结构与逐工具批准状态
// @Role teacher
// @Response 200 MCPTools
// @Error 404 not_found
func (s *Service) ToolsHTTP(w http.ResponseWriter, r *http.Request) {
	v, e := s.Tools(r.Context(), *auth.Current(r).ClassID, idOf(r))
	if e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]any{"tools": v})
}

// @Route POST /api/mcp/servers/{id}/tools/approve
// @Summary 重新确认已查看的只读工具声明变更
// @Role teacher
// @Request MCPApproval
// @Response 200 Status
// @Error 400 invalid_mcp_configuration / invalid_request_body
// @Error 404 not_found
func (s *Service) ApproveHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Names []string `json:"names"`
	}
	if !decode(w, r, &in) {
		return
	}
	if e := s.Approve(r.Context(), *auth.Current(r).ClassID, idOf(r), in.Names); e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]string{"status": "ok"})
}

type Audit struct {
	ID             int64     `json:"id"`
	Ref            string    `json:"ref"`
	ServerName     string    `json:"server_name"`
	Status         string    `json:"status"`
	UserID         int64     `json:"user_id"`
	Username       string    `json:"username"`
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"assistant_message_id"`
	Round          int       `json:"round"`
	Text           string    `json:"text"`
	LatencyMS      int64     `json:"latency_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

// @Route GET /api/mcp/tool-calls
// @Summary 本班最近100次工具调用，含用户、会话、轮次、状态和摘要
// @Role teacher
// @Response 200 MCPAudit
func (s *Service) AuditHTTP(w http.ResponseWriter, r *http.Request) {
	rows, e := s.DB.QueryContext(r.Context(), "SELECT a.id,a.tool_ref,a.server_name,a.status,a.user_id,u.username,a.conversation_id,a.assistant_message_id,a.round_no,a.text,a.latency_ms,a.created_at FROM mcp_tool_calls a JOIN users u ON u.id=a.user_id WHERE a.class_id=? ORDER BY a.id DESC LIMIT 100", *auth.Current(r).ClassID)
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []Audit{}
	for rows.Next() {
		var v Audit
		if e = rows.Scan(&v.ID, &v.Ref, &v.ServerName, &v.Status, &v.UserID, &v.Username, &v.ConversationID, &v.MessageID, &v.Round, &v.Text, &v.LatencyMS, &v.CreatedAt); e != nil {
			fail(w, e)
			return
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]any{"tool_calls": out})
}

// @Route GET /api/mcp/tool-changes
// @Summary 本班最近100条工具声明变更，含完整前后声明与确认时间
// @Role teacher
// @Response 200 MCPChanges
func (s *Service) ChangesHTTP(w http.ResponseWriter, r *http.Request) {
	rows, e := s.DB.QueryContext(r.Context(), "SELECT id,server_name,tool_name,previous_json,current_json,approved_at,created_at FROM mcp_tool_changes WHERE class_id=? ORDER BY id DESC LIMIT 100", *auth.Current(r).ClassID)
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var server, name string
		var old, new json.RawMessage
		var approved *time.Time
		var created time.Time
		if e = rows.Scan(&id, &server, &name, &old, &new, &approved, &created); e != nil {
			fail(w, e)
			return
		}
		out = append(out, map[string]any{"id": id, "server_name": server, "tool_name": name, "previous": old, "current": new, "approved_at": approved, "created_at": created})
	}
	if e = rows.Err(); e != nil {
		fail(w, e)
		return
	}
	auth.JSON(w, 200, map[string]any{"changes": out})
}
