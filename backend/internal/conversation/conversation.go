// Package conversation owns student conversations, the per-turn transaction
// and bounded model history. Authorization always derives owner and class
// from the authenticated user; conversation IDs are opaque random strings.
package conversation

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"campusclaw/internal/tutor"
)

// NeutralTitle is used until the first question provides a bounded title.
const NeutralTitle = "新的提问"

// TitleMaxChars bounds generated conversation titles.
const TitleMaxChars = 24

var (
	ErrNotFound     = errors.New("conversation not found")
	ErrForbidden    = errors.New("conversation owned by another student")
	ErrBusy         = errors.New("conversation_busy")
	ErrNoPrompt     = errors.New("prompt_not_configured")
	ErrStaleTurn    = errors.New("turn no longer streaming")
	ErrTurnConflict = errors.New("turn state changed concurrently")
)

type Service struct {
	DB *sql.DB
	// Orch drives the guided streaming turn; nil in pure CRUD contexts.
	Orch *Orchestrator
}

type Conversation struct {
	AgentID   int64  `json:"agent_id"`
	AgentName string `json:"agent_name"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Citation struct {
	Ordinal       int    `json:"ordinal"`
	MaterialID    string `json:"material_id"`
	MaterialTitle string `json:"material_title"`
	ChunkID       int64  `json:"chunk_id"`
	ChunkIndex    int    `json:"chunk_index"`
	CharStart     int    `json:"char_start"`
	CharEnd       int    `json:"char_end"`
	Excerpt       string `json:"excerpt"`
}

type Message struct {
	SkillName   string     `json:"skill_name,omitempty"`
	ID          string     `json:"id"`
	SequenceNo  int        `json:"sequence_no"`
	Role        string     `json:"role"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	ErrorCode   string     `json:"error_code,omitempty"`
	CreatedAt   string     `json:"created_at"`
	CompletedAt string     `json:"completed_at,omitempty"`
	Citations   []Citation `json:"citations,omitempty"`
}

type Turn struct {
	Citations          []Citation
	AgentID            int64
	Skill              *tutor.Skill
	MaterialIDs        []string
	ConversationID     string
	UserMessageID      string
	AssistantMessageID string
	SequenceNo         int
	PromptBody         string
	PromptVersion      int64
	SkillEnabled       bool
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func boundedTitle(question string) string {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return NeutralTitle
	}
	collapsed := strings.Join(strings.Fields(trimmed), " ")
	runes := []rune(collapsed)
	if len(runes) > TitleMaxChars {
		collapsed = string(runes[:TitleMaxChars])
	}
	return collapsed
}

// Create makes a new conversation owned by the student in their own class.
func (s *Service) Create(ctx context.Context, ownerID, classID int64, agentIDs ...int64) (Conversation, error) {
	var agentID int64
	if len(agentIDs) > 0 {
		agentID = agentIDs[0]
	} else {
		_ = s.DB.QueryRowContext(ctx, "SELECT id FROM tutor_agents WHERE class_id=? AND seed_key='legacy-default'", classID).Scan(&agentID)
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM tutor_agents WHERE class_id=? AND enabled=1", classID).Scan(&count); err != nil {
		return Conversation{}, err
	}
	if count == 0 {
		return Conversation{}, tutor.ErrNoAgents
	}
	agent, err := tutor.LoadAgent(ctx, s.DB, classID, agentID)
	if err != nil || !agent.Enabled {
		if err == nil || errors.Is(err, tutor.ErrNotFound) {
			return Conversation{}, tutor.ErrNotFound
		}
		return Conversation{}, err
	}
	id := newID()
	now := time.Now().UTC()
	if _, err := s.DB.ExecContext(ctx,
		"INSERT INTO conversations(id,owner_user_id,class_id,agent_id,title,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
		id, ownerID, classID, agentID, NeutralTitle, now, now,
	); err != nil {
		return Conversation{}, err
	}
	return Conversation{ID: id, AgentID: agentID, AgentName: agent.Name, Title: NeutralTitle, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}, nil
}

// List returns the student's conversations, most recently updated first.
func (s *Service) List(ctx context.Context, ownerID int64, classIDs ...int64) ([]Conversation, error) {
	query := "SELECT c.id,c.title,c.created_at,c.updated_at,c.agent_id,a.name FROM conversations c JOIN tutor_agents a ON a.id=c.agent_id AND a.class_id=c.class_id WHERE c.owner_user_id=?"
	args := []any{ownerID}
	if len(classIDs) > 0 {
		query += " AND c.class_id=?"
		args = append(args, classIDs[0])
	}
	query += " ORDER BY c.updated_at DESC,c.id DESC LIMIT 100"
	rows, err := s.DB.QueryContext(ctx,
		query, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var created, updated time.Time
		if err := rows.Scan(&c.ID, &c.Title, &created, &updated, &c.AgentID, &c.AgentName); err != nil {
			return nil, err
		}
		c.CreatedAt = created.Format(time.RFC3339)
		c.UpdatedAt = updated.Format(time.RFC3339)
		out = append(out, c)
	}
	return out, rows.Err()
}

// loadOwned fetches a conversation that must belong to the student. Any
// unknown, cross-class or foreign-owner id maps to the same 404-shaped error.
func (s *Service) loadOwned(ctx context.Context, id string, ownerID, classID int64) (Conversation, error) {
	var c Conversation
	var created, updated time.Time
	err := s.DB.QueryRowContext(ctx,
		"SELECT c.id,c.title,c.created_at,c.updated_at,c.agent_id,a.name FROM conversations c JOIN tutor_agents a ON a.id=c.agent_id AND a.class_id=c.class_id WHERE c.id=? AND c.owner_user_id=? AND c.class_id=?",
		id, ownerID, classID,
	).Scan(&c.ID, &c.Title, &created, &updated, &c.AgentID, &c.AgentName)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, err
	}
	c.CreatedAt = created.Format(time.RFC3339)
	c.UpdatedAt = updated.Format(time.RFC3339)
	return c, nil
}

// LoadDetail returns the conversation with ordered messages and citation
// snapshots for the owning student only.
func (s *Service) LoadDetail(ctx context.Context, id string, ownerID, classID int64) (Conversation, []Message, error) {
	c, err := s.loadOwned(ctx, id, ownerID, classID)
	if err != nil {
		return Conversation{}, nil, err
	}
	rows, err := s.DB.QueryContext(ctx,
		"SELECT id,sequence_no,role,content,status,error_code,created_at,completed_at,COALESCE(skill_name,'') FROM conversation_messages WHERE conversation_id=? ORDER BY sequence_no",
		id,
	)
	if err != nil {
		return Conversation{}, nil, err
	}
	defer rows.Close()
	messages := []Message{}
	for rows.Next() {
		var m Message
		var errorCode sql.NullString
		var completed sql.NullTime
		var created time.Time
		if err := rows.Scan(&m.ID, &m.SequenceNo, &m.Role, &m.Content, &m.Status, &errorCode, &created, &completed, &m.SkillName); err != nil {
			return Conversation{}, nil, err
		}
		m.ErrorCode = errorCode.String
		m.CreatedAt = created.Format(time.RFC3339)
		if completed.Valid {
			m.CompletedAt = completed.Time.Format(time.RFC3339)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return Conversation{}, nil, err
	}
	if err := s.attachCitations(ctx, messages); err != nil {
		return Conversation{}, nil, err
	}
	return c, messages, nil
}

func (s *Service) attachCitations(ctx context.Context, messages []Message) error {
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		rows, err := s.DB.QueryContext(ctx,
			"SELECT ordinal,material_id,title_snapshot,chunk_id,chunk_index,char_start,char_end,excerpt_snapshot FROM message_citations WHERE assistant_message_id=? ORDER BY ordinal",
			messages[i].ID,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c Citation
			if err := rows.Scan(&c.Ordinal, &c.MaterialID, &c.MaterialTitle, &c.ChunkID, &c.ChunkIndex, &c.CharStart, &c.CharEnd, &c.Excerpt); err != nil {
				rows.Close()
				return err
			}
			messages[i].Citations = append(messages[i].Citations, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

// StartTurn runs the turn-start transaction: lock the owned conversation,
// reject an active generation, snapshot prompt version and skill state,
// insert consecutive user/assistant rows and set the active lock. A missing
// prompt aborts before any row exists.
func (s *Service) StartTurn(ctx context.Context, conversationID string, ownerID, classID int64, question, model string) (Turn, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Turn{}, err
	}
	defer tx.Rollback()

	var ownerFromRow, classFromRow, agentID int64
	err = tx.QueryRowContext(ctx,
		"SELECT owner_user_id,class_id,agent_id FROM conversations WHERE id=? FOR UPDATE", conversationID,
	).Scan(&ownerFromRow, &classFromRow, &agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return Turn{}, ErrNotFound
	}
	if err != nil {
		return Turn{}, err
	}
	if ownerFromRow != ownerID || classFromRow != classID {
		return Turn{}, ErrNotFound
	}
	var active sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT active_message_id FROM conversations WHERE id=?", conversationID).Scan(&active); err != nil {
		return Turn{}, err
	}
	if active.Valid && active.String != "" {
		return Turn{}, ErrBusy
	}
	agent, err := tutor.LoadAgent(ctx, tx, classID, agentID)
	if errors.Is(err, tutor.ErrNotFound) {
		return Turn{}, ErrNotFound
	}
	if err != nil {
		return Turn{}, err
	}
	skills, err := tutor.BoundSkills(ctx, tx, classID, agentID)
	if err != nil {
		return Turn{}, err
	}
	selected := tutor.SelectSkill(skills, question)
	promptBody := agent.Prompt
	if strings.TrimSpace(promptBody) == "" {
		promptBody = tutor.DefaultPrompt
	}
	promptVersion := agent.Version
	guided := selected != nil && strings.Contains(selected.Name, "解题引导")
	var skillID *int64
	skillName := ""
	if selected != nil {
		skillID = &selected.ID
		skillName = selected.Name
	}
	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		"SELECT MAX(sequence_no) FROM conversation_messages WHERE conversation_id=?", conversationID,
	).Scan(&maxSeq); err != nil {
		return Turn{}, err
	}
	next := int(maxSeq.Int64) + 1
	userID := newID()
	assistantID := newID()
	questionText := strings.TrimSpace(question)
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status,created_at,completed_at) VALUES(?,?,?,'user',?,'completed',?,NOW(6))",
		userID, conversationID, next, questionText, time.Now().UTC(),
	); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO conversation_messages(id,conversation_id,sequence_no,role,content,status,prompt_version,skill_enabled,model,created_at,skill_id,skill_name) VALUES(?,?,?,'assistant','','streaming',?,?,?,NOW(6),?,NULLIF(?,''))",
		assistantID, conversationID, next+1, promptVersion, boolToInt(guided), model, skillID, skillName,
	); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE conversations SET active_message_id=?,updated_at=NOW(6) WHERE id=?", assistantID, conversationID,
	); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE conversations SET title=? WHERE id=? AND title=?", boundedTitle(question), conversationID, NeutralTitle,
	); err != nil {
		return Turn{}, err
	}
	if err := tx.Commit(); err != nil {
		return Turn{}, err
	}
	return Turn{
		ConversationID: conversationID, UserMessageID: userID, AssistantMessageID: assistantID,
		SequenceNo: next + 1, PromptBody: promptBody, PromptVersion: promptVersion, SkillEnabled: guided, AgentID: agentID, Skill: selected, MaterialIDs: agent.MaterialIDs,
	}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CompleteTurn records a successful answer with validated citation snapshots
// and clears only its own active lock. The guarded update touches exactly the
// still-streaming row, so stale completions cannot overwrite another turn.
func (s *Service) CompleteTurn(ctx context.Context, turn Turn, content string, citations []Citation) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		"UPDATE conversation_messages SET content=?,status='completed',completed_at=NOW(6) WHERE id=? AND conversation_id=? AND status='streaming'",
		content, turn.AssistantMessageID, turn.ConversationID,
	)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrStaleTurn
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM message_citations WHERE assistant_message_id=?", turn.AssistantMessageID); err != nil {
		return err
	}
	for _, c := range citations {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO message_citations(assistant_message_id,ordinal,material_id,chunk_id,title_snapshot,chunk_index,char_start,char_end,excerpt_snapshot) VALUES(?,?,?,?,?,?,?,?,?)",
			turn.AssistantMessageID, c.Ordinal, c.MaterialID, c.ChunkID, c.MaterialTitle, c.ChunkIndex, c.CharStart, c.CharEnd, c.Excerpt,
		); err != nil {
			return err
		}
	}
	if err := clearLock(ctx, tx, turn); err != nil {
		return err
	}
	return tx.Commit()
}

// FailTurn records a terminal failed or cancelled state with bounded partial
// text and clears only its own active lock.
func (s *Service) FailTurn(ctx context.Context, turn Turn, partial string, errorCode string, cancelled bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := "failed"
	if cancelled {
		status = "cancelled"
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE conversation_messages SET content=?,status=?,error_code=?,completed_at=NOW(6) WHERE id=? AND conversation_id=? AND status='streaming'",
		partial, status, errorCode, turn.AssistantMessageID, turn.ConversationID,
	)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrStaleTurn
	}
	if err := clearLock(ctx, tx, turn); err != nil {
		return err
	}
	return tx.Commit()
}

func clearLock(ctx context.Context, tx *sql.Tx, turn Turn) error {
	res, err := tx.ExecContext(ctx,
		"UPDATE conversations SET active_message_id=NULL WHERE id=? AND active_message_id=?", turn.ConversationID, turn.AssistantMessageID,
	)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrTurnConflict
	}
	_, err = tx.ExecContext(ctx, "UPDATE conversations SET updated_at=NOW(6) WHERE id=?", turn.ConversationID)
	return err
}

// HistoryMessage is one bounded-context message passed to the model.
type HistoryMessage struct {
	Role    string
	Content string
}

// History returns the bounded model context: the latest completed messages of
// this conversation, capped by count and Unicode characters, in chronological
// order. Failed or cancelled text never enters the result.
func (s *Service) History(ctx context.Context, conversationID string, maxMessages, maxChars int) ([]HistoryMessage, error) {
	maxMessages = 6
	maxChars = 500
	rows, err := s.DB.QueryContext(ctx,
		"SELECT role,content FROM conversation_messages WHERE conversation_id=? AND status='completed' AND content<>'' ORDER BY sequence_no DESC LIMIT ?",
		conversationID, maxMessages,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pair struct{ role, content string }
	var newest []pair
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		if role != "user" && role != "assistant" {
			continue
		}
		if utf8.RuneCountInString(content) > maxChars {
			content = string([]rune(content)[:maxChars])
		}
		newest = append(newest, pair{role, content})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]HistoryMessage, 0, len(newest))
	for i := len(newest) - 1; i >= 0; i-- {
		out = append(out, HistoryMessage{Role: newest[i].role, Content: newest[i].content})
	}
	return out, nil
}
