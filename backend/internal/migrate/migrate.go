// Package migrate provides idempotent migration from the legacy SQLite
// data store to the new MySQL schema. It preserves user/class/material IDs,
// password hashes, uploader, timestamps, content order, and upload files.
// Old sessions are never migrated; old non-ready materials are reported as
// anomalies and excluded from the target database.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// slugMap converts legacy class slugs to the new schema's short slugs.
var slugMap = map[string]string{"class-a": "a", "class-b": "b"}

type Class struct {
	ID                  int64
	Slug, OldSlug, Name string
}

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	ClassID      int64
}

type Material struct {
	ID         string
	ClassID    int64
	UploaderID int64
	Title      string
	Filename   string
	StorageKey string
	Status     string
	CreatedAt  time.Time
}

type Chunk struct {
	MaterialID string
	ClassID    int64
	ChunkIndex int
	Content    string
}

type Report struct {
	Classes   int
	Users     int
	Materials int
	Chunks    int
	Files     int
	Anomalies []string
}

// Backup copies the SQLite database file and computes SHA-256 checksums for
// the database and every upload file, writing a manifest to manifestPath.
func Backup(ctx context.Context, sqlitePath, uploadsDir, backupDir, manifestPath string) error {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	dbBackup := filepath.Join(backupDir, filepath.Base(sqlitePath))
	if err := copyFile(sqlitePath, dbBackup); err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	manifest, err := os.Create(manifestPath)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	defer manifest.Close()
	dbSum, _ := checksum(sqlitePath)
	fmt.Fprintf(manifest, "database %s %s\n", filepath.Base(sqlitePath), dbSum)
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		return fmt.Errorf("read uploads: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(uploadsDir, e.Name())
		sum, _ := checksum(p)
		fmt.Fprintf(manifest, "file %s %s\n", e.Name(), sum)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Run performs the idempotent migration from SQLite to MySQL.
func Run(ctx context.Context, sqlitePath, uploadsDir string, mysql *sql.DB, targetUploadsDir string) (*Report, error) {
	src, err := sql.Open("sqlite", sqlitePath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer src.Close()
	r := &Report{}
	if err := migrateClasses(ctx, src, mysql, r); err != nil {
		return r, err
	}
	if err := migrateUsers(ctx, src, mysql, r); err != nil {
		return r, err
	}
	if err := migrateMaterials(ctx, src, mysql, r); err != nil {
		return r, err
	}
	if err := migrateChunks(ctx, src, mysql, r); err != nil {
		return r, err
	}
	if err := copyUploads(ctx, src, mysql, uploadsDir, targetUploadsDir, r); err != nil {
		return r, err
	}
	return r, nil
}

func migrateClasses(ctx context.Context, src, dst *sql.DB, r *Report) error {
	rows, err := src.QueryContext(ctx, "SELECT id, slug, name FROM classes")
	if err != nil {
		return fmt.Errorf("read classes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.OldSlug, &c.Name); err != nil {
			return err
		}
		c.Slug = slugMap[c.OldSlug]
		if c.Slug == "" {
			c.Slug = c.OldSlug
		}
		_, err := dst.ExecContext(ctx,
			"INSERT INTO classes(id,slug,name) VALUES(?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name)",
			c.ID, c.Slug, c.Name)
		if err != nil {
			return fmt.Errorf("insert class %d: %w", c.ID, err)
		}
		r.Classes++
	}
	return rows.Err()
}

func migrateUsers(ctx context.Context, src, dst *sql.DB, r *Report) error {
	rows, err := src.QueryContext(ctx, "SELECT id, username, password_hash, role, class_id FROM users")
	if err != nil {
		return fmt.Errorf("read users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.ClassID); err != nil {
			return err
		}
		_, err := dst.ExecContext(ctx,
			"INSERT INTO users(id,username,password_hash,role,class_id) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE password_hash=VALUES(password_hash), role=VALUES(role), class_id=VALUES(class_id)",
			u.ID, u.Username, u.PasswordHash, u.Role, u.ClassID)
		if err != nil {
			return fmt.Errorf("insert user %d: %w", u.ID, err)
		}
		r.Users++
	}
	return rows.Err()
}

func migrateMaterials(ctx context.Context, src, dst *sql.DB, r *Report) error {
	rows, err := src.QueryContext(ctx, "SELECT id, class_id, uploader_id, original_filename, storage_key, status, created_at FROM materials")
	if err != nil {
		return fmt.Errorf("read materials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m Material
		var createdAtStr string
		if err := rows.Scan(&m.ID, &m.ClassID, &m.UploaderID, &m.Filename, &m.StorageKey, &m.Status, &createdAtStr); err != nil {
			return err
		}
		if m.Status != "ready" {
			r.Anomalies = append(r.Anomalies, fmt.Sprintf("material %s skipped: status=%s", m.ID, m.Status))
			continue
		}
		m.Title = m.Filename
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		if m.CreatedAt.IsZero() {
			m.CreatedAt = time.Now().UTC()
		}
		_, err := dst.ExecContext(ctx,
			"INSERT IGNORE INTO materials(id,class_id,uploader_id,title,original_filename,storage_key,status,created_at) VALUES(?,?,?,?,?,?,?,?)",
			m.ID, m.ClassID, m.UploaderID, m.Title, m.Filename, m.StorageKey, "ready", m.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert material %s: %w", m.ID, err)
		}
		r.Materials++
	}
	return rows.Err()
}

func migrateChunks(ctx context.Context, src, dst *sql.DB, r *Report) error {
	rows, err := src.QueryContext(ctx, "SELECT material_id, class_id, chunk_index, content FROM knowledge_chunks ORDER BY material_id, chunk_index")
	if err != nil {
		return fmt.Errorf("read chunks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.MaterialID, &c.ClassID, &c.ChunkIndex, &c.Content); err != nil {
			return err
		}
		_, err := dst.ExecContext(ctx,
			"INSERT IGNORE INTO knowledge_entries(material_id,class_id,chunk_index,content) VALUES(?,?,?,?)",
			c.MaterialID, c.ClassID, c.ChunkIndex, c.Content)
		if err != nil {
			return fmt.Errorf("insert chunk for %s: %w", c.MaterialID, err)
		}
		r.Chunks++
	}
	return rows.Err()
}

func copyUploads(ctx context.Context, src, dst *sql.DB, srcDir, dstDir string, r *Report) error {
	rows, err := src.QueryContext(ctx, "SELECT storage_key FROM materials WHERE status='ready'")
	if err != nil {
		return fmt.Errorf("read storage keys: %w", err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create uploads dir: %w", err)
	}
	for _, key := range keys {
		srcPath := filepath.Join(srcDir, key)
		dstPath := filepath.Join(dstDir, key)
		if _, err := os.Stat(dstPath); err == nil {
			r.Files++
			continue
		}
		if _, err := os.Stat(srcPath); err != nil {
			r.Anomalies = append(r.Anomalies, fmt.Sprintf("file missing for storage_key=%s", key))
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy file %s: %w", key, err)
		}
		r.Files++
	}
	return nil
}
