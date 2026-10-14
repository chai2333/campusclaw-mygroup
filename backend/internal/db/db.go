package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"campusclaw/internal/config"
	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schema string

func Open(c config.Config) (*sql.DB, error) {
	opts := mysql.NewConfig()
	opts.User = c.DBUser
	opts.Passwd = c.DBPassword
	opts.Net = "tcp"
	opts.Addr = net.JoinHostPort(c.DBHost, strconv.Itoa(c.DBPort))
	opts.DBName = c.DBName
	opts.ParseTime = true
	opts.Loc = time.UTC
	opts.Timeout = 3 * time.Second
	opts.ReadTimeout = 10 * time.Second
	opts.WriteTimeout = 10 * time.Second
	opts.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	conn, err := sql.Open("mysql", opts.FormatDSN())
	if err != nil {
		return nil, errors.New("database configuration invalid")
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)
	return conn, nil
}
func Wait(ctx context.Context, conn *sql.DB) error {
	delay := 100 * time.Millisecond
	for {
		if err := conn.PingContext(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("database startup deadline exceeded")
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}
func Migrate(ctx context.Context, conn *sql.DB) error {
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return errors.New("database migration failed")
		}
	}
	if err := convertKnowledgeLegacy(ctx, conn); err != nil {
		return errors.New("database migration failed")
	}
	if err := migrateAgents(ctx, conn); err != nil {
		return errors.New("database migration failed")
	}
	if err := seedSkillRows(ctx, conn); err != nil {
		return errors.New("database migration failed")
	}
	return nil
}

// seedSkillRows gives every existing class an enabled guided_problem_solving
// skill row. INSERT IGNORE keeps the migration idempotent.
func seedSkillRows(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, "INSERT IGNORE INTO class_skills (class_id,skill_key,enabled) SELECT id,'guided_problem_solving',1 FROM classes")
	return err
}

// Repair performs the startup data fix-up required before accepting chat
// traffic: stale streaming turns from a previous process become failed with
// a server_interrupted code, active locks that no longer guard a streaming
// message are cleared, and missing skill rows are restored. It is idempotent
// and leaves completed history untouched.
func Repair(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, "UPDATE conversation_messages SET status='failed',error_code='server_interrupted',completed_at=NOW(6) WHERE status='streaming'"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "UPDATE conversations c LEFT JOIN conversation_messages m ON m.id=c.active_message_id SET c.active_message_id=NULL WHERE c.active_message_id IS NOT NULL AND (m.id IS NULL OR m.status<>'streaming')"); err != nil {
		return err
	}
	return seedSkillRows(ctx, conn)
}

// convertKnowledgeLegacy transforms a pre-existing chunked knowledge_entries
// table (id, material_id, class_id, chunk_index, content) into the current
// form where one body_text row per material is authoritative. It is idempotent:
// when the legacy content column is absent the table is already converted.
func convertKnowledgeLegacy(ctx context.Context, conn *sql.DB) error {
	hasContent, err := columnExists(ctx, conn, "knowledge_entries", "content")
	if err != nil || !hasContent {
		return err
	}
	if ok, err := columnExists(ctx, conn, "knowledge_entries", "body_text"); err != nil {
		return err
	} else if !ok {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries ADD COLUMN body_text MEDIUMTEXT NULL"); err != nil {
			return err
		}
	}
	// Merge ordered legacy chunks into one body_text per material, keeping the
	// lowest id row. Merging happens in Go because GROUP_CONCAT truncates.
	rows, err := conn.QueryContext(ctx, "SELECT id, material_id, chunk_index, content FROM knowledge_entries ORDER BY material_id, chunk_index")
	if err != nil {
		return err
	}
	type entry struct {
		id   int64
		body strings.Builder
	}
	merged := map[string]*entry{}
	for rows.Next() {
		var id int64
		var material string
		var chunkIndex int
		var content string
		if err := rows.Scan(&id, &material, &chunkIndex, &content); err != nil {
			rows.Close()
			return err
		}
		if e, ok := merged[material]; ok {
			e.body.WriteString(content)
			continue
		}
		e := &entry{id: id}
		e.body.WriteString(content)
		merged[material] = e
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for material, e := range merged {
		if _, err := conn.ExecContext(ctx, "UPDATE knowledge_entries SET body_text=? WHERE id=?", e.body.String(), e.id); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "DELETE FROM knowledge_entries WHERE material_id=? AND id<>?", material, e.id); err != nil {
			return err
		}
	}
	if ok, err := indexExists(ctx, conn, "knowledge_entries", "material_chunk"); err != nil {
		return err
	} else if ok {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP INDEX material_chunk"); err != nil {
			return err
		}
	}
	if ok, err := checkExists(ctx, conn, "knowledge_entries", "nonnegative_chunk"); err != nil {
		return err
	} else if ok {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP CHECK nonnegative_chunk"); err != nil {
			return err
		}
	}
	hasID, err := columnExists(ctx, conn, "knowledge_entries", "id")
	if err != nil {
		return err
	}
	if hasID {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries MODIFY id BIGINT NOT NULL"); err != nil {
			return err
		}
		pk, err := primaryKeyColumn(ctx, conn, "knowledge_entries")
		if err != nil {
			return err
		}
		if pk == "id" {
			if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP PRIMARY KEY"); err != nil {
				return err
			}
		}
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP COLUMN id"); err != nil {
			return err
		}
	}
	if pk, err := primaryKeyColumn(ctx, conn, "knowledge_entries"); err != nil {
		return err
	} else if pk == "" {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries ADD PRIMARY KEY (material_id)"); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries MODIFY body_text MEDIUMTEXT NOT NULL"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP COLUMN content"); err != nil {
		return err
	}
	if ok, err := columnExists(ctx, conn, "knowledge_entries", "chunk_index"); err != nil {
		return err
	} else if ok {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries DROP COLUMN chunk_index"); err != nil {
			return err
		}
	}
	if ok, err := indexExists(ctx, conn, "knowledge_entries", "knowledge_class"); err != nil {
		return err
	} else if !ok {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE knowledge_entries ADD INDEX knowledge_class (class_id)"); err != nil {
			return err
		}
	}
	return nil
}

func columnExists(ctx context.Context, conn *sql.DB, table, column string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?", table, column).Scan(&count)
	return count > 0, err
}

func indexExists(ctx context.Context, conn *sql.DB, table, index string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(DISTINCT INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?", table, index).Scan(&count)
	return count > 0, err
}

func checkExists(ctx context.Context, conn *sql.DB, table, constraint string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME=? AND CONSTRAINT_TYPE='CHECK' AND CONSTRAINT_NAME=?", table, constraint).Scan(&count)
	return count > 0, err
}

func primaryKeyColumn(ctx context.Context, conn *sql.DB, table string) (string, error) {
	var column sql.NullString
	err := conn.QueryRowContext(ctx, "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND CONSTRAINT_NAME='PRIMARY' LIMIT 1", table).Scan(&column)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return column.String, nil
}
