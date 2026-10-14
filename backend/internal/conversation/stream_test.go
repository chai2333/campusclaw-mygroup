package conversation

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/assistant"
	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/dialogue"
	"campusclaw/internal/dialogue/dialoguetest"
	"campusclaw/internal/retrieval/retrievaltest"
)

type sseFrame struct {
	Event string
	Data  string
}

// parseSSE parses raw SSE bytes into ordered frames. Every data payload must
// stay on a single line.
func parseSSE(t *testing.T, body io.Reader) []sseFrame {
	t.Helper()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var frames []sseFrame
	current := sseFrame{}
	flush := func() {
		if current.Event != "" {
			frames = append(frames, current)
		}
		current = sseFrame{}
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			current.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			if strings.Contains(data, "\n") {
				t.Fatalf("data payload is not single-line: %q", data)
			}
			current.Data = data
		case line == "":
			flush()
		default:
			t.Fatalf("unexpected SSE line: %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}
	flush()
	return frames
}

func streamEnv(t *testing.T) (*httptest.Server, *Service, *dialoguetest.FakeGateway, *retrievaltest.FakeRetriever, *assistant.Service, int64, int64, func(user, pass string) *http.Client) {
	t.Helper()
	conn, svc := testEnv(t)
	classA, _, studentA, _, _, _ := ids(t, conn)
	assistantSvc := &assistant.Service{DB: conn}
	retriever := &retrievaltest.FakeRetriever{}
	gateway := &dialoguetest.FakeGateway{}
	svc.Orch = &Orchestrator{
		Retriever:          retriever,
		Gateway:            gateway,
		Model:              "test-model",
		MaxTokens:          2048,
		HistoryMaxMessages: 12,
		HistoryMaxChars:    24000,
		Timeout:            30 * time.Second,
	}
	cfg := config.Config{Environment: "test", SessionSecret: "test-session-secret-key-32bytes!", SessionTTL: 28800, MaxFailures: 5, WindowSeconds: 300, LockSeconds: 300}
	authSvc, err := auth.New(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", authSvc.Login)
	mux.HandleFunc("POST /api/conversations/{id}/messages", Student(authSvc, svc.PostMessage))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	login := func(user, pass string) *http.Client {
		client := &http.Client{}
		resp, err := client.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("login %s failed: %d", user, resp.StatusCode)
		}
		var result auth.LoginResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		client.Transport = &fixedBearer{token: result.AccessToken}
		return client
	}
	return ts, svc, gateway, retriever, assistantSvc, classA, studentA, login
}

// TestMessageStreamContract drives the happy path and asserts the exact wire
// contract: headers, event order, single-line JSON frames and one done.
func TestMessageStreamContract(t *testing.T) {
	ts, svc, gateway, retriever, assistantSvc, classA, studentA, login := streamEnv(t)
	ctx := context.Background()
	student := login("a-student", "student-fixture-pw")
	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "教师提示词"); err != nil {
		t.Fatal(err)
	}
	conv, err := svc.Create(ctx, studentA, classA)
	if err != nil {
		t.Fatal(err)
	}
	retriever.Hits = sampleHits(classA)
	gateway.Deltas = []string{"先解释概念[1]。\n换行示例", "再看练习[2]。"}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/messages", strings.NewReader(`{"text":"光的折射是什么？"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := student.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 SSE, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("cache-control %q", cc)
	}
	frames := parseSSE(t, resp.Body)
	wantOrder := []string{"meta", "delta", "delta", "done"}
	if len(frames) != len(wantOrder) {
		t.Fatalf("expected %d frames, got %d: %+v", len(wantOrder), len(frames), frames)
	}
	var startPayload, donePayload map[string]any
	for i, f := range frames {
		if f.Event != wantOrder[i] {
			t.Fatalf("frame %d event %s, want %s (frames %+v)", i, f.Event, wantOrder[i], frames)
		}
		if f.Data == "" {
			t.Fatalf("frame %s has empty data", f.Event)
		}
	}
	if err := json.Unmarshal([]byte(frames[0].Data), &startPayload); err != nil {
		t.Fatal(err)
	}
	if startPayload["conversation_id"] != conv.ID {
		t.Fatalf("start payload wrong: %v", startPayload)
	}
	assistantID := startPayload["assistant_message_id"].(string)
	if err := json.Unmarshal([]byte(frames[len(frames)-1].Data), &donePayload); err != nil {
		t.Fatal(err)
	}
	if donePayload["status"] != "completed" || donePayload["assistant_message_id"] != assistantID {
		t.Fatalf("done payload wrong: %v", donePayload)
	}
	citations, ok := donePayload["citations"].([]any)
	if !ok || len(citations) != 2 {
		t.Fatalf("done citations wrong: %v", donePayload["citations"])
	}
	// Delta text round-trips escaped newlines.
	var deltaPayload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(frames[1].Data), &deltaPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(deltaPayload.Text, "\n") {
		t.Fatalf("newline delta lost: %q", deltaPayload.Text)
	}
	// The turn persisted as completed with the two citations.
	var status string
	if err := svc.DB.QueryRow("SELECT status FROM conversation_messages WHERE id=?", assistantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("assistant status %s", status)
	}
	var citationCount int
	if err := svc.DB.QueryRow("SELECT COUNT(*) FROM message_citations WHERE assistant_message_id=?", assistantID).Scan(&citationCount); err != nil {
		t.Fatal(err)
	}
	if citationCount != 2 {
		t.Fatalf("citations persisted: %d", citationCount)
	}
}

// TestMessageStreamPreStreamErrors verifies every pre-stream failure stays
// plain JSON before SSE starts and creates no messages.
func TestMessageStreamPreStreamErrors(t *testing.T) {
	ts, svc, gateway, retriever, assistantSvc, classA, studentA, login := streamEnv(t)
	ctx := context.Background()
	student := login("a-student", "student-fixture-pw")
	teacher := login("a-teacher", "teacher-fixture-pw")
	other := login("b-student", "student-fixture-pw")
	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "教师提示词"); err != nil {
		t.Fatal(err)
	}
	conv, err := svc.Create(ctx, studentA, classA)
	if err != nil {
		t.Fatal(err)
	}
	retriever.Hits = sampleHits(classA)

	post := func(client *http.Client, conversationID, body, contentType string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/conversations/"+conversationID+"/messages", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	checkJSON := func(name string, resp *http.Response, wantStatus int, wantError string) {
		t.Helper()
		defer resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s: status %d, want %d", name, resp.StatusCode, wantStatus)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("%s: content type %q must be JSON", name, ct)
		}
		var envelope map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if envelope["error"] != wantError {
			t.Fatalf("%s: error %v", name, envelope)
		}
	}

	checkJSON("teacher foreign owner", post(teacher, conv.ID, `{"text":"问题"}`, ""), 404, "conversation_not_found")
	checkJSON("foreign", post(other, conv.ID, `{"text":"问题"}`, ""), 404, "conversation_not_found")
	checkJSON("unknown conversation", post(student, "does-not-exist", `{"text":"问题"}`, ""), 404, "conversation_not_found")
	checkJSON("media type", post(student, conv.ID, `{"text":"问题"}`, "text/plain"), 400, "invalid_media_type")
	checkJSON("malformed json", post(student, conv.ID, `{"text":`, ""), 400, "invalid_request_body")
	checkJSON("extra json", post(student, conv.ID, `{"text":"问题"}{"more":1}`, ""), 400, "invalid_request_body")
	checkJSON("unknown field", post(student, conv.ID, `{"text":"问题","class_id":2}`, ""), 400, "invalid_request_body")
	checkJSON("blank", post(student, conv.ID, `{"text":"   "}`, ""), 400, "invalid_question")
	checkJSON("oversized", post(student, conv.ID, `{"text":"`+strings.Repeat("问", 8001)+`"}`, ""), 400, "invalid_question")

	// A short single-character question is rejected before creating rows.
	checkJSON("one character", post(student, conv.ID, `{"text":"问"}`, ""), 400, "invalid_question")
	// Busy turn: an active generation rejects the second send as JSON 409.
	turn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "进行中的问题", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON("busy", post(student, conv.ID, `{"text":"第二个问题"}`, ""), 409, "conversation_busy")
	var count int
	if err := svc.DB.QueryRow("SELECT COUNT(*) FROM conversation_messages WHERE conversation_id=?", conv.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("busy request created rows: %d", count)
	}
	if err := svc.FailTurn(ctx, turn, "", "client_cancelled", true); err != nil {
		t.Fatal(err)
	}
	if gateway.Calls != 0 {
		t.Fatalf("gateway must never run in pre-stream error tests, got %d", gateway.Calls)
	}
}

// TestMessageStreamErrorEvent verifies a post-start failure emits exactly one
// error event and never a done event.
func TestMessageStreamErrorEvent(t *testing.T) {
	ts, svc, gateway, retriever, assistantSvc, classA, studentA, login := streamEnv(t)
	ctx := context.Background()
	student := login("a-student", "student-fixture-pw")
	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "教师提示词"); err != nil {
		t.Fatal(err)
	}
	conv, _ := svc.Create(ctx, studentA, classA)
	retriever.Hits = sampleHits(classA)
	gateway.Err = dialogue.ErrUnavailable

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/messages", strings.NewReader(`{"text":"问题"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := student.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := parseSSE(t, resp.Body)
	if len(frames) != 2 || frames[0].Event != "meta" || frames[1].Event != "error" {
		t.Fatalf("expected meta+error, got %+v", frames)
	}
	var errorPayload struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal([]byte(frames[1].Data), &errorPayload); err != nil {
		t.Fatal(err)
	}
	if errorPayload.Code != CodeModelUnavailable || !errorPayload.Retryable || errorPayload.Message == "" {
		t.Fatalf("error payload wrong: %+v", errorPayload)
	}
	status, code := messageState(t, svc, turn2Assistant(t, svc, conv.ID))
	if status != "failed" || code != CodeModelUnavailable {
		t.Fatalf("failed turn not persisted: %s/%s", status, code)
	}
}

// TestMessageStreamClientDisconnect verifies a browser disconnect cancels the
// downstream generation and persists cancelled without a background success.
func TestMessageStreamClientDisconnect(t *testing.T) {
	ts, svc, gateway, retriever, assistantSvc, classA, studentA, login := streamEnv(t)
	ctx := context.Background()
	student := login("a-student", "student-fixture-pw")
	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "教师提示词"); err != nil {
		t.Fatal(err)
	}
	conv, _ := svc.Create(ctx, studentA, classA)
	retriever.Hits = sampleHits(classA)
	gateway.Delay = 80 * time.Millisecond
	gateway.Deltas = []string{"片段一", "片段二"}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/messages", strings.NewReader(`{"text":"问题"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	cancelCtx, cancel := context.WithCancel(ctx)
	req = req.WithContext(cancelCtx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	resp, err := student.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	// Wait for the terminal state through the detached persistence path.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var status string
		if err := svc.DB.QueryRow("SELECT status FROM conversation_messages WHERE conversation_id=? AND role='assistant' ORDER BY sequence_no DESC LIMIT 1", conv.ID).Scan(&status); err == nil && status == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("assistant message never became cancelled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gateway.DeltaCalls > 1 {
		t.Fatalf("downstream must observe cancellation promptly: %d deltas", gateway.DeltaCalls)
	}
	// The conversation accepts a later turn after the terminal state.
	turn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "再来一问", "test-model")
	if err != nil {
		t.Fatalf("conversation should accept a later turn: %v", err)
	}
	if err := svc.FailTurn(ctx, turn, "", "client_cancelled", true); err != nil {
		t.Fatal(err)
	}
}
