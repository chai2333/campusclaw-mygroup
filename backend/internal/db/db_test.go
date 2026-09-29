package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set; skipping MySQL integration test")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Wait(ctx, conn); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanupTables(t, conn)
	return conn
}

func cleanupTables(t *testing.T, conn *sql.DB) {
	ctx := context.Background()
	for _, stmt := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
		"TRUNCATE TABLE knowledge_entries",
		"TRUNCATE TABLE materials",
		"TRUNCATE TABLE sessions",
		"TRUNCATE TABLE users",
		"TRUNCATE TABLE classes",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}
}

func TestSchemaMigration(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES('a','A班'),('b','B班')"); err != nil {
		t.Fatal(err)
	}
	var aID, bID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='a'").Scan(&aID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='b'").Scan(&bID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('u1','$2a$12$xxx','teacher',?)", aID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('u2','$2a$12$xxx','badrole',?)", aID); err == nil {
		t.Fatal("invalid role accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role) VALUES('u3','$2a$12$xxx','student')"); err != nil {
		t.Fatal("class_id NULL should be allowed for migrating users")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('u1','$2a$12$xxx','student',?)", aID); err == nil {
		t.Fatal("duplicate username accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO sessions(id,user_id,created_at,expires_at) VALUES('k1',1,100,50)"); err == nil {
		t.Fatal("invalid expiry accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES('m1',?,1,'t','f.txt','sk1')", aID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES('m2',?,1,'t','f.txt','sk1')", bID); err == nil {
		t.Fatal("duplicate storage_key accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES('m1',?,0,'hello')", bID); err == nil {
		t.Fatal("class mismatch between material and knowledge accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES('m1',?,0,'hello')", aID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES('m1',?,0,'dup')", aID); err == nil {
		t.Fatal("duplicate chunk index accepted")
	}
}

func TestSeedIdempotent(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Seed(ctx, conn, "teacher-seed-pw", "student-seed-pw", dir); err != nil {
		t.Fatal(err)
	}
	var classCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM classes").Scan(&classCount); err != nil {
		t.Fatal(err)
	}
	if classCount != 2 {
		t.Fatalf("expected 2 classes, got %d", classCount)
	}
	var userCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 4 {
		t.Fatalf("expected 4 users, got %d", userCount)
	}
	var materialCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials").Scan(&materialCount); err != nil {
		t.Fatal(err)
	}
	if materialCount != 2 {
		t.Fatalf("expected 2 seed materials, got %d", materialCount)
	}
	var aHash, bHash string
	if err := conn.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username='a-teacher'").Scan(&aHash); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username='b-teacher'").Scan(&bHash); err != nil {
		t.Fatal(err)
	}
	if aHash == bHash {
		t.Fatal("same-password hashes should differ (unique salts)")
	}
	if _, err := os.Stat(filepath.Join(dir, "seed-a-intro")); err != nil {
		t.Fatal("seed file A not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "seed-b-intro")); err != nil {
		t.Fatal("seed file B not created")
	}
	if err := Seed(ctx, conn, "teacher-seed-pw", "student-seed-pw", dir); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 4 {
		t.Fatalf("second seed duplicated users: %d", userCount)
	}
	var aHash2 string
	if err := conn.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username='a-teacher'").Scan(&aHash2); err != nil {
		t.Fatal(err)
	}
	if aHash2 != aHash {
		t.Fatal("second seed reset password")
	}
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials").Scan(&materialCount); err != nil {
		t.Fatal(err)
	}
	if materialCount != 2 {
		t.Fatalf("second seed duplicated materials: %d", materialCount)
	}
}

func TestSeedClassIsolation(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Seed(ctx, conn, "teacher-seed-pw", "student-seed-pw", dir); err != nil {
		t.Fatal(err)
	}
	var aTitle, aContent string
	if err := conn.QueryRowContext(ctx, "SELECT title FROM materials WHERE id='seed-a-intro'").Scan(&aTitle); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aTitle, "A班") {
		t.Fatal("A class seed title not distinguishable")
	}
	if err := conn.QueryRowContext(ctx, "SELECT content FROM knowledge_entries WHERE material_id='seed-a-intro' ORDER BY chunk_index LIMIT 1").Scan(&aContent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aContent, "A班") {
		t.Fatal("A class seed content not distinguishable")
	}
	var bContent string
	if err := conn.QueryRowContext(ctx, "SELECT content FROM knowledge_entries WHERE material_id='seed-b-intro' ORDER BY chunk_index LIMIT 1").Scan(&bContent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bContent, "B班") {
		t.Fatal("B class seed content not distinguishable")
	}
	if strings.Contains(aContent, "B班") || strings.Contains(bContent, "A班") {
		t.Fatal("cross-class content leakage")
	}
}
