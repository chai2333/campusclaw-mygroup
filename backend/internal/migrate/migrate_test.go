package migrate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"campusclaw/internal/db"
	_ "modernc.org/sqlite"
)

func initSQLite(t *testing.T, path string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	schema := `
CREATE TABLE IF NOT EXISTS classes (id INTEGER PRIMARY KEY, slug TEXT NOT NULL UNIQUE, name TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL, class_id INTEGER);
CREATE TABLE IF NOT EXISTS materials (id TEXT PRIMARY KEY, class_id INTEGER NOT NULL, uploader_id INTEGER NOT NULL, original_filename TEXT NOT NULL, storage_key TEXT NOT NULL UNIQUE, status TEXT NOT NULL, error TEXT, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS knowledge_chunks (id INTEGER PRIMARY KEY, material_id TEXT NOT NULL, class_id INTEGER NOT NULL, chunk_index INTEGER NOT NULL, content TEXT NOT NULL, UNIQUE(material_id, chunk_index));
`
	for _, stmt := range []string{
		schema,
		"INSERT INTO classes(id,slug,name) VALUES(1,'class-a','A 班'),(2,'class-b','B 班')",
		"INSERT INTO users(id,username,password_hash,role,class_id) VALUES(1,'a-teacher','$argon2id$v=19$m=65536,t=3,p=4$czA$Zm9v','teacher',1)",
		"INSERT INTO materials(id,class_id,uploader_id,original_filename,storage_key,status,created_at) VALUES('mat-1',1,1,'intro.md','key-1.md','ready','2026-01-01 10:00:00')",
		"INSERT INTO materials(id,class_id,uploader_id,original_filename,storage_key,status,created_at) VALUES('mat-2',1,1,'draft.md','key-2.md','failed','2026-01-02 10:00:00')",
		"INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,content) VALUES('mat-1',1,0,'hello')",
		"INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,content) VALUES('mat-1',1,1,'world')",
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	srcDir := filepath.Dir(path)
	uploadsDir := filepath.Join(srcDir, "uploads")
	os.MkdirAll(uploadsDir, 0o755)
	os.WriteFile(filepath.Join(uploadsDir, "key-1.md"), []byte("hello\nworld"), 0o644)
}

func mysqlConn(t *testing.T) *sql.DB {
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
	t.Cleanup(func() { conn.Close() })
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	if err := db.Migrate(ctx2, conn); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
 "TRUNCATE TABLE tutor_agent_materials", "TRUNCATE TABLE tutor_agent_skills", "TRUNCATE TABLE tutor_agents", "TRUNCATE TABLE skills",
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
	return conn
}

func TestMigration(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "campusclaw.sqlite3")
	uploadsDir := filepath.Join(tmpDir, "uploads")
	initSQLite(t, sqlitePath)
	dstUploads := filepath.Join(tmpDir, "dst-uploads")
	mysql := mysqlConn(t)
	ctx := context.Background()
	report, err := Run(ctx, sqlitePath, uploadsDir, mysql, dstUploads)
	if err != nil {
		t.Fatal(err)
	}
	if report.Classes != 2 {
		t.Fatalf("expected 2 classes, got %d", report.Classes)
	}
	if report.Users != 1 {
		t.Fatalf("expected 1 user, got %d", report.Users)
	}
	if report.Materials != 1 {
		t.Fatalf("expected 1 ready material, got %d", report.Materials)
	}
	if report.Chunks != 2 {
		t.Fatalf("expected 2 chunks, got %d", report.Chunks)
	}
	if report.Files != 1 {
		t.Fatalf("expected 1 file, got %d", report.Files)
	}
	if len(report.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly (failed material), got %d", len(report.Anomalies))
	}
	var slug string
	if err := mysql.QueryRowContext(ctx, "SELECT slug FROM classes WHERE id=1").Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug != "a" {
		t.Fatalf("expected slug 'a' for migrated class-a, got %q", slug)
	}
	var hash string
	if err := mysql.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username='a-teacher'").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "" || hash[0] != '$' {
		t.Fatalf("password hash not preserved: %q", hash)
	}
	var count int
	mysql.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials WHERE status='failed'").Scan(&count)
	if count != 0 {
		t.Fatal("failed material should not be migrated")
	}
	var body string
	if err := mysql.QueryRowContext(ctx, "SELECT body_text FROM knowledge_entries WHERE material_id='mat-1'").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "helloworld" {
		t.Fatalf("migrated body mismatch: %q", body)
	}
	// Idempotency: second run should not duplicate
	report2, err := Run(ctx, sqlitePath, uploadsDir, mysql, dstUploads)
	if err != nil {
		t.Fatal(err)
	}
	if report2.Materials != 1 || report2.Chunks != 2 {
		t.Fatalf("second run duplicated: %+v", report2)
	}
}

func TestBackup(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "campusclaw.sqlite3")
	uploadsDir := filepath.Join(tmpDir, "uploads")
	initSQLite(t, sqlitePath)
	backupDir := filepath.Join(tmpDir, "backup")
	manifestPath := filepath.Join(tmpDir, "manifest.txt")
	ctx := context.Background()
	if err := Backup(ctx, sqlitePath, uploadsDir, backupDir, manifestPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "campusclaw.sqlite3")); err != nil {
		t.Fatal("backup copy not created")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("manifest empty")
	}
}
