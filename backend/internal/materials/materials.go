package materials

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"campusclaw/internal/auth"
	"campusclaw/internal/knowledge"
)

type Material struct {
	ID         string    `json:"id"`
	ClassID    int64     `json:"class_id"`
	UploaderID int64     `json:"uploader_id"`
	Uploader   string    `json:"uploader"`
	Title      string    `json:"title"`
	Filename   string    `json:"original_filename"`
	CreatedAt  time.Time `json:"created_at"`
	Content    string    `json:"content"`
	StorageKey string    `json:"-"`
	Status     string    `json:"-"`
}
type Service struct {
	DB       *sql.DB
	Dir      string
	MaxBytes int64
	Clean    atomic.Bool
}

func RandomKey() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func SafeKey(key string) bool {
	return key != "" && filepath.Base(key) == key && !strings.ContainsAny(key, "/\\") && key != "." && key != ".."
}
func (s *Service) Body(ctx context.Context, id string) (string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT content FROM knowledge_entries WHERE material_id=? ORDER BY chunk_index", id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var part string
		if err = rows.Scan(&part); err != nil {
			return "", err
		}
		b.WriteString(part)
	}
	return b.String(), rows.Err()
}
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT m.id,m.class_id,m.uploader_id,u.username,m.title,m.original_filename,m.created_at FROM materials m JOIN users u ON u.id=m.uploader_id WHERE m.class_id=? AND m.status='ready' ORDER BY m.created_at DESC,m.id`, *u.ClassID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	items := []Material{}
	for rows.Next() {
		var m Material
		if err = rows.Scan(&m.ID, &m.ClassID, &m.UploaderID, &m.Uploader, &m.Title, &m.Filename, &m.CreatedAt); err != nil {
			break
		}
		items = append(items, m)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := []Material{}
	for _, m := range items {
		m.Content, err = s.Body(r.Context(), m.ID)
		if err != nil {
			auth.Error(w, 503, "service_unavailable")
			return
		}
		if query == "" || strings.Contains(strings.ToLower(m.Title), query) || strings.Contains(strings.ToLower(m.Content), query) {
			filtered = append(filtered, m)
		}
	}
	auth.JSON(w, 200, map[string]any{"materials": filtered})
}
func (s *Service) Find(w http.ResponseWriter, r *http.Request) (*Material, bool) {
	var m Material
	err := s.DB.QueryRowContext(r.Context(), `SELECT m.id,m.class_id,m.uploader_id,u.username,m.title,m.original_filename,m.created_at,m.storage_key,m.status FROM materials m JOIN users u ON u.id=m.uploader_id WHERE m.id=?`, r.PathValue("id")).Scan(&m.ID, &m.ClassID, &m.UploaderID, &m.Uploader, &m.Title, &m.Filename, &m.CreatedAt, &m.StorageKey, &m.Status)
	reason := "missing"
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		auth.Error(w, 503, "service_unavailable")
		return nil, false
	}
	if err != nil || m.ClassID != *auth.Current(r).ClassID || m.Status != "ready" {
		if err == nil {
			reason = "class_or_state_denied"
		}
		slog.Info("material_access_denied", "actor", auth.Current(r).ID, "resource", r.PathValue("id"), "operation", r.Method, "reason", reason)
		auth.Error(w, 404, "not_found")
		return nil, false
	}
	return &m, true
}
func (s *Service) Detail(w http.ResponseWriter, r *http.Request) {
	m, ok := s.Find(w, r)
	if !ok {
		return
	}
	body, err := s.Body(r.Context(), m.ID)
	if err != nil {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	m.Content = body
	auth.JSON(w, 200, m)
}
func (s *Service) Download(w http.ResponseWriter, r *http.Request) {
	m, ok := s.Find(w, r)
	if !ok {
		return
	}
	if !SafeKey(m.StorageKey) {
		auth.Error(w, 404, "not_found")
		return
	}
	f, err := os.Open(filepath.Join(s.Dir, m.StorageKey))
	if err != nil {
		auth.Error(w, 404, "not_found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		auth.Error(w, 404, "not_found")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": m.Filename}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, m.Filename, info.ModTime(), f)
}
func Validate(filename, contentType string, data []byte) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".txt" && ext != ".md" {
		return errors.New("unsupported_file")
	}
	if !utf8.Valid(data) || len(data) == 0 || strings.TrimSpace(string(data)) == "" {
		return errors.New("invalid_text")
	}
	for _, ch := range string(data) {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\r' && ch != '\t' {
			return errors.New("invalid_text")
		}
	}
	for _, prefix := range []string{"%PDF-", "PK\x03\x04", "MZ", "\x89PNG", "GIF87a", "GIF89a"} {
		if strings.HasPrefix(string(data), prefix) {
			return errors.New("invalid_text")
		}
	}
	ct, _, err := mime.ParseMediaType(contentType)
	if contentType != "" && err != nil {
		return errors.New("invalid_content_type")
	}
	if ct != "" && ct != "application/octet-stream" && ct != "text/plain" && !(ext == ".md" && (ct == "text/markdown" || ct == "text/x-markdown")) {
		return errors.New("invalid_content_type")
	}
	if len([]rune(filename)) > 255 {
		return errors.New("invalid_filename")
	}
	return nil
}
func (s *Service) Upload(w http.ResponseWriter, r *http.Request) {
	if !s.Clean.Load() {
		auth.Error(w, 503, "service_unavailable")
		return
	}
	// Allow bounded multipart headers/fields in addition to the configured file bytes.
	capBytes := s.MaxBytes + 65536
	if r.ContentLength > capBytes {
		auth.Error(w, 413, "file_too_large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, capBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		auth.Error(w, 400, "invalid_upload")
		return
	}
	var filename, ct string
	var data []byte
	seen := false
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(e, &tooLarge) {
				auth.Error(w, 413, "file_too_large")
			} else {
				auth.Error(w, 400, "invalid_upload")
			}
			return
		}
		if part.FormName() == "file" {
			if seen || part.FileName() == "" {
				part.Close()
				auth.Error(w, 400, "invalid_upload")
				return
			}
			seen = true
			filename = part.FileName()
			ct = part.Header.Get("Content-Type")
			data, e = io.ReadAll(io.LimitReader(part, s.MaxBytes+1))
			if int64(len(data)) > s.MaxBytes {
				part.Close()
				auth.Error(w, 413, "file_too_large")
				return
			}
		} else {
			_, e = io.Copy(io.Discard, part)
		}
		part.Close()
		if e != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(e, &tooLarge) {
				auth.Error(w, 413, "file_too_large")
			} else {
				auth.Error(w, 400, "invalid_upload")
			}
			return
		}
	}
	if !seen {
		auth.Error(w, 400, "invalid_upload")
		return
	}
	if err = Validate(filename, ct, data); err != nil {
		auth.Error(w, 400, err.Error())
		return
	}
	id, err := RandomKey()
	if err != nil {
		auth.Error(w, 500, "internal_error")
		return
	}
	m := Material{ID: id, ClassID: *auth.Current(r).ClassID, UploaderID: auth.Current(r).ID, Title: filename, Filename: filename, StorageKey: id, Status: "ready"}
	if err = s.Create(r.Context(), m, data); err != nil {
		if errors.Is(err, errStorage) {
			auth.Error(w, 500, "storage_failed")
		} else {
			auth.Error(w, 503, "service_unavailable")
		}
		return
	}
	auth.JSON(w, 201, map[string]string{"id": id, "status": "ready"})
}

var errStorage = errors.New("storage failure")

func (s *Service) Create(ctx context.Context, m Material, data []byte) error {
	if !SafeKey(m.StorageKey) {
		return errStorage
	}
	temp, err := os.CreateTemp(s.Dir, ".pending-")
	if err != nil {
		return errStorage
	}
	tempName := temp.Name()
	finalName := filepath.Join(s.Dir, m.StorageKey)
	moved := false
	committed := false
	defer func() {
		temp.Close()
		if !committed {
			target := tempName
			if moved {
				target = finalName
			}
			if e := os.Remove(target); e != nil && !os.IsNotExist(e) {
				s.Clean.Store(false)
				slog.Error("upload_cleanup_failed")
			}
		}
	}()
	if _, err = temp.Write(data); err != nil {
		return errStorage
	}
	if err = temp.Sync(); err != nil {
		return errStorage
	}
	if err = temp.Close(); err != nil {
		return errStorage
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key,status,created_at) VALUES(?,?,?,?,?,?,'ready',?)`, m.ID, m.ClassID, m.UploaderID, m.Title, m.Filename, m.StorageKey, m.CreatedAt)
	if err != nil {
		return err
	}
	for i, chunk := range knowledge.Split(string(data), 2000) {
		if _, err = tx.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES(?,?,?,?)", m.ID, m.ClassID, i, chunk); err != nil {
			return err
		}
	}
	if _, err = os.Lstat(finalName); err == nil || !os.IsNotExist(err) {
		return errStorage
	}
	if err = os.Rename(tempName, finalName); err != nil {
		return errStorage
	}
	moved = true
	if err = tx.Commit(); err != nil {
		// A failed commit can be ambiguous. Preserve the file until startup reconciliation
		// instead of deleting a file that may have a committed record.
		committed = true
		s.Clean.Store(false)
		slog.Error("upload_commit_uncertain")
		return err
	}
	committed = true
	return nil
}
func (s *Service) Reconcile(ctx context.Context) error {
	s.Clean.Store(false)
	rows, err := s.DB.QueryContext(ctx, "SELECT storage_key FROM materials WHERE status='ready'")
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		if !SafeKey(key) {
			rows.Close()
			return errStorage
		}
		keys[key] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return errStorage
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return errStorage
		}
		if !keys[entry.Name()] {
			if err = os.Remove(filepath.Join(s.Dir, entry.Name())); err != nil {
				return errStorage
			}
		}
	}
	for key := range keys {
		info, e := os.Lstat(filepath.Join(s.Dir, key))
		if e != nil || !info.Mode().IsRegular() {
			return errStorage
		}
	}
	s.Clean.Store(true)
	return nil
}
func (s *Service) Ready(ctx context.Context) bool {
	if !s.Clean.Load() || s.DB.PingContext(ctx) != nil {
		return false
	}
	f, err := os.CreateTemp(s.Dir, ".probe-")
	if err != nil {
		return false
	}
	name := f.Name()
	_, writeErr := fmt.Fprint(f, "ok")
	closeErr := f.Close()
	removeErr := os.Remove(name)
	return writeErr == nil && closeErr == nil && removeErr == nil
}
