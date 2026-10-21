// POST SSE endpoint for conversation questions. Every validation failure —
// auth, media type, JSON shape, question bounds, unknown conversation, busy
// turn or missing prompt — is answered as plain JSON before the event stream
// starts. After the stream starts, failures use exactly one terminal error
// event and never a done event.
package conversation

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/internal/auth"
	"campusclaw/internal/retrieval"
)

// QuestionMaxChars bounds one submitted question in Unicode characters.
const QuestionMaxChars = 500

// streamWriter emits single-line-JSON SSE frames and flushes every frame.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// begin writes the SSE headers and requires Flusher support. Callers must
// only invoke it after every pre-stream JSON error path is exhausted.
func beginStream(w http.ResponseWriter) (*streamWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("response writer does not support flushing")
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-store, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &streamWriter{w: w, flusher: flusher}
	return s, nil
}

// send writes one event frame. JSON escaping keeps every data payload on a
// single line even when the text contains newlines or control characters.
func (s *streamWriter) send(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := s.w.Write([]byte("event: " + event + "\ndata: " + string(data) + "\n\n")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

type questionRequest struct {
	Text    string `json:"text"`
	AgentID *int64 `json:"agent_id,omitempty"`
}

// PostMessage answers POST /api/conversations/{id}/messages with a Bearer-
// protected SSE stream of one guided turn.
// @Route POST /api/conversations/{id}/messages
// @Summary 发送2–500字问题，检索后先meta再delta，落库后done
// @Role class
// @Request Question
// @Response 200 Stream
// @Error 400 invalid_question / invalid_media_type / invalid_request_body
// @Error 404 conversation_not_found
// @Error 409 conversation_busy
// @Error 503 retrieval_unavailable / service_unavailable
func (s *Service) PostMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.Current(r)
	var body questionRequest
	if !decodeQuestion(w, r, &body) {
		return
	}
	question := strings.TrimSpace(body.Text)
	if n := utf8.RuneCountInString(question); n < 2 || n > QuestionMaxChars {
		auth.Error(w, 400, "invalid_question")
		return
	}
	conversationID := r.PathValue("id")
	// Same-shape 404 for foreign, cross-class or unknown conversations before
	// any row is created.
	if _, err := s.loadOwned(r.Context(), conversationID, user.ID, *user.ClassID); err != nil {
		writeReadError(w, err)
		return
	}
	if s.Orch == nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}

	var stream *streamWriter
	var assistantMessageID string
	outcome, err := s.Orch.Run(r.Context(), s, conversationID, user.ID, *user.ClassID, question,
		func(turn Turn) error {
			if stream == nil {
				var err error
				stream, err = beginStream(w)
				if err != nil {
					return err
				}
			}
			sw := stream
			assistantMessageID = turn.AssistantMessageID
			var skill any
			if turn.Skill != nil {
				skill = map[string]any{"id": turn.Skill.ID, "name": turn.Skill.Name}
			}
			payload := map[string]any{
				"status":               "ok",
				"skill":                skill,
				"citations":            turn.Citations,
				"conversation_id":      turn.ConversationID,
				"user_message_id":      turn.UserMessageID,
				"assistant_message_id": turn.AssistantMessageID,
			}
			if len(turn.ToolCalls) > 0 {
				payload["tool_calls"] = turn.ToolCalls
			}
			return sw.send("meta", payload)
		},
		func(delta string) error {
			if stream == nil {
				return nil
			}
			return stream.send("delta", map[string]string{"text": delta})
		})
	if err != nil {
		writeTurnError(w, err)
		return
	}
	// Post-start terminal events: citation/done only after the committing
	// transaction; exactly one error event without done on failure.
	if stream == nil {
		return
	}
	switch outcome.Status {
	case "completed":
		_ = stream.send("done", map[string]any{
			"assistant_message_id": assistantMessageID,
			"status":               "completed",
			"citations":            outcome.Citations,
		})
	default:
		_ = stream.send("error", map[string]any{
			"code":      outcome.ErrorCode,
			"message":   safeErrorMessage(outcome.ErrorCode),
			"retryable": retryableCode(outcome.ErrorCode),
		})
	}
}

func writeTurnError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, retrieval.ErrUnavailable):
		auth.Error(w, 503, "retrieval_unavailable")
	case errors.Is(err, ErrNotFound):
		auth.Error(w, 404, "conversation_not_found")
	case errors.Is(err, ErrBusy):
		auth.Error(w, 409, "conversation_busy")
	case errors.Is(err, ErrNoPrompt):
		auth.Error(w, 409, "prompt_not_configured")
	default:
		auth.Error(w, 503, "service_unavailable")
	}
}

func safeErrorMessage(code string) string {
	switch code {
	case CodeRetrievalUnavailable:
		return "资料检索暂时不可用，请稍后重试"
	case CodeModelTimeout:
		return "回答生成超时，请重试"
	case CodeModelUnavailable:
		return "回答服务暂时不可用，请稍后重试"
	case CodeOutputLimit:
		return "回答超过长度限制，请缩小问题范围后重试"
	case CodeUnsupportedCitation:
		return "回答引用校验未通过，请重试"
	case CodeMissingDisclosure:
		return "回答未说明本班资料未命中，请重试"
	case CodeGuidedViolation:
		return "回答未通过解题引导检查，请重试"
	case CodeClientCancelled:
		return "已停止生成"
	default:
		return "服务暂时不可用，请稍后重试"
	}
}

func retryableCode(code string) bool {
	switch code {
	case CodeOutputLimit, CodeGuidedViolation, CodeUnsupportedCitation, CodeMissingDisclosure, CodeInternal:
		return false
	}
	return true
}

// decodeQuestion applies media-type and strict JSON decoding rules before any
// stream starts. Unknown fields — including forged class or role overrides —
// are rejected.
func decodeQuestion(w http.ResponseWriter, r *http.Request, target any) bool {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			auth.Error(w, 400, "invalid_media_type")
			return false
		}
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	return true
}
