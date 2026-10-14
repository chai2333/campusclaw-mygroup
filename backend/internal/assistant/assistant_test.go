package assistant

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/db"
)

func testEnv(t *testing.T) (*sql.DB, config.Config, *auth.Service) {
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
	cfg := config.Config{Environment: "test", SessionSecret: "test-session-secret-key-32bytes!", SessionTTL: 28800, MaxFailures: 5, WindowSeconds: 300, LockSeconds: 300}
	authSvc, err := auth.New(conn, cfg)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, cfg, authSvc
}

func fixtureIDs(t *testing.T, conn *sql.DB) (classID, teacherID, studentID int64) {
	t.Helper()
	if err := conn.QueryRow("SELECT id FROM classes WHERE slug='a'").Scan(&classID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT id FROM users WHERE username='a-teacher'").Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT id FROM users WHERE username='a-student'").Scan(&studentID); err != nil {
		t.Fatal(err)
	}
	return classID, teacherID, studentID
}

func TestPromptSaveFirstUpdateConcurrentAndValidation(t *testing.T) {
	conn, _, _ := testEnv(t)
	classID, teacherID, _ := fixtureIDs(t, conn)
	svc := &Service{DB: conn}
	ctx := context.Background()

	version, err := svc.SavePrompt(ctx, classID, teacherID, "第一版提示词")
	if err != nil || version != 1 {
		t.Fatalf("first save: version=%d err=%v", version, err)
	}
	version, err = svc.SavePrompt(ctx, classID, teacherID, "第二版提示词")
	if err != nil || version != 2 {
		t.Fatalf("second save: version=%d err=%v", version, err)
	}

	const concurrent = 5
	var wg sync.WaitGroup
	versions := make([]int64, concurrent)
	errs := make([]error, concurrent)
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			versions[i], errs[i] = svc.SavePrompt(ctx, classID, teacherID, "并发提示词")
		}(i)
	}
	wg.Wait()
	seen := map[int64]bool{}
	for i := range versions {
		if errs[i] != nil {
			t.Fatalf("concurrent save %d: %v", i, errs[i])
		}
		if seen[versions[i]] {
			t.Fatalf("duplicate concurrent version %d", versions[i])
		}
		seen[versions[i]] = true
	}
	var final int64
	if err := conn.QueryRow("SELECT version FROM assistant_prompts WHERE class_id=?", classID).Scan(&final); err != nil {
		t.Fatal(err)
	}
	if final != int64(2+concurrent) {
		t.Fatalf("final version %d, want %d", final, 2+concurrent)
	}

	for _, bad := range []string{"", "   \n\t ", strings.Repeat("字", MaxPromptChars+1), string([]byte{0xff, 0xfe})} {
		if _, err := svc.SavePrompt(ctx, classID, teacherID, bad); err == nil {
			t.Fatalf("invalid prompt accepted: %q", bad)
		}
	}
	var body string
	var unchanged int64
	if err := conn.QueryRow("SELECT body,version FROM assistant_prompts WHERE class_id=?", classID).Scan(&body, &unchanged); err != nil {
		t.Fatal(err)
	}
	if body != "并发提示词" || unchanged != final {
		t.Fatalf("rejected saves modified state: body=%q version=%d", body, unchanged)
	}
}

func TestPromptRollbackOnDatabaseFailure(t *testing.T) {
	conn, _, _ := testEnv(t)
	classID, teacherID, _ := fixtureIDs(t, conn)
	svc := &Service{DB: conn}
	ctx := context.Background()
	if _, err := svc.SavePrompt(ctx, classID, teacherID, "初始内容"); err != nil {
		t.Fatal(err)
	}
	broken := &Service{DB: conn}
	conn.Close() // pool closed; further writes fail at the driver level
	if _, err := broken.SavePrompt(ctx, classID, teacherID, "不应写入"); err == nil {
		t.Fatal("write on closed database must fail")
	}
	reopened, err := sql.Open("mysql", os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Skip("cannot reopen database")
	}
	defer reopened.Close()
	var body string
	var version int64
	if err := reopened.QueryRow("SELECT body,version FROM assistant_prompts WHERE class_id=?", classID).Scan(&body, &version); err != nil {
		t.Fatal(err)
	}
	if body != "初始内容" || version != 1 {
		t.Fatalf("failed write partially applied: body=%q version=%d", body, version)
	}
}

func TestSkillRepository(t *testing.T) {
	conn, _, _ := testEnv(t)
	classID, teacherID, _ := fixtureIDs(t, conn)
	svc := &Service{DB: conn}
	ctx := context.Background()

	state, err := svc.LoadSkill(ctx, classID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.SkillKey != SkillKey {
		t.Fatalf("unexpected default skill state: %+v", state)
	}
	if err := svc.SaveSkill(ctx, classID, teacherID, false); err != nil {
		t.Fatal(err)
	}
	state, err = svc.LoadSkill(ctx, classID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.UpdatedBy == nil || *state.UpdatedBy != teacherID || state.UpdatedAt == "" {
		t.Fatalf("unexpected skill state after update: %+v", state)
	}
}

func captureLogs(t *testing.T, run func()) []string {
	t.Helper()
	var buf bytes.Buffer
	flags := log.Flags()
	writer := log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	}()
	run()
	var lines []string
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func TestAuditLogsOmitPromptBody(t *testing.T) {
	conn, _, _ := testEnv(t)
	classID, teacherID, _ := fixtureIDs(t, conn)
	svc := &Service{DB: conn}
	ctx := context.Background()
	secretPrompt := "机密提示词内容-do-not-log"

	lines := captureLogs(t, func() {
		if _, err := svc.SavePrompt(ctx, classID, teacherID, secretPrompt); err != nil {
			t.Fatal(err)
		}
		if err := svc.SaveSkill(ctx, classID, teacherID, true); err != nil {
			t.Fatal(err)
		}
	})
	var promptLine, skillLine string
	for _, line := range lines {
		if strings.Contains(line, "assistant_prompt_updated") {
			promptLine = line
		}
		if strings.Contains(line, "assistant_skill_updated") {
			skillLine = line
		}
	}
	if promptLine == "" || skillLine == "" {
		t.Fatalf("audit lines missing: %v", lines)
	}
	if strings.Contains(promptLine, secretPrompt) || strings.Contains(promptLine, "机密") {
		t.Fatalf("prompt body leaked into audit log: %q", promptLine)
	}
	if !strings.Contains(promptLine, "actor_id=") || !strings.Contains(promptLine, "class_id=") || !strings.Contains(promptLine, "version=") {
		t.Fatalf("audit line missing metadata: %q", promptLine)
	}
	if !strings.Contains(skillLine, "enabled=true") {
		t.Fatalf("skill audit line missing state: %q", skillLine)
	}
}

type endpointFixture struct {
	server  *httptest.Server
	teacher *http.Client
	student *http.Client
	anon    *http.Client
}

func endpointEnv(t *testing.T) endpointFixture {
	conn, _, authSvc := testEnv(t)
	svc := &Service{DB: conn}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", authSvc.Login)
	mux.HandleFunc("GET /api/assistant/prompt", authSvc.Teacher(svc.GetPrompt))
	mux.HandleFunc("PUT /api/assistant/prompt", authSvc.Teacher(svc.PutPrompt))
	mux.HandleFunc("GET /api/assistant/skills/guided-problem-solving", authSvc.Teacher(svc.GetSkill))
	mux.HandleFunc("PUT /api/assistant/skills/guided-problem-solving", authSvc.Teacher(svc.PutSkill))
	ts := httptest.NewServer(mux)
	t.Cleanup(func() { ts.Close(); conn.Close() })

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
		client.Transport = &bearer{token: result.AccessToken}
		return client
	}
	return endpointFixture{
		server:  ts,
		teacher: login("a-teacher", "teacher-fixture-pw"),
		student: login("a-student", "student-fixture-pw"),
		anon:    &http.Client{},
	}
}

type bearer struct{ token string }

func (b *bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func do(t *testing.T, client *http.Client, method, url, body string) (*http.Response, string) {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return resp, out.String()
}

func TestPromptEndpointAuthorizationAndValidation(t *testing.T) {
	env := endpointEnv(t)
	base := env.server.URL + "/api/assistant/prompt"

	if resp, _ := do(t, env.anon, "GET", base, ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated should 401: %d", resp.StatusCode)
	}
	if resp, _ := do(t, env.student, "GET", base, ""); resp.StatusCode != 403 {
		t.Fatalf("student should 403: %d", resp.StatusCode)
	}
	if resp, _ := do(t, env.student, "PUT", base, `{"content":"学生写入"}`); resp.StatusCode != 403 {
		t.Fatalf("student put should 403: %d", resp.StatusCode)
	}

	resp, body := do(t, env.teacher, "GET", base, "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"configured":false`) {
		t.Fatalf("unconfigured read: %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("response must be no-store: %q", resp.Header.Get("Cache-Control"))
	}

	if resp, _ = do(t, env.teacher, "PUT", base, `not json`); resp.StatusCode != 400 {
		t.Fatalf("malformed body should 400: %d", resp.StatusCode)
	}
	if resp, _ = do(t, env.teacher, "PUT", base, `{"content":"内容","class_id":9}`); resp.StatusCode != 400 {
		t.Fatalf("client class_id must be rejected: %d", resp.StatusCode)
	}
	if resp, _ = do(t, env.teacher, "PUT", base, `{"content":"  "}`); resp.StatusCode != 400 {
		t.Fatalf("blank prompt should 400: %d", resp.StatusCode)
	}
	overLimit := `{"content":"` + strings.Repeat("字", MaxPromptChars+1) + `"}`
	if resp, _ = do(t, env.teacher, "PUT", base, overLimit); resp.StatusCode != 400 {
		t.Fatalf("over-limit prompt should 400: %d", resp.StatusCode)
	}

	resp, body = do(t, env.teacher, "PUT", base, `{"content":"# 引导提示"}`)
	if resp.StatusCode != 200 || !strings.Contains(body, `"configured":true`) || !strings.Contains(body, `"version":1`) {
		t.Fatalf("first save failed: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, env.teacher, "PUT", base, `{"content":"第二版"} `)
	if resp.StatusCode != 200 || !strings.Contains(body, `"version":2`) {
		t.Fatalf("second save failed: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, env.teacher, "GET", base, "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"content":"第二版"`) {
		t.Fatalf("read-back failed: %d %s", resp.StatusCode, body)
	}
	if !utf8.ValidString(body) {
		t.Fatal("response is not valid UTF-8")
	}
}

func TestSkillEndpointAuthorizationAndValidation(t *testing.T) {
	env := endpointEnv(t)
	base := env.server.URL + "/api/assistant/skills/guided-problem-solving"

	if resp, _ := do(t, env.anon, "GET", base, ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated should 401: %d", resp.StatusCode)
	}
	if resp, _ := do(t, env.student, "PUT", base, `{"enabled":false}`); resp.StatusCode != 403 {
		t.Fatalf("student should 403: %d", resp.StatusCode)
	}

	resp, body := do(t, env.teacher, "GET", base, "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"skill_key":"guided_problem_solving"`) {
		t.Fatalf("default skill read failed: %d %s", resp.StatusCode, body)
	}
	if resp, _ = do(t, env.teacher, "PUT", base, `{"enabled":false,"class_id":9}`); resp.StatusCode != 400 {
		t.Fatalf("client class_id must be rejected: %d", resp.StatusCode)
	}
	if resp, _ = do(t, env.teacher, "PUT", base, `{}`); resp.StatusCode != 400 {
		t.Fatalf("missing enabled must be rejected: %d", resp.StatusCode)
	}
	if resp, _ = do(t, env.teacher, "PUT", base, `{"enabled":"yes"}`); resp.StatusCode != 400 {
		t.Fatalf("wrong type must be rejected: %d", resp.StatusCode)
	}
	resp, body = do(t, env.teacher, "PUT", base, `{"enabled":false}`)
	if resp.StatusCode != 200 || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("skill update failed: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, env.teacher, "GET", base, "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("skill state did not persist: %d %s", resp.StatusCode, body)
	}
}
