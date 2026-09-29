CREATE TABLE IF NOT EXISTS classes (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 slug VARCHAR(64) NOT NULL UNIQUE,
 name VARCHAR(255) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS users (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 username VARCHAR(255) COLLATE utf8mb4_bin NOT NULL UNIQUE,
 password_hash VARCHAR(512) NOT NULL,
 role VARCHAR(16) NOT NULL,
 class_id BIGINT NULL,
 CONSTRAINT valid_role CHECK (role IN ('teacher','student')),
 FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS sessions (
 id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 user_id BIGINT NOT NULL,
 created_at BIGINT NOT NULL,
 expires_at BIGINT NOT NULL,
 CONSTRAINT valid_expiry CHECK (expires_at > created_at),
 INDEX sessions_expiry (expires_at),
 FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS materials (
 id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 class_id BIGINT NOT NULL,
 uploader_id BIGINT NOT NULL,
 original_filename VARCHAR(255) NOT NULL,
 title VARCHAR(255) NOT NULL,
 storage_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
 status VARCHAR(16) NOT NULL DEFAULT 'ready',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 CONSTRAINT valid_status CHECK (status IN ('ready','processing','failed')),
 UNIQUE KEY material_class (id,class_id),
 INDEX materials_class_status (class_id,status,created_at),
 FOREIGN KEY (class_id) REFERENCES classes(id),
 FOREIGN KEY (uploader_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS knowledge_entries (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 class_id BIGINT NOT NULL,
 chunk_index INT NOT NULL,
 content MEDIUMTEXT NOT NULL,
 CONSTRAINT nonnegative_chunk CHECK (chunk_index >= 0),
 UNIQUE KEY material_chunk (material_id,chunk_index),
 INDEX knowledge_class_material (class_id,material_id),
 FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE,
 FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
