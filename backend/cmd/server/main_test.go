package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/db"
	"campusclaw/internal/materials"
)

func setupServer(t *testing.T) (string, *auth.Service, *materials.Service) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Wait(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
 "TRUNCATE TABLE tutor_agent_materials", "TRUNCATE TABLE tutor_agent_skills", "TRUNCATE TABLE tutor_agents", "TRUNCATE TABLE skills",
		"TRUNCATE TABLE message_citations",
		"TRUNCATE TABLE conversation_messages",
		"TRUNCATE TABLE conversations",
		"TRUNCATE TABLE class_skills",
		"TRUNCATE TABLE assistant_prompts",
		"TRUNCATE TABLE knowledge_chunks",
		"TRUNCATE TABLE knowledge_entries",
		"TRUNCATE TABLE materials",
		"TRUNCATE TABLE sessions",
		"TRUNCATE TABLE users",
		"TRUNCATE TABLE classes",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		conn.ExecContext(context.Background(), stmt)
	}
	uploadDir := t.TempDir()
	cfg := config.Config{
		Environment:     "test",
		SessionSecret:   "test-session-secret-key-32bytes!",
		DBHost:          "",
		DBName:          "campusclaw",
		DBUser:          "campusclaw",
		DBPassword:      "",
		UploadDir:       uploadDir,
		MaxUploadBytes:  2097152,
		SessionTTL:      28800,
		MaxFailures:     5,
		WindowSeconds:   300,
		LockSeconds:     300,
		TeacherPassword: "teacher-fixture-pw",
		StudentPassword: "student-fixture-pw",
	}
	if err := db.Seed(ctx, conn, cfg.TeacherPassword, cfg.StudentPassword, uploadDir); err != nil {
		t.Fatal(err)
	}
	authSvc, err := auth.New(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	matSvc := &materials.Service{DB: conn, Dir: uploadDir, MaxBytes: int64(cfg.MaxUploadBytes)}
	matSvc.Clean.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", authSvc.Login)
	mux.HandleFunc("GET /api/me", authSvc.Require(authSvc.Me))
	mux.HandleFunc("POST /api/logout", authSvc.Require(authSvc.Logout))
	mux.HandleFunc("GET /api/materials", authSvc.Class(matSvc.List))
	mux.HandleFunc("POST /api/materials", authSvc.Teacher(matSvc.Upload))
	mux.HandleFunc("GET /api/materials/{id}", authSvc.Class(matSvc.Detail))
	mux.HandleFunc("GET /api/materials/{id}/file", authSvc.Class(matSvc.Download))
	ts := httptest.NewServer(mux)
	t.Cleanup(func() { ts.Close() })
	return ts.URL, authSvc, matSvc
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

type bearerTransport struct {
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

func newBearerClient() *http.Client {
	return &http.Client{Transport: &bearerTransport{}}
}

func tokenOf(client *http.Client) string {
	return client.Transport.(*bearerTransport).token
}

func setToken(client *http.Client, token string) {
	client.Transport.(*bearerTransport).token = token
}

func login(t *testing.T, client *http.Client, baseURL, user, pass string) int {
	resp := doJSON(t, client, "POST", baseURL+"/api/login", map[string]string{"username": user, "password": pass})
	if resp.StatusCode == http.StatusOK {
		var result auth.LoginResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.TokenType != "Bearer" || result.AccessToken == "" || result.ExpiresIn <= 0 {
			t.Fatalf("invalid login response: token_type=%q token_length=%d expires_in=%d", result.TokenType, len(result.AccessToken), result.ExpiresIn)
		}
		setToken(client, result.AccessToken)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestLoginSuccessAndFailure(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	if code := login(t, client, baseURL, "a-teacher", "teacher-fixture-pw"); code != 200 {
		t.Fatalf("teacher login failed: %d", code)
	}
	if code := login(t, client, baseURL, "a-teacher", "wrong"); code != 401 {
		t.Fatalf("wrong password should 401: %d", code)
	}
	if code := login(t, client, baseURL, "nobody", "teacher-fixture-pw"); code != 401 {
		t.Fatalf("unknown user should 401: %d", code)
	}
	if code := login(t, client, baseURL, "a-student", "student-fixture-pw"); code != 200 {
		t.Fatalf("student login failed: %d", code)
	}
}

func TestLoginInvalidJSON(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := &http.Client{}
	req, _ := http.NewRequest("POST", baseURL+"/api/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("invalid JSON should 400: %d", resp.StatusCode)
	}
	resp.Body.Close()
	req2, _ := http.NewRequest("POST", baseURL+"/api/login", strings.NewReader(`{"username":"a"}`))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := client.Do(req2)
	if resp2.StatusCode != 400 {
		t.Fatalf("missing password should 400: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestApiMeAndLogout(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-teacher", "teacher-fixture-pw")
	resp, err := client.Get(baseURL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("/api/me should 200 after login: %d", resp.StatusCode)
	}
	var user auth.User
	json.NewDecoder(resp.Body).Decode(&user)
	resp.Body.Close()
	if user.Role != "teacher" || user.Username != "a-teacher" || user.ClassID == nil {
		t.Fatalf("unexpected user: %+v", user)
	}
	logoutResp, err := client.Post(baseURL+"/api/logout", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != 200 {
		t.Fatalf("logout should 200: %d", logoutResp.StatusCode)
	}
	logoutResp.Body.Close()
	meAfter, _ := client.Get(baseURL + "/api/me")
	if meAfter.StatusCode != 401 {
		t.Fatalf("/api/me should 401 after logout: %d", meAfter.StatusCode)
	}
	meAfter.Body.Close()
}

func TestSessionRotationOnRelogin(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-teacher", "teacher-fixture-pw")
	token1 := tokenOf(client)
	login(t, client, baseURL, "a-teacher", "teacher-fixture-pw")
	token2 := tokenOf(client)
	if token1 == "" || token2 == "" {
		t.Fatal("no bearer token returned")
	}
	if token1 == token2 {
		t.Fatal("session not rotated")
	}
	oldClient := newBearerClient()
	setToken(oldClient, token1)
	oldResp, _ := oldClient.Get(baseURL + "/api/me")
	if oldResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old token should be revoked: %d", oldResp.StatusCode)
	}
	oldResp.Body.Close()
}

func TestMaterialsListClassIsolation(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	clientA := newBearerClient()
	login(t, clientA, baseURL, "a-student", "student-fixture-pw")
	resp, _ := clientA.Get(baseURL + "/api/materials")
	if resp.StatusCode != 200 {
		t.Fatalf("list should 200: %d", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	items, _ := result["materials"].([]any)
	for _, m := range items {
		mat := m.(map[string]any)
		if !strings.Contains(mat["title"].(string), "A班") {
			t.Fatalf("B class material leaked into A: %v", mat["title"])
		}
	}
	clientB := newBearerClient()
	login(t, clientB, baseURL, "b-student", "student-fixture-pw")
	resp2, _ := clientB.Get(baseURL + "/api/materials")
	json.NewDecoder(resp2.Body).Decode(&result)
	resp2.Body.Close()
	items, _ = result["materials"].([]any)
	for _, m := range items {
		mat := m.(map[string]any)
		if !strings.Contains(mat["title"].(string), "B班") {
			t.Fatalf("A class material leaked into B: %v", mat["title"])
		}
	}
}

func TestMaterialDetailCrossClass404(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	clientA := newBearerClient()
	login(t, clientA, baseURL, "a-student", "student-fixture-pw")
	resp, _ := clientA.Get(baseURL + "/api/materials/seed-a-intro")
	if resp.StatusCode != 200 {
		t.Fatalf("own class detail should 200: %d", resp.StatusCode)
	}
	resp.Body.Close()
	clientB := newBearerClient()
	login(t, clientB, baseURL, "b-student", "student-fixture-pw")
	resp2, _ := clientB.Get(baseURL + "/api/materials/seed-a-intro")
	if resp2.StatusCode != 404 {
		t.Fatalf("cross-class detail should 404: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
	resp3, _ := clientB.Get(baseURL + "/api/materials/nonexistent")
	if resp3.StatusCode != 404 {
		t.Fatalf("nonexistent should 404: %d", resp3.StatusCode)
	}
	resp3.Body.Close()
}

func TestStudentUploadForbidden(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-student", "student-fixture-pw")
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "test.md")
	fw.Write([]byte("# test\nhello"))
	w.Close()
	req, _ := http.NewRequest("POST", baseURL+"/api/materials", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("student upload should 403: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTeacherUploadSuccess(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-teacher", "teacher-fixture-pw")
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "upload-test.md")
	fw.Write([]byte("# Upload Test\n\nThis is a test material for A class."))
	w.Close()
	req, _ := http.NewRequest("POST", baseURL+"/api/materials", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload should 201: %d body=%s", resp.StatusCode, body)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	matID := result["id"]
	if matID == "" {
		t.Fatal("no material id returned")
	}
	detailResp, _ := client.Get(baseURL + "/api/materials/" + matID)
	if detailResp.StatusCode != 200 {
		t.Fatalf("new material detail should 200: %d", detailResp.StatusCode)
	}
	detailResp.Body.Close()
	listResp, _ := client.Get(baseURL + "/api/materials?q=Upload")
	if listResp.StatusCode != 200 {
		t.Fatalf("search should 200: %d", listResp.StatusCode)
	}
	listResp.Body.Close()
}

func TestUploadValidation(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-teacher", "teacher-fixture-pw")
	tests := []struct {
		name     string
		filename string
		content  string
		wantCode int
	}{
		{"bad extension", "test.pdf", "hello", 400},
		{"empty content", "test.txt", "   \n\t ", 400},
		{"binary content", "test.txt", "\x00\x01\x02\x03", 400},
		{"valid txt", "test.txt", "hello world", 201},
		{"valid md", "test.md", "# hello", 201},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			fw, _ := w.CreateFormFile("file", tc.filename)
			fw.Write([]byte(tc.content))
			w.Close()
			req, _ := http.NewRequest("POST", baseURL+"/api/materials", &buf)
			req.Header.Set("Content-Type", w.FormDataContentType())
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, resp.StatusCode)
			}
			resp.Body.Close()
		})
	}
}

func TestMaterialDownload(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	clientA := newBearerClient()
	login(t, clientA, baseURL, "a-student", "student-fixture-pw")
	resp, _ := clientA.Get(baseURL + "/api/materials/seed-a-intro/file")
	if resp.StatusCode != 200 {
		t.Fatalf("download should 200: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "A班") {
		t.Fatal("download content mismatch")
	}
	clientB := newBearerClient()
	login(t, clientB, baseURL, "b-student", "student-fixture-pw")
	resp2, _ := clientB.Get(baseURL + "/api/materials/seed-a-intro/file")
	if resp2.StatusCode != 404 {
		t.Fatalf("cross-class download should 404: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestNoSessionEndpointsRequireAuth(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := &http.Client{}
	resp, _ := client.Get(baseURL + "/api/me")
	if resp.StatusCode != 401 {
		t.Fatalf("no session /api/me should 401: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
	}
	resp.Body.Close()
	resp2, _ := client.Get(baseURL + "/api/materials")
	if resp2.StatusCode != 401 {
		t.Fatalf("no session /api/materials should 401: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestOnlyValidBearerAuthorizationAuthenticates(t *testing.T) {
	baseURL, _, _ := setupServer(t)
	client := newBearerClient()
	login(t, client, baseURL, "a-student", "student-fixture-pw")
	token := tokenOf(client)

	tests := []struct {
		name    string
		headers []string
		cookie  bool
	}{
		{name: "basic", headers: []string{"Basic " + token}},
		{name: "missing token", headers: []string{"Bearer"}},
		{name: "invalid token", headers: []string{"Bearer invalid"}},
		{name: "duplicate", headers: []string{"Bearer " + token, "Bearer " + token}},
		{name: "cookie only", cookie: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", baseURL+"/api/me", nil)
			for _, value := range tc.headers {
				req.Header.Add("Authorization", value)
			}
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: "campus_session", Value: token})
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}
