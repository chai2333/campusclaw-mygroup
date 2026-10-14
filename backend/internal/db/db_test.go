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
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES('m1',?,'hello')", bID); err == nil {
		t.Fatal("class mismatch between material and knowledge accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES('m1',?,'hello')", aID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES('m1',?,'dup')", aID); err == nil {
		t.Fatal("duplicate material accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES('m1',?,0,'hello',0,5,'bogus','pending')", aID); err == nil {
		t.Fatal("invalid chunk strategy accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES('m1',?,0,'hello',0,5,'auto','ready')", aID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES('m1',?,0,'dup',0,3,'auto','ready')", aID); err == nil {
		t.Fatal("duplicate chunk index accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES('m1',?,1,'world',5,10,'custom','pending')", aID); err != nil {
		t.Fatal(err)
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
	if err := conn.QueryRowContext(ctx, "SELECT body_text FROM knowledge_entries WHERE material_id='seed-a-intro'").Scan(&aContent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aContent, "A班") {
		t.Fatal("A class seed content not distinguishable")
	}
	var bContent string
	if err := conn.QueryRowContext(ctx, "SELECT body_text FROM knowledge_entries WHERE material_id='seed-b-intro'").Scan(&bContent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bContent, "B班") {
		t.Fatal("B class seed content not distinguishable")
	}
	if strings.Contains(aContent, "B班") || strings.Contains(bContent, "A班") {
		t.Fatal("cross-class content leakage")
	}
}

func TestLegacyKnowledgeConversion(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "DROP TABLE knowledge_chunks"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "DROP TABLE knowledge_entries"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TABLE knowledge_entries (
	 id BIGINT PRIMARY KEY AUTO_INCREMENT,
	 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
	 class_id BIGINT NOT NULL,
	 chunk_index INT NOT NULL,
	 content MEDIUMTEXT NOT NULL,
	 CONSTRAINT nonnegative_chunk CHECK (chunk_index >= 0),
	 UNIQUE KEY material_chunk (material_id,chunk_index),
	 INDEX knowledge_class_material (class_id,material_id),
	 FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE,
	 FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES('c','C班')"); err != nil {
		t.Fatal(err)
	}
	var classID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='c'").Scan(&classID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('u-c','$2a$12$xxx','teacher',?)", classID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES('m1',?,1,'t','f.md','sk-c')", classID); err != nil {
		t.Fatal(err)
	}
	// Insert chunks out of order to prove chunk_index ordering drives the merge.
	for _, c := range []struct {
		idx  int
		text string
	}{{2, "世界"}, {0, "你好"}, {1, "，"}} {
		if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES('m1',?,?,?)", classID, c.idx, c.text); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := conn.QueryRowContext(ctx, "SELECT body_text FROM knowledge_entries WHERE material_id='m1'").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "你好，世界" {
		t.Fatalf("merged body mismatch: %q", body)
	}
	var rowCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM knowledge_entries").Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 merged row, got %d", rowCount)
	}
	for _, column := range []string{"id", "content", "chunk_index"} {
		var count int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='knowledge_entries' AND COLUMN_NAME=?", column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy column %s still present", column)
		}
	}
	var pk string
	if err := conn.QueryRowContext(ctx, "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='knowledge_entries' AND CONSTRAINT_NAME='PRIMARY'").Scan(&pk); err != nil {
		t.Fatal(err)
	}
	if pk != "material_id" {
		t.Fatalf("unexpected primary key column: %s", pk)
	}
	// Second run must be a no-op.
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT body_text FROM knowledge_entries WHERE material_id='m1'").Scan(&body); err != nil || body != "你好，世界" {
		t.Fatalf("idempotent rerun changed data: %q %v", body, err)
	}
	// knowledge_chunks must exist again after migration and accept rows.
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES('m1',?,0,'你好，世界',0,5,'auto','pending')", classID); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantSchemaConstraints(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES('a','A班')"); err != nil {
		t.Fatal(err)
	}
	var classID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='a'").Scan(&classID); err != nil {
		t.Fatal(err)
	}
	// Re-running migration after classes exist must seed their skill rows
	// and stay idempotent.
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	// Migration seeds an enabled guided_problem_solving skill row per class.
	var enabled int
	if err := conn.QueryRowContext(ctx, "SELECT enabled FROM class_skills WHERE class_id=? AND skill_key='guided_problem_solving'", classID).Scan(&enabled); err != nil {
		t.Fatalf("skill row not seeded by migration: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("seeded skill must be enabled, got %d", enabled)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO class_skills(class_id,skill_key,enabled) VALUES(?,'other_skill',1)", classID); err == nil {
		t.Fatal("unsupported skill key accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO assistant_prompts(class_id,body,version) VALUES(?,'提示词',0)", classID); err == nil {
		t.Fatal("prompt version 0 accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO assistant_prompts(class_id,body,version) VALUES(?,'提示词',1)", classID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO assistant_prompts(class_id,body,version) VALUES(?,'重复',2)", classID); err == nil {
		t.Fatal("duplicate prompt row accepted")
	}
	// Conversation, message and citation chain.
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('stu','$2a$12$xxx','student',?)", classID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM users WHERE username='stu'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversations(id,owner_user_id,class_id,title) VALUES('conv1',?,?,'新对话')", userID, classID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m1','conv1',1,'moderator','问题','completed')"); err == nil {
		t.Fatal("invalid role accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m1','conv1',1,'user','问题','flying')"); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m1','conv1',0,'user','问题','completed')"); err == nil {
		t.Fatal("sequence 0 accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m1','conv1',1,'user','问题','completed')"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m2','conv1',1,'assistant','回答','completed')"); err == nil {
		t.Fatal("duplicate sequence accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status,prompt_version,skill_enabled,model) VALUES('m2','conv1',2,'assistant','回答','streaming',1,1,'test-model')"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO message_citations(assistant_message_id,ordinal,material_id,chunk_id,title_snapshot,chunk_index,char_start,char_end,excerpt_snapshot) VALUES('m2',0,'mat',1,'标题',0,0,5,'摘录')"); err == nil {
		t.Fatal("citation ordinal 0 accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO message_citations(assistant_message_id,ordinal,material_id,chunk_id,title_snapshot,chunk_index,char_start,char_end,excerpt_snapshot) VALUES('m2',5,'mat',1,'标题',0,0,5,'摘录')"); err == nil {
		t.Fatal("citation ordinal 5 accepted")
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO message_citations(assistant_message_id,ordinal,material_id,chunk_id,title_snapshot,chunk_index,char_start,char_end,excerpt_snapshot) VALUES('m2',1,'mat',1,'标题',0,0,5,'摘录')"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO message_citations(assistant_message_id,ordinal,material_id,chunk_id,title_snapshot,chunk_index,char_start,char_end,excerpt_snapshot) VALUES('m2',1,'mat',2,'标题',1,3,7,'摘录2')"); err == nil {
		t.Fatal("duplicate citation ordinal accepted")
	}
}

func TestStartupRepair(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES('a','A班')"); err != nil {
		t.Fatal(err)
	}
	var classID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='a'").Scan(&classID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES('stu','$2a$12$xxx','student',?)", classID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM users WHERE username='stu'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO conversations(id,owner_user_id,class_id,title) VALUES('conv1',?,?,'新对话')", userID, classID); err != nil {
		t.Fatal(err)
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m1','conv1',1,'user','问题','completed')")
	mustExec("INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status,prompt_version,skill_enabled) VALUES('m2','conv1',2,'assistant','历史回答','completed',1,1)")
	mustExec("INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('m3','conv1',3,'user','新的问题','completed')")
	mustExec("INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status,prompt_version) VALUES('m4','conv1',4,'assistant','中断前的部分内容','streaming',2)")
	mustExec("UPDATE conversations SET active_message_id='m4' WHERE id='conv1'")
	mustExec("DELETE FROM class_skills WHERE class_id=?", classID)

	if err := Repair(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var status, errorCode sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT status,error_code FROM conversation_messages WHERE id='m4'").Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status.String != "failed" || errorCode.String != "server_interrupted" {
		t.Fatalf("stale turn not repaired: %q/%q", status.String, errorCode.String)
	}
	var completed string
	if err := conn.QueryRowContext(ctx, "SELECT status FROM conversation_messages WHERE id='m2'").Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != "completed" {
		t.Fatalf("completed history changed: %q", completed)
	}
	var active sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT active_message_id FROM conversations WHERE id='conv1'").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active.Valid {
		t.Fatalf("active lock not cleared: %q", active.String)
	}
	var skills int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM class_skills WHERE class_id=? AND skill_key='guided_problem_solving' AND enabled=1", classID).Scan(&skills); err != nil {
		t.Fatal(err)
	}
	if skills != 1 {
		t.Fatalf("missing skill row not repaired: %d", skills)
	}
	// Second run must be a no-op.
	if err := Repair(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT status,error_code FROM conversation_messages WHERE id='m4'").Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status.String != "failed" || errorCode.String != "server_interrupted" {
		t.Fatalf("second repair changed terminal state: %q/%q", status.String, errorCode.String)
	}
}
