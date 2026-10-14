package tutor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

var ErrNotFound = errors.New("not_found")
var ErrInvalid = errors.New("invalid_agent")
var ErrDuplicate = errors.New("duplicate_name")
var ErrNoAgents = errors.New("no_available_agents")

const DefaultPrompt = "你是本班学习助手。用清楚的中文回应学生，帮助学生理解问题；只在有真实资料依据时标注出处，不编造来源。"

type Service struct{ DB *sql.DB }
type Agent struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Prompt      string   `json:"prompt"`
	Enabled     bool     `json:"enabled"`
	Version     int64    `json:"version"`
	SkillIDs    []int64  `json:"skill_ids"`
	MaterialIDs []string `json:"material_ids"`
}
type AgentInput struct {
	Name        string   `json:"name"`
	Prompt      string   `json:"prompt"`
	Enabled     bool     `json:"enabled"`
	SkillIDs    []int64  `json:"skill_ids"`
	MaterialIDs []string `json:"material_ids"`
}

type Querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func LoadAgent(ctx context.Context, q Querier, classID, id int64) (Agent, error) {
	a := Agent{SkillIDs: []int64{}, MaterialIDs: []string{}}
	err := q.QueryRowContext(ctx, "SELECT id,name,prompt,enabled,version FROM tutor_agents WHERE id=? AND class_id=?", id, classID).Scan(&a.ID, &a.Name, &a.Prompt, &a.Enabled, &a.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	rows, err := q.QueryContext(ctx, "SELECT skill_id FROM tutor_agent_skills WHERE agent_id=? AND class_id=? ORDER BY skill_id", id, classID)
	if err != nil {
		return a, err
	}
	for rows.Next() {
		var v int64
		if err = rows.Scan(&v); err != nil {
			break
		}
		a.SkillIDs = append(a.SkillIDs, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return a, err
	}
	rows, err = q.QueryContext(ctx, "SELECT material_id FROM tutor_agent_materials WHERE agent_id=? AND class_id=? ORDER BY material_id", id, classID)
	if err != nil {
		return a, err
	}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			break
		}
		a.MaterialIDs = append(a.MaterialIDs, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return a, err
}

func BoundSkills(ctx context.Context, q Querier, classID, agentID int64) ([]Skill, error) {
	return loadSkills(ctx, q, `SELECT s.id,s.name,s.trigger_text,s.subject_text,s.description,s.tools_json,s.source_text,s.enabled FROM skills s JOIN tutor_agent_skills b ON b.skill_id=s.id AND b.class_id=s.class_id WHERE b.agent_id=? AND s.class_id=? AND s.enabled=1 ORDER BY s.id`, agentID, classID)
}

func loadSkills(ctx context.Context, q Querier, query string, args ...any) ([]Skill, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Skill{}
	for rows.Next() {
		var s Skill
		var raw string
		if err = rows.Scan(&s.ID, &s.Name, &s.TriggerText, &s.SubjectText, &s.Description, &raw, &s.SourceText, &s.Enabled); err != nil {
			return nil, err
		}
		s.Tools = decodeTools(raw)
		list = append(list, s)
	}
	return list, rows.Err()
}

func (s *Service) ListAgents(ctx context.Context, classID int64, teacher bool) ([]Agent, error) {
	query := "SELECT id FROM tutor_agents WHERE class_id=?"
	if !teacher {
		query += " AND enabled=1"
	}
	query += " ORDER BY id"
	rows, err := s.DB.QueryContext(ctx, query, classID)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	list := []Agent{}
	for _, id := range ids {
		a, e := LoadAgent(ctx, s.DB, classID, id)
		if e != nil {
			return nil, e
		}
		if !teacher {
			a.Prompt = ""
		}
		list = append(list, a)
	}
	return list, nil
}

func (s *Service) SaveAgent(ctx context.Context, classID, id int64, input AgentInput) (Agent, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 64 || !utf8.ValidString(input.Name) || !utf8.ValidString(input.Prompt) || utf8.RuneCountInString(input.Prompt) > 2000 || len(input.SkillIDs) > 256 || len(input.MaterialIDs) > 256 {
		return Agent{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback()
	if id != 0 {
		var exists int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM tutor_agents WHERE id=? AND class_id=? FOR UPDATE", id, classID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		if err != nil {
			return Agent{}, err
		}
	}
	skills := map[int64]bool{}
	materials := map[string]bool{}
	for _, sid := range input.SkillIDs {
		var exists int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM skills WHERE id=? AND class_id=? FOR SHARE", sid, classID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrNotFound
		} else if err != nil {
			return Agent{}, err
		}
		skills[sid] = true
	}
	for _, mid := range input.MaterialIDs {
		var exists string
		if err = tx.QueryRowContext(ctx, "SELECT id FROM materials WHERE id=? AND class_id=? AND status='ready' FOR SHARE", mid, classID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrNotFound
		} else if err != nil {
			return Agent{}, err
		}
		materials[mid] = true
	}
	if id == 0 {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "INSERT INTO tutor_agents(class_id,name,prompt,enabled,version) VALUES(?,?,?,?,1)", classID, input.Name, input.Prompt, input.Enabled)
		if err == nil {
			id, err = result.LastInsertId()
		}
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE tutor_agents SET name=?,prompt=?,enabled=?,version=version+1 WHERE id=? AND class_id=?", input.Name, input.Prompt, input.Enabled, id, classID)
	}
	if err != nil {
		return Agent{}, classify(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM tutor_agent_skills WHERE agent_id=?", id); err != nil {
		return Agent{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM tutor_agent_materials WHERE agent_id=?", id); err != nil {
		return Agent{}, err
	}
	skillIDs := []int64{}
	for k := range skills {
		skillIDs = append(skillIDs, k)
	}
	sort.Slice(skillIDs, func(i, j int) bool { return skillIDs[i] < skillIDs[j] })
	for _, sid := range skillIDs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO tutor_agent_skills(agent_id,skill_id,class_id) VALUES(?,?,?)", id, sid, classID); err != nil {
			return Agent{}, err
		}
	}
	for mid := range materials {
		if _, err = tx.ExecContext(ctx, "INSERT INTO tutor_agent_materials(agent_id,material_id,class_id) VALUES(?,?,?)", id, mid, classID); err != nil {
			return Agent{}, err
		}
	}
	a, err := LoadAgent(ctx, tx, classID, id)
	if err != nil {
		return a, err
	}
	if err = tx.Commit(); err != nil {
		return Agent{}, err
	}
	return a, nil
}

func classify(err error) error {
	var m *mysql.MySQLError
	if errors.As(err, &m) && m.Number == 1062 {
		return ErrDuplicate
	}
	return err
}

func (s *Service) ListSkills(ctx context.Context, classID int64) ([]Skill, error) {
	return loadSkills(ctx, s.DB, "SELECT id,name,trigger_text,subject_text,description,tools_json,source_text,enabled FROM skills WHERE class_id=? ORDER BY id", classID)
}
func (s *Service) Skill(ctx context.Context, classID, id int64) (Skill, error) {
	list, err := loadSkills(ctx, s.DB, "SELECT id,name,trigger_text,subject_text,description,tools_json,source_text,enabled FROM skills WHERE class_id=? AND id=?", classID, id)
	if err != nil {
		return Skill{}, err
	}
	if len(list) == 0 {
		return Skill{}, ErrNotFound
	}
	return list[0], nil
}
func (s *Service) SaveSkill(ctx context.Context, classID, id int64, skill Skill) (Skill, error) {
	tools, _ := json.Marshal(skill.Tools)
	var err error
	if id == 0 {
		var result sql.Result
		result, err = s.DB.ExecContext(ctx, "INSERT INTO skills(class_id,name,trigger_text,subject_text,description,tools_json,source_text,enabled) VALUES(?,?,?,?,?,?,?,?)", classID, skill.Name, skill.TriggerText, skill.SubjectText, skill.Description, string(tools), skill.SourceText, skill.Enabled)
		if err == nil {
			id, err = result.LastInsertId()
		}
	} else {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return Skill{}, e
		}
		defer tx.Rollback()
		var exists int64
		e = tx.QueryRowContext(ctx, "SELECT id FROM skills WHERE id=? AND class_id=? FOR UPDATE", id, classID).Scan(&exists)
		if errors.Is(e, sql.ErrNoRows) {
			return Skill{}, ErrNotFound
		}
		if e != nil {
			return Skill{}, e
		}
		_, err = tx.ExecContext(ctx, "UPDATE skills SET name=?,trigger_text=?,subject_text=?,description=?,tools_json=?,source_text=?,enabled=? WHERE id=? AND class_id=?", skill.Name, skill.TriggerText, skill.SubjectText, skill.Description, string(tools), skill.SourceText, skill.Enabled, id, classID)
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		return Skill{}, classify(err)
	}
	skill.ID = id
	return skill, nil
}
func (s *Service) DeleteSkill(ctx context.Context, classID, id int64) error {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM skills WHERE id=? AND class_id=?", id, classID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
