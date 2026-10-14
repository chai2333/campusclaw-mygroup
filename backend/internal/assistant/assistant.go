// Package assistant stores the per-class teacher prompt and skill
// configuration and serves the teacher-only management endpoints. The
// request context is the only source of class identity; bodies never carry
// class_id.
package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/internal/auth"
)

// SkillKey is the only skill this lesson supports.
const SkillKey = "guided_problem_solving"

// MaxPromptChars bounds the teacher prompt in Unicode characters.
const MaxPromptChars = 12000

type Service struct {
	DB *sql.DB
}

type promptState struct {
	Configured bool   `json:"configured"`
	Content    string `json:"content,omitempty"`
	Version    int64  `json:"version,omitempty"`
	UpdatedBy  *int64 `json:"updated_by,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type skillState struct {
	SkillKey  string `json:"skill_key"`
	Enabled   bool   `json:"enabled"`
	UpdatedBy *int64 `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// LoadPrompt returns the stored prompt for the class. found=false means the
// teacher has not saved a prompt yet.
func (s *Service) LoadPrompt(ctx context.Context, classID int64) (body string, version int64, updatedBy *int64, updatedAt string, found bool, err error) {
	var scannedBody sql.NullString
	var scannedUpdated sql.NullString
	var scannedBy sql.NullInt64
	err = s.DB.QueryRowContext(ctx,
		"SELECT body,version,updated_by,updated_at FROM assistant_prompts WHERE class_id=?", classID,
	).Scan(&scannedBody, &version, &scannedBy, &scannedUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil, "", false, nil
	}
	if err != nil {
		return "", 0, nil, "", false, err
	}
	return scannedBody.String, version, byPointer(scannedBy), scannedUpdated.String, true, nil
}

// SavePrompt atomically inserts the first version or increments the version.
// Concurrent saves each receive a distinct version because the upsert locks
// the row. The returned version is read back after the write commits.
func (s *Service) SavePrompt(ctx context.Context, classID int64, actorID int64, content string) (int64, error) {
	if !utf8.ValidString(content) {
		return 0, errors.New("prompt content is not valid UTF-8")
	}
	if strings.TrimSpace(content) == "" {
		return 0, errors.New("prompt content must not be blank")
	}
	if utf8.RuneCountInString(content) > MaxPromptChars {
		return 0, errors.New("prompt content exceeds the character limit")
	}
	if _, err := s.DB.ExecContext(ctx,
		"INSERT INTO assistant_prompts(class_id,body,version,updated_by) VALUES(?,?,1,?) "+
			"ON DUPLICATE KEY UPDATE body=VALUES(body),version=version+1,updated_by=VALUES(updated_by)",
		classID, content, actorID,
	); err != nil {
		return 0, err
	}
	var version int64
	if err := s.DB.QueryRowContext(ctx,
		"SELECT version FROM assistant_prompts WHERE class_id=?", classID,
	).Scan(&version); err != nil {
		return 0, err
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE tutor_agents SET prompt=LEFT(?,2000),version=? WHERE class_id=? AND seed_key='legacy-default'", content, version, classID); err != nil {
		return 0, err
	}
	log.Printf("audit assistant_prompt_updated actor_id=%d class_id=%d version=%d", actorID, classID, version)
	return version, nil
}

// LoadSkill returns the skill state; a missing row falls back to the enabled
// default that startup repair would restore.
func (s *Service) LoadSkill(ctx context.Context, classID int64) (skillState, error) {
	state := skillState{SkillKey: SkillKey, Enabled: true}
	var enabled int
	var updated sql.NullString
	var by sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		"SELECT enabled,updated_by,updated_at FROM class_skills WHERE class_id=? AND skill_key=?", classID, SkillKey,
	).Scan(&enabled, &by, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Enabled = enabled == 1
	state.UpdatedBy = byPointer(by)
	state.UpdatedAt = updated.String
	return state, nil
}

// SaveSkill persists the enabled state and records the updating teacher.
func (s *Service) SaveSkill(ctx context.Context, classID int64, actorID int64, enabled bool) error {
	res, err := s.DB.ExecContext(ctx,
		"UPDATE class_skills SET enabled=?,updated_by=? WHERE class_id=? AND skill_key=?", boolInt(enabled), actorID, classID, SkillKey,
	)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		if _, err := s.DB.ExecContext(ctx,
			"INSERT INTO class_skills(class_id,skill_key,enabled,updated_by) VALUES(?,?,?,?)",
			classID, SkillKey, boolInt(enabled), actorID,
		); err != nil {
			return err
		}
	}
	log.Printf("audit assistant_skill_updated actor_id=%d class_id=%d skill=%s enabled=%t", actorID, classID, SkillKey, enabled)
	if _, err := s.DB.ExecContext(ctx, "UPDATE skills SET enabled=? WHERE class_id=? AND seed_key='legacy-guided'", boolInt(enabled), classID); err != nil {
		return err
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func byPointer(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func classIDOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	user := auth.Current(r)
	if user == nil || user.ClassID == nil {
		auth.Error(w, 403, "class_membership_required")
		return 0, false
	}
	return *user.ClassID, true
}

func decodeStrict(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		auth.Error(w, 400, "invalid_request_body")
		return false
	}
	return true
}

// @Route GET /api/assistant/prompt
// @Summary 兼容旧班级提示词读取（新会话使用智能体）
// @Role teacher
// @Response 200 Prompt
// @Deprecated true
func (s *Service) GetPrompt(w http.ResponseWriter, r *http.Request) {
	classID, ok := classIDOf(w, r)
	if !ok {
		return
	}
	body, version, updatedBy, updatedAt, found, err := s.LoadPrompt(r.Context(), classID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	state := promptState{Configured: found, Content: body, Version: version, UpdatedBy: updatedBy, UpdatedAt: updatedAt}
	auth.JSON(w, 200, state)
}

// @Route PUT /api/assistant/prompt
// @Summary 兼容旧提示词保存并同步默认智能体
// @Role teacher
// @Request PromptInput
// @Response 200 Prompt
// @Error 400 invalid_prompt / invalid_request_body
// @Deprecated true
func (s *Service) PutPrompt(w http.ResponseWriter, r *http.Request) {
	classID, ok := classIDOf(w, r)
	if !ok {
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if !decodeStrict(w, r, &input) {
		return
	}
	version, err := s.SavePrompt(r.Context(), classID, auth.Current(r).ID, input.Content)
	if err != nil {
		auth.Error(w, 400, "invalid_prompt")
		return
	}
	_, _, updatedBy, updatedAt, _, err := s.LoadPrompt(r.Context(), classID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	auth.JSON(w, 200, promptState{Configured: true, Content: input.Content, Version: version, UpdatedBy: updatedBy, UpdatedAt: updatedAt})
}

// @Route GET /api/assistant/skills/guided-problem-solving
// @Summary 兼容旧班级技能开关读取
// @Role teacher
// @Response 200 ClassSkill
// @Deprecated true
func (s *Service) GetSkill(w http.ResponseWriter, r *http.Request) {
	classID, ok := classIDOf(w, r)
	if !ok {
		return
	}
	state, err := s.LoadSkill(r.Context(), classID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	auth.JSON(w, 200, state)
}

// @Route PUT /api/assistant/skills/guided-problem-solving
// @Summary 兼容旧开关更新默认智能体技能
// @Role teacher
// @Request ClassSkillInput
// @Response 200 ClassSkill
// @Error 400 invalid_request_body
// @Deprecated true
func (s *Service) PutSkill(w http.ResponseWriter, r *http.Request) {
	classID, ok := classIDOf(w, r)
	if !ok {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeStrict(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		auth.Error(w, 400, "invalid_request_body")
		return
	}
	if err := s.SaveSkill(r.Context(), classID, auth.Current(r).ID, *input.Enabled); err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	state, err := s.LoadSkill(r.Context(), classID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	auth.JSON(w, 200, state)
}
