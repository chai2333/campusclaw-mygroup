package conversation

import (
	"errors"
	"net/http"

	"campusclaw/internal/auth"
	"campusclaw/internal/tutor"
)

// Student authorizes authenticated students with a class; teachers get 403
// because conversation management is a student-only surface.
func Student(authSvc *auth.Service, next http.HandlerFunc) http.HandlerFunc {
	return authSvc.Class(next)
}

// @Route POST /api/conversations
// @Summary 创建自己的会话并固定本班已启用智能体
// @Role class
// @Request ConversationInput
// @Response 201 Conversation
// @Error 400 no_available_agents / invalid_request_body
// @Error 404 not_found
func (s *Service) CreateConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.Current(r)
	var body struct {
		AgentID int64 `json:"agent_id"`
	}
	if !decodeQuestion(w, r, &body) {
		return
	}
	c, err := s.Create(r.Context(), user.ID, *user.ClassID, body.AgentID)
	if err != nil {
		if errors.Is(err, tutor.ErrNotFound) {
			auth.Error(w, 404, "not_found")
			return
		}
		if errors.Is(err, tutor.ErrNoAgents) {
			auth.Error(w, 400, "no_available_agents")
			return
		}
		auth.Error(w, 503, "service_unavailable")
		return
	}
	auth.JSON(w, 201, c)
}

// @Route GET /api/conversations
// @Summary 列出本人本班会话
// @Role class
// @Response 200 Conversations
func (s *Service) ListConversations(w http.ResponseWriter, r *http.Request) {
	user := auth.Current(r)
	list, err := s.List(r.Context(), user.ID, *user.ClassID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	auth.JSON(w, 200, map[string]any{"conversations": list})
}

// @Route GET /api/conversations/{id}
// @Summary 读取自己的会话、固定智能体和历史
// @Role class
// @Response 200 ConversationDetail
// @Error 404 conversation_not_found
func (s *Service) GetConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.Current(r)
	c, messages, err := s.LoadDetail(r.Context(), r.PathValue("id"), user.ID, *user.ClassID)
	if err != nil {
		writeReadError(w, err)
		return
	}
	auth.JSON(w, 200, map[string]any{"conversation": c, "messages": messages})
}

func writeReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		auth.Error(w, 404, "conversation_not_found")
		return
	}
	auth.Error(w, 503, "service_unavailable")
}
