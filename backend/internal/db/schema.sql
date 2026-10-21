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
 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 class_id BIGINT NOT NULL,
 body_text MEDIUMTEXT NOT NULL,
 INDEX knowledge_class (class_id),
 FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE,
 FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS knowledge_chunks (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 class_id BIGINT NOT NULL,
 chunk_index INT NOT NULL,
 chunk_text MEDIUMTEXT NOT NULL,
 char_start INT NOT NULL,
 char_end INT NOT NULL,
 strategy VARCHAR(16) NOT NULL,
 index_status VARCHAR(16) NOT NULL DEFAULT 'pending',
 CONSTRAINT valid_chunk_strategy CHECK (strategy IN ('auto','custom','hierarchy')),
 CONSTRAINT valid_chunk_status CHECK (index_status IN ('pending','ready','failed')),
 CONSTRAINT chunks_nonnegative_chunk CHECK (chunk_index >= 0),
 CONSTRAINT valid_char_range CHECK (char_start >= 0 AND char_end >= char_start),
 UNIQUE KEY material_chunk (material_id,chunk_index),
 INDEX chunks_class_status (class_id,index_status),
 FULLTEXT KEY chunks_text (chunk_text) WITH PARSER ngram,
 FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE,
 FOREIGN KEY (class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS assistant_prompts (
 class_id BIGINT PRIMARY KEY,
 body MEDIUMTEXT NOT NULL,
 version INT NOT NULL,
 updated_by BIGINT NULL,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 CONSTRAINT prompt_version_positive CHECK (version >= 1),
 FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE CASCADE,
 FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS class_skills (
 class_id BIGINT NOT NULL,
 skill_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 enabled TINYINT(1) NOT NULL DEFAULT 1,
 updated_by BIGINT NULL,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (class_id,skill_key),
 CONSTRAINT skill_key_supported CHECK (skill_key = 'guided_problem_solving'),
 FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE CASCADE,
 FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS tutor_agents (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 class_id BIGINT NOT NULL,
 name VARCHAR(64) NOT NULL,
 prompt TEXT NOT NULL,
 enabled TINYINT(1) NOT NULL DEFAULT 1,
 version INT NOT NULL DEFAULT 1,
 seed_key VARCHAR(64) NULL,
 UNIQUE KEY agent_name (class_id,name),
 UNIQUE KEY agent_seed (class_id,seed_key),
 UNIQUE KEY agent_class (id,class_id),
 FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS skills (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 class_id BIGINT NOT NULL,
 name VARCHAR(32) NOT NULL,
 trigger_text VARCHAR(120) NOT NULL,
 subject_text VARCHAR(32) NOT NULL DEFAULT '',
 description TEXT NOT NULL,
 tools_json JSON NOT NULL,
 source_text TEXT NOT NULL,
 enabled TINYINT(1) NOT NULL DEFAULT 1,
 seed_key VARCHAR(64) NULL,
 UNIQUE KEY skill_name (class_id,name),
 UNIQUE KEY skill_seed (class_id,seed_key),
 UNIQUE KEY skill_class (id,class_id),
 FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS tutor_agent_skills (
 agent_id BIGINT NOT NULL,
 skill_id BIGINT NOT NULL,
 class_id BIGINT NOT NULL,
 PRIMARY KEY (agent_id,skill_id),
 FOREIGN KEY (agent_id,class_id) REFERENCES tutor_agents(id,class_id) ON DELETE CASCADE,
 FOREIGN KEY (skill_id,class_id) REFERENCES skills(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS tutor_agent_materials (
 agent_id BIGINT NOT NULL,
 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 class_id BIGINT NOT NULL,
 PRIMARY KEY (agent_id,material_id),
 FOREIGN KEY (agent_id,class_id) REFERENCES tutor_agents(id,class_id) ON DELETE CASCADE,
 FOREIGN KEY (material_id,class_id) REFERENCES materials(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS conversations (
 id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 owner_user_id BIGINT NOT NULL,
 agent_id BIGINT NULL,
 class_id BIGINT NOT NULL,
 title VARCHAR(255) NOT NULL,
 active_message_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 INDEX conversations_owner (owner_user_id,updated_at),
 INDEX conversations_class (class_id),
 INDEX conversations_active (active_message_id),
 FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
 FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS conversation_messages (
 id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 conversation_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 sequence_no INT NOT NULL,
 role VARCHAR(16) NOT NULL,
 content MEDIUMTEXT NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'streaming',
 skill_id BIGINT NULL,
 skill_name VARCHAR(32) NULL,
 tool_calls_json JSON NULL,
 prompt_version INT NULL,
 skill_enabled TINYINT(1) NULL,
 model VARCHAR(128) NULL,
 error_code VARCHAR(64) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 completed_at DATETIME(6) NULL,
 CONSTRAINT message_role_valid CHECK (role IN ('user','assistant')),
 CONSTRAINT message_status_valid CHECK (status IN ('streaming','completed','failed','cancelled')),
 CONSTRAINT message_sequence_positive CHECK (sequence_no >= 1),
 UNIQUE KEY conversation_sequence (conversation_id,sequence_no),
 INDEX messages_conversation_status (conversation_id,status),
 FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS message_citations (
 assistant_message_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 ordinal INT NOT NULL,
 material_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 chunk_id BIGINT NOT NULL,
 title_snapshot VARCHAR(255) NOT NULL,
 chunk_index INT NOT NULL,
 char_start INT NOT NULL,
 char_end INT NOT NULL,
 excerpt_snapshot TEXT NOT NULL,
 PRIMARY KEY (assistant_message_id,ordinal),
 CONSTRAINT citation_ordinal_valid CHECK (ordinal >= 1 AND ordinal <= 4),
 CONSTRAINT citation_char_range CHECK (char_start >= 0 AND char_end >= char_start AND chunk_index >= 0),
 FOREIGN KEY (assistant_message_id) REFERENCES conversation_messages(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS mcp_servers (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 class_id BIGINT NOT NULL,
 name VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 base_url VARCHAR(512) NOT NULL,
 token_cipher TEXT NOT NULL,
 connected TINYINT(1) NOT NULL DEFAULT 0,
 version BIGINT NOT NULL DEFAULT 1,
 last_error VARCHAR(200) NOT NULL DEFAULT '',
 last_tools_json JSON NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY mcp_class_name(class_id,name),
 UNIQUE KEY mcp_id_class(id,class_id),
 FOREIGN KEY(class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS mcp_tools (
 server_id BIGINT NOT NULL,
 class_id BIGINT NOT NULL,
 name VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 definition_json JSON NOT NULL,
 approved TINYINT(1) NOT NULL DEFAULT 0,
 PRIMARY KEY(server_id,name),
 FOREIGN KEY(server_id,class_id) REFERENCES mcp_servers(id,class_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS mcp_tool_changes (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 server_id BIGINT NULL,
 class_id BIGINT NOT NULL,
 server_name VARCHAR(48) NOT NULL,
 tool_name VARCHAR(48) NOT NULL,
 previous_json JSON NULL,
 current_json JSON NOT NULL,
 approved_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(server_id) REFERENCES mcp_servers(id) ON DELETE SET NULL,
 FOREIGN KEY(class_id) REFERENCES classes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS mcp_tool_calls (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 server_id BIGINT NULL,
 class_id BIGINT NOT NULL,
 server_name VARCHAR(48) NOT NULL,
 tool_name VARCHAR(48) NOT NULL,
 tool_ref VARCHAR(100) NOT NULL,
 status VARCHAR(16) NOT NULL,
 user_id BIGINT NOT NULL,
 conversation_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 assistant_message_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 round_no INT NOT NULL,
 text VARCHAR(800) NOT NULL,
 latency_ms BIGINT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX mcp_calls_class(class_id,id),
 INDEX mcp_calls_conversation(conversation_id),
 FOREIGN KEY(server_id) REFERENCES mcp_servers(id) ON DELETE SET NULL,
 FOREIGN KEY(class_id) REFERENCES classes(id),
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
 FOREIGN KEY(assistant_message_id) REFERENCES conversation_messages(id) ON DELETE CASCADE,
 CONSTRAINT mcp_status_valid CHECK(status IN ('ok','denied','error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
