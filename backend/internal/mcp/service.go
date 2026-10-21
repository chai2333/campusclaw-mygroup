package mcp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
)

type InputError string

func (e InputError) Error() string { return string(e) }

type Service struct {
	DB                                            *sql.DB
	Client                                        Client
	Secret, DefaultToken, DefaultURL              string
	AllowedURLs                                   []string
	MaxRounds, MaxTurnCalls, MaxConversationCalls int
}
type Server struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"base_url"`
	TokenConfigured bool   `json:"token_configured"`
	Connected       bool   `json:"connected"`
	LastError       string `json:"last_error"`
	Version         int64  `json:"version"`
	tokenCipher     string
	lastTools       []Tool
}
type ServerInput struct {
	Name        string  `json:"name"`
	BaseURL     string  `json:"base_url"`
	AuthToken   *string `json:"auth_token,omitempty"`
	UseEnvToken bool    `json:"use_env_token,omitempty"`
}

func (s *Service) cipher() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("campusclaw-mcp-token:" + s.Secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s *Service) encrypt(token string) (string, error) {
	a, err := s.cipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(token), nil)), nil
}
func (s *Service) decrypt(encoded string) (string, error) {
	a, err := s.cipher()
	if err != nil {
		return "", ErrRemote
	}
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < a.NonceSize() {
		return "", ErrRemote
	}
	data, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], nil)
	if err != nil {
		return "", ErrRemote
	}
	return string(data), nil
}
func canonical(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || len(raw) > 512 {
		return "", InputError("Base URL须为不带凭据或查询参数的HTTP(S)地址")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func (s *Service) allowed(raw string) bool {
	for _, base := range s.AllowedURLs {
		if v, e := canonical(base); e == nil && v == raw {
			return true
		}
	}
	return false
}
func scanServer(row interface{ Scan(...any) error }) (Server, error) {
	var v Server
	var raw sql.NullString
	err := row.Scan(&v.ID, &v.Name, &v.BaseURL, &v.tokenCipher, &v.Connected, &v.LastError, &v.Version, &raw)
	v.TokenConfigured = v.tokenCipher != ""
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &v.lastTools)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

const serverColumns = "id,name,base_url,token_cipher,connected,last_error,version,last_tools_json"

func (s *Service) Get(ctx context.Context, classID, id int64) (Server, error) {
	return scanServer(s.DB.QueryRowContext(ctx, "SELECT "+serverColumns+" FROM mcp_servers WHERE class_id=? AND id=?", classID, id))
}
func (s *Service) List(ctx context.Context, classID int64) ([]Server, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+serverColumns+" FROM mcp_servers WHERE class_id=? ORDER BY id", classID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		v, e := scanServer(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) Save(ctx context.Context, classID, id int64, in ServerInput) (Server, error) {
	in.Name = strings.TrimSpace(in.Name)
	base, err := canonical(in.BaseURL)
	if err != nil {
		return Server{}, err
	}
	if !ServerName.MatchString(in.Name) {
		return Server{}, InputError("服务器名须以小写字母开头，最多48字符，只含小写字母、数字、下划线或连字符")
	}
	if !s.allowed(base) {
		return Server{}, InputError("此地址尚未加入服务端MCP_ALLOWED_BASE_URLS，请先由维护者配置")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback()
	token := ""
	if id > 0 {
		v, e := scanServer(tx.QueryRowContext(ctx, "SELECT "+serverColumns+" FROM mcp_servers WHERE class_id=? AND id=? FOR UPDATE", classID, id))
		if e != nil {
			return Server{}, e
		}
		token, e = s.decrypt(v.tokenCipher)
		if e != nil && in.AuthToken == nil && !in.UseEnvToken {
			return Server{}, InputError("服务端令牌密钥已改变，请重新配置令牌")
		}
	}
	if in.UseEnvToken {
		defaultURL, _ := canonical(s.DefaultURL)
		if base != defaultURL || s.DefaultToken == "" {
			return Server{}, InputError("此地址未配置本机环境令牌")
		}
		token = s.DefaultToken
	}
	if in.AuthToken != nil {
		token = *in.AuthToken
	}
	if len(token) < 16 || len(token) > 4096 || strings.ContainsAny(token, "\r\n") {
		return Server{}, InputError("须提供至少16字符且不含换行的专属Bearer令牌，或使用本机环境令牌")
	}
	encrypted, err := s.encrypt(token)
	if err != nil {
		return Server{}, err
	}
	if id == 0 {
		var res sql.Result
		res, err = tx.ExecContext(ctx, "INSERT INTO mcp_servers(class_id,name,base_url,token_cipher) VALUES(?,?,?,?)", classID, in.Name, base, encrypted)
		if err == nil {
			id, err = res.LastInsertId()
		}
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE mcp_servers SET name=?,base_url=?,token_cipher=?,connected=0,version=version+1,last_error='' WHERE class_id=? AND id=?", in.Name, base, encrypted, classID, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM mcp_tools WHERE server_id=? AND class_id=?", id, classID)
		}
	}
	if err != nil {
		var dup *mysql.MySQLError
		if errors.As(err, &dup) && dup.Number == 1062 {
			return Server{}, InputError("本班已有同名MCP服务器")
		}
		return Server{}, err
	}
	if err = tx.Commit(); err != nil {
		return Server{}, err
	}
	return s.Get(ctx, classID, id)
}
func (s *Service) Disconnect(ctx context.Context, classID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE mcp_servers SET connected=0,version=version+1,last_error='' WHERE class_id=? AND id=?", classID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mcp_tools WHERE server_id=? AND class_id=?", id, classID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Delete(ctx context.Context, classID, id int64) error {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM mcp_servers WHERE class_id=? AND id=?", classID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func definition(t Tool) string {
	t.Approved = false
	t.PreviousDescription = ""
	v, _ := json.Marshal(t)
	var canonical any
	_ = json.Unmarshal(v, &canonical)
	v, _ = json.Marshal(canonical)
	return string(v)
}
func (s *Service) Connect(ctx context.Context, classID, id int64) (Server, error) {
	if err := s.Disconnect(ctx, classID, id); err != nil {
		return Server{}, err
	}
	v, err := s.Get(ctx, classID, id)
	if err != nil {
		return v, err
	}
	token, err := s.decrypt(v.tokenCipher)
	var tools []Tool
	if err == nil {
		tools, err = s.Client.Discover(ctx, v.BaseURL, token)
	}
	for _, t := range tools {
		if strings.Contains(definition(t), token) || (s.Secret != "" && strings.Contains(definition(t), s.Secret)) {
			err = ErrRemote
		}
	}
	if err != nil {
		_, _ = s.DB.ExecContext(ctx, "UPDATE mcp_servers SET last_error=? WHERE class_id=? AND id=? AND version=?", ErrRemote.Error(), classID, id, v.Version)
		return Server{}, ErrRemote
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(ctx, "SELECT version FROM mcp_servers WHERE class_id=? AND id=? FOR UPDATE", classID, id).Scan(&version); err != nil || version != v.Version {
		return Server{}, ErrConflict
	}
	old := map[string]Tool{}
	for _, t := range v.lastTools {
		old[t.Name] = t
	}
	for i, t := range tools {
		previous, exists := old[t.Name]
		changed := exists && definition(previous) != definition(t)
		t.Approved = t.Safe() && (!exists || previous.Approved) && !changed
		if changed {
			t.PreviousDescription = previous.Description
			_, err = tx.ExecContext(ctx, "INSERT INTO mcp_tool_changes(server_id,class_id,server_name,tool_name,previous_json,current_json) VALUES(?,?,?,?,?,?)", id, classID, v.Name, t.Name, definition(previous), definition(t))
			if err != nil {
				return Server{}, err
			}
		}
		tools[i] = t
		if _, err = tx.ExecContext(ctx, "INSERT INTO mcp_tools(server_id,class_id,name,definition_json,approved) VALUES(?,?,?,?,?)", id, classID, t.Name, definition(t), t.Approved); err != nil {
			return Server{}, err
		}
	}
	raw, _ := json.Marshal(tools)
	if _, err = tx.ExecContext(ctx, "UPDATE mcp_servers SET connected=1,last_error='',last_tools_json=? WHERE class_id=? AND id=?", string(raw), classID, id); err != nil {
		return Server{}, err
	}
	if err = tx.Commit(); err != nil {
		return Server{}, err
	}
	return s.Get(ctx, classID, id)
}
func (s *Service) Tools(ctx context.Context, classID, id int64) ([]Tool, error) {
	server, err := s.Get(ctx, classID, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT definition_json,approved FROM mcp_tools WHERE class_id=? AND server_id=? ORDER BY name", classID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tool{}
	previous := map[string]string{}
	for _, t := range server.lastTools {
		previous[t.Name] = t.PreviousDescription
	}
	for rows.Next() {
		var raw string
		var approved bool
		if err = rows.Scan(&raw, &approved); err != nil {
			return nil, err
		}
		var t Tool
		if json.Unmarshal([]byte(raw), &t) != nil {
			return nil, ErrRemote
		}
		t.Approved = approved
		t.PreviousDescription = previous[t.Name]
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Service) Approve(ctx context.Context, classID, id int64, names []string) error {
	if len(names) == 0 || len(names) > 64 {
		return InputError("请选择要重新确认的工具")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	server, err := scanServer(tx.QueryRowContext(ctx, "SELECT "+serverColumns+" FROM mcp_servers WHERE class_id=? AND id=? FOR UPDATE", classID, id))
	if err != nil {
		return err
	}
	if !server.Connected {
		return InputError("请先连接服务器")
	}
	for _, name := range names {
		var raw string
		if tx.QueryRowContext(ctx, "SELECT definition_json FROM mcp_tools WHERE server_id=? AND class_id=? AND name=?", id, classID, name).Scan(&raw) != nil {
			return InputError("未发现此工具")
		}
		var t Tool
		if json.Unmarshal([]byte(raw), &t) != nil || !t.Safe() {
			return InputError("写入或外发工具需要逐次人工确认，本课仅开放只读提示工具")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE mcp_tools SET approved=1 WHERE server_id=? AND class_id=? AND name=?", id, classID, name); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE mcp_tool_changes SET approved_at=NOW(6) WHERE server_id=? AND class_id=? AND tool_name=? AND approved_at IS NULL", id, classID, name); err != nil {
			return err
		}
		for i := range server.lastTools {
			if server.lastTools[i].Name == name {
				server.lastTools[i].Approved = true
			}
		}
	}
	raw, _ := json.Marshal(server.lastTools)
	if _, err = tx.ExecContext(ctx, "UPDATE mcp_servers SET last_tools_json=? WHERE class_id=? AND id=?", string(raw), classID, id); err != nil {
		return err
	}
	return tx.Commit()
}

type Query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ValidateRefs uses the skill-save transaction's snapshot, never client class declarations.
func ValidateRefs(ctx context.Context, q Query, classID int64, refs []string) error {
	for _, ref := range refs {
		if ref == "knowledge_search" {
			continue
		}
		tool, server, ok := ParseRef(ref)
		if !ok {
			return InputError("工具须使用knowledge_search或formula_hint@campus-tools格式")
		}
		var found int64
		err := q.QueryRowContext(ctx, "SELECT s.id FROM mcp_servers s JOIN mcp_tools t ON t.server_id=s.id AND t.class_id=s.class_id WHERE s.class_id=? AND s.name=? AND s.connected=1 AND t.name=? AND t.approved=1 FOR SHARE", classID, server, tool).Scan(&found)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return InputError(fmt.Sprintf("工具引用%s不可用：须为本班已连接、已发现并确认的工具", ref))
		}
	}
	return nil
}
