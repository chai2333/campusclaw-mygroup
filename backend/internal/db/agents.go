package db

import (
	"campusclaw/internal/tutor"
	"context"
	"database/sql"
	"encoding/json"
)

func migrateAgents(ctx context.Context, conn *sql.DB) error {
	for _, col := range []struct{ table, name, definition string }{{"skills", "tool_policy", "VARCHAR(16) NOT NULL DEFAULT 'auto'"}, {"conversation_messages", "tool_calls_json", "JSON NULL"}, {"conversations", "agent_id", "BIGINT NULL"}, {"conversation_messages", "skill_id", "BIGINT NULL"}, {"conversation_messages", "skill_name", "VARCHAR(32) NULL"}} {
		has, err := columnExists(ctx, conn, col.table, col.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = conn.ExecContext(ctx, "ALTER TABLE "+col.table+" ADD COLUMN "+col.name+" "+col.definition); err != nil {
				return err
			}
		}
	}
	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME='conversations' AND CONSTRAINT_NAME='fk_conversation_agent'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE conversations ADD CONSTRAINT fk_conversation_agent FOREIGN KEY(agent_id,class_id) REFERENCES tutor_agents(id,class_id)"); err != nil {
			return err
		}
	}
	return seedAgents(ctx, conn)
}

var starterSkills = []struct{ key, source string }{
	{"math", "名称：数学解题引导\n适用条件：方程,去括号,分数,通分,面积,应用题,未知数,行程,速度,路程,匀速\n资料：数学\n工具：knowledge_search\n启用：是\n\n先接住学生已有的计算，指出下一步应检查哪一项；不要算出最终得数或给出整题过程。依据数学资料给分步提示并注明出处。"},
	{"chinese", "名称：语文解题引导\n适用条件：春晓,默写,阅读,中心,作文,开头,古诗,词语\n资料：语文\n工具：knowledge_search\n启用：是\n\n先接住学生已有的一句，指出应往哪里找依据；不要直接给诗的下一句、中心句或改好的段落。引用语文资料，保留学生自己完成的关键一步。"},
	{"legacy-guided", "名称：解题引导\n适用条件：题,如何,怎么,求,问题\n资料：\n工具：knowledge_search\n启用：是\n\n解释概念、给出下一步提示和检查方法，请学生自己完成关键推理，不直接给出最终答案。"},
}

// Bootstrap only seeds/bridges once; subsequent startup never restores removed
// bindings or replaces teacher edits. Legacy tables are not read by chat.
func seedAgents(ctx context.Context, conn *sql.DB) error {
	rows, err := conn.QueryContext(ctx, "SELECT id FROM classes ORDER BY id")
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, classID := range ids {
		var existingID int64
		existingErr := conn.QueryRowContext(ctx, "SELECT id FROM tutor_agents WHERE class_id=? AND seed_key='legacy-default'", classID).Scan(&existingID)
		if existingErr == nil {
			if _, err := conn.ExecContext(ctx, "UPDATE conversations SET agent_id=? WHERE class_id=? AND agent_id IS NULL", existingID, classID); err != nil {
				return err
			}
			continue
		}
		if existingErr != sql.ErrNoRows {
			return existingErr
		}

		for _, seed := range starterSkills {
			skill, e := tutor.ParseSkill(seed.source)
			if e != nil {
				return e
			}
			tools, _ := json.Marshal(skill.Tools)
			if _, e = conn.ExecContext(ctx, "INSERT IGNORE INTO skills(class_id,name,trigger_text,subject_text,description,tools_json,source_text,enabled,seed_key) VALUES(?,?,?,?,?,?,?,1,?)", classID, skill.Name, skill.TriggerText, skill.SubjectText, skill.Description, string(tools), seed.source, seed.key); e != nil {
				return e
			}
		}
		result, e := conn.ExecContext(ctx, `INSERT IGNORE INTO tutor_agents(class_id,name,prompt,enabled,version,seed_key)
    SELECT c.id,'默认学习助手',LEFT(COALESCE(p.body,''),2000),1,COALESCE(p.version,1),'legacy-default' FROM classes c LEFT JOIN assistant_prompts p ON p.class_id=c.id WHERE c.id=?`, classID)
		if e != nil {
			return e
		}
		created, _ := result.RowsAffected()
		var agentID int64
		if e = conn.QueryRowContext(ctx, "SELECT id FROM tutor_agents WHERE class_id=? AND seed_key='legacy-default'", classID).Scan(&agentID); e != nil {
			return e
		}
		if created > 0 {
			if _, e = conn.ExecContext(ctx, "INSERT IGNORE INTO tutor_agent_skills(agent_id,skill_id,class_id) SELECT ?,id,class_id FROM skills WHERE class_id=? AND seed_key IS NOT NULL", agentID, classID); e != nil {
				return e
			}
			if _, e = conn.ExecContext(ctx, "INSERT IGNORE INTO tutor_agent_materials(agent_id,material_id,class_id) SELECT ?,id,class_id FROM materials WHERE class_id=? AND status='ready'", agentID, classID); e != nil {
				return e
			}
			if _, e = conn.ExecContext(ctx, "UPDATE skills s JOIN class_skills old ON old.class_id=s.class_id SET s.enabled=old.enabled WHERE s.class_id=? AND s.seed_key='legacy-guided'", classID); e != nil {
				return e
			}
		}
		if _, e = conn.ExecContext(ctx, "UPDATE conversations SET agent_id=? WHERE class_id=? AND agent_id IS NULL", agentID, classID); e != nil {
			return e
		}
	}
	return nil
}

// Seed independent applications once; subsequent starts never rewrite prompts,
// restore removed bindings or broaden their material scope.
func seedBuiltinAgents(ctx context.Context, conn *sql.DB) error {
	rows, err := conn.QueryContext(ctx, "SELECT id FROM classes ORDER BY id")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, classID := range ids {
		for _, preset := range []struct{ key, name, prompt, skill string }{
			{"builtin-math-v1", "数学学习助手", "你是数学学习助手，先帮助学生理解题意、检查单位和已有步骤，再给下一步提示，引导学生自行完成关键计算。", "math"},
			{"builtin-reading-v1", "语文阅读助手", "你是语文阅读助手，引导学生寻找原文依据、理解词句和文章结构，鼓励学生先表达自己的理解。", "chinese"},
		} {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, "INSERT IGNORE INTO tutor_agents(class_id,name,prompt,enabled,version,seed_key) VALUES(?,?,?,1,1,?)", classID, preset.name, preset.prompt, preset.key)
			if err != nil {
				tx.Rollback()
				return err
			}
			created, _ := result.RowsAffected()
			if created > 0 {
				id, err := result.LastInsertId()
				if err != nil {
					tx.Rollback()
					return err
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO tutor_agent_skills(agent_id,skill_id,class_id) SELECT ?,id,class_id FROM skills WHERE class_id=? AND seed_key=?", id, classID, preset.skill)
				if err != nil {
					tx.Rollback()
					return err
				}
			}
			if err = tx.Commit(); err != nil {
				return err
			}
		}
	}
	return nil
}
