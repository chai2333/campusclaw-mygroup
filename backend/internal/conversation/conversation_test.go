package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"campusclaw/internal/assistant"
	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/db"
)

func testEnv(t *testing.T) (*sql.DB, *Service) {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Wait(ctx, conn); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
		"TRUNCATE TABLE tutor_agent_materials", "TRUNCATE TABLE tutor_agent_skills", "TRUNCATE TABLE tutor_agents", "TRUNCATE TABLE skills",
		"TRUNCATE TABLE message_citations", "TRUNCATE TABLE conversation_messages",
		"TRUNCATE TABLE conversations", "TRUNCATE TABLE class_skills",
		"TRUNCATE TABLE assistant_prompts", "TRUNCATE TABLE knowledge_chunks",
		"TRUNCATE TABLE knowledge_entries", "TRUNCATE TABLE materials",
		"TRUNCATE TABLE sessions", "TRUNCATE TABLE users", "TRUNCATE TABLE classes",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		if _, err := conn.ExecContext(context.Background(), stmt); err != nil {
			conn.Close()
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}
	if err := db.Seed(ctx, conn, "teacher-fixture-pw", "student-fixture-pw", t.TempDir()); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	// The fake retriever's sample source is explicitly bound, just like real sources.
	if _, err := conn.Exec(`INSERT INTO materials(id,class_id,uploader_id,original_filename,title,storage_key,status)
 SELECT 'mat-1',class_id,id,'材料一.txt','材料一','mat-1','ready' FROM users WHERE username='a-teacher'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO tutor_agent_materials(agent_id,material_id,class_id)
 SELECT id,'mat-1',class_id FROM tutor_agents WHERE seed_key='legacy-default' AND class_id=(SELECT id FROM classes WHERE slug='a')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, &Service{DB: conn}
}

func ids(t *testing.T, conn *sql.DB) (classA, classB, studentA, studentB, crossStudent, teacher int64) {
	t.Helper()
	for _, pair := range []struct {
		slug   string
		target *int64
	}{{"a", &classA}, {"b", &classB}} {
		if err := conn.QueryRow("SELECT id FROM classes WHERE slug=?", pair.slug).Scan(pair.target); err != nil {
			t.Fatal(err)
		}
	}
	for _, triple := range []struct {
		name   string
		class  int64
		target *int64
	}{{"a-student", classA, &studentA}, {"b-student", classB, &studentB}} {
		if err := conn.QueryRow("SELECT id FROM users WHERE username=?", triple.name).Scan(triple.target); err != nil {
			t.Fatal(err)
		}
	}
	// Another student inside class A and a teacher for matrix checks.
	if _, err := conn.Exec("INSERT INTO users(username,password_hash,role,class_id) VALUES('a-student-2','$2a$12$xxx','student',?)", classA); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT id FROM users WHERE username='a-student-2'").Scan(&crossStudent); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT id FROM users WHERE username='a-teacher'").Scan(&teacher); err != nil {
		t.Fatal(err)
	}
	return classA, classB, studentA, studentB, crossStudent, teacher
}

func TestConversationOwnershipMatrix(t *testing.T) {
	conn, svc := testEnv(t)
	_, _, studentA, _, sameClassOther, _ := ids(t, conn)
	ctx := context.Background()

	created, err := svc.Create(ctx, studentA, classAOf(t, conn, studentA))
	if err != nil {
		t.Fatal(err)
	}
	if created.Title != NeutralTitle || len(created.ID) < 16 {
		t.Fatalf("unexpected conversation: %+v", created)
	}
	if got, _ := svc.List(ctx, studentA); len(got) != 1 || got[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", got)
	}

	// Same-class other owner, cross-class and unknown ids share the 404 shape.
	for _, tc := range []struct {
		name    string
		owner   int64
		classID int64
	}{
		{"same-class other owner", sameClassOther, classAOf(t, conn, sameClassOther)},
		{"cross-class student", studentBOf(t, conn), classBOf(t, conn)},
	} {
		if _, _, err := svc.LoadDetail(ctx, created.ID, tc.owner, tc.classID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: expected ErrNotFound, got %v", tc.name, err)
		}
	}
	if _, _, err := svc.LoadDetail(ctx, "does-not-exist", studentA, classAOf(t, conn, studentA)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: expected ErrNotFound, got %v", err)
	}

	// Owner loads with citation snapshot support and ordered messages.
	detail, messages, err := svc.LoadDetail(ctx, created.ID, studentA, classAOf(t, conn, studentA))
	if err != nil || detail.ID != created.ID || messages == nil {
		t.Fatalf("owner load failed: %v %v", detail, err)
	}
}

func classAOf(t *testing.T, conn *sql.DB, _ int64) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow("SELECT id FROM classes WHERE slug='a'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func classBOf(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow("SELECT id FROM classes WHERE slug='b'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func studentBOf(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow("SELECT id FROM users WHERE username='b-student'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStartTurnTransaction(t *testing.T) {
	conn, svc := testEnv(t)
	classA, _, studentA, _, _, _ := ids(t, conn)
	assistantSvc := &assistant.Service{DB: conn}
	ctx := context.Background()

	conv, err := svc.Create(ctx, studentA, classA)
	if err != nil {
		t.Fatal(err)
	}
	// Empty agent prompt uses fallback, and still creates a real locked turn.
	fallback, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "问题", "m")
	if err != nil || fallback.PromptBody == "" {
		t.Fatalf("fallback role missing: %+v %v", fallback, err)
	}
	if err := svc.FailTurn(ctx, fallback, "", "client_cancelled", true); err != nil {
		t.Fatal(err)
	}

	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "教师提示词"); err != nil {
		t.Fatal(err)
	}
	conv, _ = svc.Create(ctx, studentA, classA)
	longQuestion := strings.Repeat("题", 120)
	turn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, longQuestion, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if turn.PromptBody != "教师提示词" || turn.PromptVersion != 1 || !turn.SkillEnabled {
		t.Fatalf("snapshot mismatch: %+v", turn)
	}
	var role, content, status string
	if err := conn.QueryRow("SELECT role,content,status FROM conversation_messages WHERE id=?", turn.UserMessageID).Scan(&role, &content, &status); err != nil {
		t.Fatal(err)
	}
	if role != "user" || status != "completed" || !utf8.ValidString(content) {
		t.Fatalf("user row wrong: %s/%s/%s", role, content, status)
	}
	if err := conn.QueryRow("SELECT role,status FROM conversation_messages WHERE id=?", turn.AssistantMessageID).Scan(&role, &status); err != nil {
		t.Fatal(err)
	}
	if role != "assistant" || status != "streaming" {
		t.Fatalf("assistant row wrong: %s/%s", role, status)
	}
	var active sql.NullString
	if err := conn.QueryRow("SELECT active_message_id FROM conversations WHERE id=?", conv.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active.Valid || active.String != turn.AssistantMessageID {
		t.Fatalf("active lock not set: %v", active)
	}
	var title string
	if err := conn.QueryRow("SELECT title FROM conversations WHERE id=?", conv.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if runeCount(title) != TitleMaxChars {
		t.Fatalf("title should be bounded to %d runes, got %d", TitleMaxChars, runeCount(title))
	}

	// A second start while a turn is active must be rejected.
	if _, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "第二个问题", "test-model"); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func TestTurnTerminalTransactions(t *testing.T) {
	conn, svc := testEnv(t)
	classA, _, studentA, _, _, _ := ids(t, conn)
	assistantSvc := &assistant.Service{DB: conn}
	ctx := context.Background()

	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "提示词"); err != nil {
		t.Fatal(err)
	}
	conv, _ := svc.Create(ctx, studentA, classA)
	turn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "第一问", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	citations := []Citation{
		{Ordinal: 1, MaterialID: "mat", MaterialTitle: "材料", ChunkID: 11, ChunkIndex: 0, CharStart: 0, CharEnd: 8, Excerpt: "摘录一"},
		{Ordinal: 2, MaterialID: "mat", MaterialTitle: "材料", ChunkID: 12, ChunkIndex: 1, CharStart: 9, CharEnd: 20, Excerpt: "摘录二"},
	}
	if err := svc.CompleteTurn(ctx, turn, "回答内容", citations); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := conn.QueryRow("SELECT status FROM conversation_messages WHERE id=?", turn.AssistantMessageID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("status %s", status)
	}
	var active sql.NullString
	if err := conn.QueryRow("SELECT active_message_id FROM conversations WHERE id=?", conv.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active.Valid {
		t.Fatalf("lock not cleared: %v", active)
	}
	var citationCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM message_citations WHERE assistant_message_id=?", turn.AssistantMessageID).Scan(&citationCount); err != nil {
		t.Fatal(err)
	}
	if citationCount != 2 {
		t.Fatalf("citations not saved: %d", citationCount)
	}
	// Stale completion must not overwrite.
	if err := svc.CompleteTurn(ctx, turn, "覆盖内容", nil); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("expected ErrStaleTurn, got %v", err)
	}
	var content string
	if err := conn.QueryRow("SELECT content FROM conversation_messages WHERE id=?", turn.AssistantMessageID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "回答内容" {
		t.Fatalf("stale completion overwrote answer: %q", content)
	}

	// Failed turn stores error code and clears lock.
	conv2, _ := svc.Create(ctx, studentA, classA)
	turn2, _ := svc.StartTurn(ctx, conv2.ID, studentA, classA, "第二问", "test-model")
	if err := svc.FailTurn(ctx, turn2, "部分输出", "model_timeout", false); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT status,error_code FROM conversation_messages WHERE id=?", turn2.AssistantMessageID).Scan(&status, new(any)); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("failed status wrong: %s", status)
	}

	// Cancelled turn and mismatched conversation id are rejected.
	conv3, _ := svc.Create(ctx, studentA, classA)
	turn3, _ := svc.StartTurn(ctx, conv3.ID, studentA, classA, "第三问", "test-model")
	wrong := turn3
	wrong.ConversationID = conv.ID
	if err := svc.CompleteTurn(ctx, wrong, "错位回答", citations); !errors.Is(err, ErrStaleTurn) {
		t.Fatalf("expected ErrStaleTurn for mismatched conversation, got %v", err)
	}
	if err := svc.FailTurn(ctx, turn3, "", "client_cancelled", true); err != nil {
		t.Fatal(err)
	}
	var cancelled string
	if err := conn.QueryRow("SELECT status FROM conversation_messages WHERE id=?", turn3.AssistantMessageID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled != "cancelled" {
		t.Fatalf("cancelled status wrong: %s", cancelled)
	}
}

func TestHistoryBounds(t *testing.T) {
	conn, svc := testEnv(t)
	classA, _, studentA, _, _, _ := ids(t, conn)
	assistantSvc := &assistant.Service{DB: conn}
	ctx := context.Background()

	if _, err := assistantSvc.SavePrompt(ctx, classA, 1, "提示词"); err != nil {
		t.Fatal(err)
	}
	conv, _ := svc.Create(ctx, studentA, classA)
	// Three completed turns; failed/cancelled turns live in conv2 below.
	for i := 1; i <= 3; i++ {
		turn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, "问题", "test-model")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.CompleteTurn(ctx, turn, "回答"+string(rune('0'+i)), nil); err != nil {
			t.Fatal(err)
		}
	}

	history, err := svc.History(ctx, conv.ID, 12, 24000)
	if err != nil {
		t.Fatal(err)
	}
	// 3 completed user + 3 completed assistant messages, chronological.
	if len(history) != 6 {
		t.Fatalf("expected 6 history messages, got %d: %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "问题" {
		t.Fatalf("history not chronological: %+v", history[0])
	}

	bounded, err := svc.History(ctx, conv.ID, 2, 24000)
	if err != nil {
		t.Fatal(err)
	}
	// The latest two completed rows are the fifth user question and the third
	// assistant answer, restored to chronological order.
	if len(bounded) != 6 || bounded[0].Role != "user" || bounded[5].Content != "回答3" {
		t.Fatalf("message bound wrong: %+v", bounded)
	}

	longTurn, err := svc.StartTurn(ctx, conv.ID, studentA, classA, strings.Repeat("问", 650), "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.CompleteTurn(ctx, longTurn, strings.Repeat("答", 650), nil); err != nil {
		t.Fatal(err)
	}
	charBounded, err := svc.History(ctx, conv.ID, 12, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(charBounded) != 6 || utf8.RuneCountInString(charBounded[4].Content) != 500 || utf8.RuneCountInString(charBounded[5].Content) != 500 {
		t.Fatalf("fixed six-message/500-char window wrong: %+v", charBounded)
	}
	for _, m := range charBounded {
		if utf8.RuneCountInString(m.Content) > 500 {
			t.Fatal("message over 500")
		}
	}

	// Failed and cancelled turns: user questions stay in history (their rows
	// are completed), but assistant text must never enter model context.
	conv2, _ := svc.Create(ctx, studentA, classA)
	turn, _ := svc.StartTurn(ctx, conv2.ID, studentA, classA, "失败问", "test-model")
	if err := svc.FailTurn(ctx, turn, "失败输出", "model_unavailable", false); err != nil {
		t.Fatal(err)
	}
	turn, _ = svc.StartTurn(ctx, conv2.ID, studentA, classA, "取消问", "test-model")
	if err := svc.FailTurn(ctx, turn, "取消输出", "client_cancelled", true); err != nil {
		t.Fatal(err)
	}
	terminalHistory, err := svc.History(ctx, conv2.ID, 12, 24000)
	if err != nil {
		t.Fatal(err)
	}
	if len(terminalHistory) != 2 {
		t.Fatalf("expected 2 user questions in terminal history, got %d: %+v", len(terminalHistory), terminalHistory)
	}
	for _, m := range terminalHistory {
		if m.Role != "user" {
			t.Fatalf("only user rows should enter history: %+v", m)
		}
		if strings.Contains(m.Content, "失败输出") || strings.Contains(m.Content, "取消输出") {
			t.Fatalf("terminal-failed text entered history: %+v", m)
		}
	}
}

func TestConversationEndpoints(t *testing.T) {
	conn, svc := testEnv(t)
	_, _, _, _, _, _ = ids(t, conn)
	cfg := config.Config{Environment: "test", SessionSecret: "test-session-secret-key-32bytes!", SessionTTL: 28800, MaxFailures: 5, WindowSeconds: 300, LockSeconds: 300}
	authSvc, err := auth.New(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", authSvc.Login)
	mux.HandleFunc("POST /api/conversations", Student(authSvc, svc.CreateConversation))
	mux.HandleFunc("GET /api/conversations", Student(authSvc, svc.ListConversations))
	mux.HandleFunc("GET /api/conversations/{id}", Student(authSvc, svc.GetConversation))
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
	student := login("a-student", "student-fixture-pw")
	teacher := login("a-teacher", "teacher-fixture-pw")

	resp, err := teacher.Get(ts.URL + "/api/conversations")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("teacher own list should 200: %d", resp.StatusCode)
	}

	var agentID int64
	if err := conn.QueryRow("SELECT id FROM tutor_agents WHERE class_id=(SELECT id FROM classes WHERE slug='a') AND seed_key='legacy-default'").Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]int64{"agent_id": agentID})
	resp, err = student.Post(ts.URL+"/api/conversations", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("create should 201: %d", resp.StatusCode)
	}
	var created Conversation
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if created.Title != NeutralTitle {
		t.Fatalf("unexpected title: %q", created.Title)
	}

	resp, err = student.Get(ts.URL + "/api/conversations/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("detail should 200: %d", resp.StatusCode)
	}
	var detail struct {
		Conversation Conversation `json:"conversation"`
		Messages     []Message    `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Messages == nil {
		t.Fatal("messages must serialize as an array")
	}

	otherStudent := login("b-student", "student-fixture-pw")
	resp2, err := otherStudent.Get(ts.URL + "/api/conversations/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("cross-owner should 404: %d", resp2.StatusCode)
	}
	var envelope map[string]string
	if err := json.NewDecoder(resp2.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["error"] != "conversation_not_found" {
		t.Fatalf("unexpected 404 envelope: %v", envelope)
	}
}

type fixedBearer struct{ token string }

func (b *fixedBearer) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}
