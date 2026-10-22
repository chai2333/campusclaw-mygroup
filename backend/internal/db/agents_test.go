package db

import (
	"context"
	"testing"
)

func TestAgentMigrationPreservesOldHistoryAndTeacherEdits(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	statements := []string{
		"INSERT INTO classes(id,slug,name) VALUES(1,'legacy','旧班')",
		"INSERT INTO users(id,username,password_hash,role,class_id) VALUES(1,'legacy-user','hash','student',1)",
		"INSERT INTO assistant_prompts(class_id,body,version) VALUES(1,'旧角色提示词',7)",
		"INSERT INTO class_skills(class_id,skill_key,enabled) VALUES(1,'guided_problem_solving',0)",
		"INSERT INTO conversations(id,owner_user_id,class_id,title) VALUES('old-conversation',1,1,'原来的标题')",
		"INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status) VALUES('old-message','old-conversation',1,'assistant','原来的回答','completed')",
	}
	for _, q := range statements {
		if _, err := conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, conn); err != nil {
			t.Fatal(err)
		}
	}
	var agentID int64
	var prompt string
	var version, count int
	if err := conn.QueryRow("SELECT id,prompt,version FROM tutor_agents WHERE class_id=1 AND seed_key='legacy-default'").Scan(&agentID, &prompt, &version); err != nil {
		t.Fatal(err)
	}
	if prompt != "旧角色提示词" || version != 7 {
		t.Fatalf("legacy prompt not migrated: %s/%d", prompt, version)
	}
	var linked int64
	var title, content string
	if err := conn.QueryRow("SELECT agent_id,title FROM conversations WHERE id='old-conversation'").Scan(&linked, &title); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT content FROM conversation_messages WHERE id='old-message'").Scan(&content); err != nil {
		t.Fatal(err)
	}
	if linked != agentID || title != "原来的标题" || content != "原来的回答" {
		t.Fatal("history changed during migration")
	}
	if _, err := conn.Exec("UPDATE tutor_agents SET prompt='新的智能体角色' WHERE id=?", agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("DELETE FROM tutor_agent_skills WHERE agent_id=?", agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("DELETE FROM skills WHERE class_id=1 AND seed_key='math'"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT prompt FROM tutor_agents WHERE id=?", agentID).Scan(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt != "新的智能体角色" {
		t.Fatal("startup overwrote teacher configuration")
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM tutor_agent_skills WHERE agent_id=?", agentID).Scan(&count); err != nil || count != 0 {
		t.Fatal("startup restored removed bindings")
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM skills WHERE class_id=1 AND seed_key='math'").Scan(&count); err != nil || count != 0 {
		t.Fatal("startup restored deleted skill")
	}
}
func TestAgentBindingClassForeignKeys(t *testing.T) {
	conn := testDB(t)
	for _, q := range []string{"INSERT INTO classes(id,slug,name) VALUES(1,'a','A'),(2,'b','B')", "INSERT INTO tutor_agents(id,class_id,name,prompt) VALUES(1,1,'A助手','')", "INSERT INTO skills(id,class_id,name,trigger_text,description,tools_json,source_text) VALUES(1,2,'异班技能','x','说明','[]','原文')"} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec("INSERT INTO tutor_agent_skills(agent_id,skill_id,class_id) VALUES(1,1,1)"); err == nil {
		t.Fatal("database accepted cross-class skill binding")
	}
}

func TestBuiltinApplicationsAreIndependentAndSeedOnce(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	if err := Seed(ctx, conn, "teacher-fixture-pw", "student-fixture-pw", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var classID, mathID int64
	if err := conn.QueryRow("SELECT id FROM classes WHERE slug='a'").Scan(&classID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM tutor_agents WHERE class_id=?", classID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("built-in count %d %v", count, err)
	}
	if err := conn.QueryRow("SELECT id FROM tutor_agents WHERE class_id=? AND seed_key='builtin-math-v1'", classID).Scan(&mathID); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := conn.QueryRow("SELECT s.name FROM skills s JOIN tutor_agent_skills b ON b.skill_id=s.id WHERE b.agent_id=?", mathID).Scan(&name); err != nil || name != "数学解题引导" {
		t.Fatalf("math binding: %s %v", name, err)
	}
	if _, err := conn.Exec("UPDATE tutor_agents SET name='教师改名',prompt='自定义提示词',enabled=0 WHERE id=?", mathID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("DELETE FROM tutor_agent_skills WHERE agent_id=?", mathID); err != nil {
		t.Fatal(err)
	}
	if err := seedBuiltinAgents(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var prompt string
	var enabled bool
	if err := conn.QueryRow("SELECT name,prompt,enabled FROM tutor_agents WHERE id=?", mathID).Scan(&name, &prompt, &enabled); err != nil || name != "教师改名" || prompt != "自定义提示词" || enabled {
		t.Fatal("teacher edits overwritten", err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM tutor_agent_skills WHERE agent_id=?", mathID).Scan(&count); err != nil || count != 0 {
		t.Fatal("removed binding restored", err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM tutor_agents WHERE class_id=?", classID).Scan(&count); err != nil || count != 3 {
		t.Fatal("duplicate built-ins", err)
	}
}
