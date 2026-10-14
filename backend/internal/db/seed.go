package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"campusclaw/internal/auth"
)

type seedClass struct {
	slug, name string
}

type seedUser struct {
	username, role, classSlug string
}

type seedMaterial struct {
	id, classSlug, uploader, filename, title, content string
}

var seedClasses = []seedClass{
	{"a", "A班"},
	{"b", "B班"},
}

var seedUsers = []seedUser{
	{"a-teacher", "teacher", "a"},
	{"a-student", "student", "a"},
	{"b-teacher", "teacher", "b"},
	{"b-student", "student", "b"},
}

var seedMaterials = []seedMaterial{
	{
		id: "seed-a-intro", classSlug: "a", uploader: "a-teacher",
		filename: "A班课程导览.md", title: "A班课程导览",
		content: "# A班课程导览\n\n欢迎来到A班。这是A班的示例材料，仅供A班同学使用。\n\n## 课程安排\n\n- 第一讲：入门\n- 第二讲：进阶\n",
	},
	{
		id: "seed-b-intro", classSlug: "b", uploader: "b-teacher",
		filename: "B班课程导览.md", title: "B班课程导览",
		content: "# B班课程导览\n\n欢迎来到B班。这是B班的示例材料，仅供B班同学使用。\n\n## 课程安排\n\n- 第一讲：基础\n- 第二讲：实践\n",
	},
}

// Seed idempotently initializes A/B classes, the four existing account names,
// and one distinguishable seed material per class. It never overwrites user
// passwords or user-uploaded materials on repeated invocation.
func Seed(ctx context.Context, conn *sql.DB, teacherPassword, studentPassword, uploadDir string) error {
	for _, class := range seedClasses {
		if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES(?,?) ON DUPLICATE KEY UPDATE name=VALUES(name)", class.slug, class.name); err != nil {
			return err
		}
	}
	classIDs := map[string]int64{}
	for _, class := range seedClasses {
		var id int64
		if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug=?", class.slug).Scan(&id); err != nil {
			return err
		}
		classIDs[class.slug] = id
	}
	for _, u := range seedUsers {
		password := studentPassword
		if u.role == "teacher" {
			password = teacherPassword
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE role=VALUES(role), class_id=VALUES(class_id)", u.username, hash, u.role, classIDs[u.classSlug]); err != nil {
			return err
		}
	}
	userIDs := map[string]int64{}
	for _, u := range seedUsers {
		var id int64
		if err := conn.QueryRowContext(ctx, "SELECT id FROM users WHERE username=?", u.username).Scan(&id); err != nil {
			return err
		}
		userIDs[u.username] = id
	}
	for _, m := range seedMaterials {
		classID := classIDs[m.classSlug]
		uploaderID := userIDs[m.uploader]
		_, err := conn.ExecContext(ctx, "INSERT IGNORE INTO materials(id,class_id,uploader_id,title,original_filename,storage_key,status) VALUES(?,?,?,?,?,?,'ready')", m.id, classID, uploaderID, m.title, m.filename, m.id)
		if err != nil {
			return err
		}
		if err = ensureSeedBody(ctx, conn, m, classID); err != nil {
			return err
		}
		path := filepath.Join(uploadDir, m.id)
		if _, err = os.Stat(path); os.IsNotExist(err) {
			if err = os.MkdirAll(uploadDir, 0o755); err != nil {
				return err
			}
			if err = os.WriteFile(path, []byte(m.content), 0o644); err != nil {
				return err
			}
		}
	}
	if err := seedSkillRows(ctx, conn); err != nil {
		return err
	}
	return seedAgents(ctx, conn)
}

func ensureSeedBody(ctx context.Context, conn *sql.DB, m seedMaterial, classID int64) error {
	_, err := conn.ExecContext(ctx, "INSERT IGNORE INTO knowledge_entries(material_id,class_id,body_text) VALUES(?,?,?)", m.id, classID, m.content)
	return err
}
