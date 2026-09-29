PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS classes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('teacher', 'student')),
    class_id INTEGER REFERENCES classes(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS materials (
    id TEXT PRIMARY KEY,
    class_id INTEGER NOT NULL REFERENCES classes(id) ON DELETE RESTRICT,
    uploader_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    original_filename TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('processing', 'ready', 'failed')),
    error TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    material_id TEXT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    class_id INTEGER NOT NULL REFERENCES classes(id) ON DELETE RESTRICT,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    UNIQUE(material_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_materials_class_status
ON materials(class_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_class_material
ON knowledge_chunks(class_id, material_id);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
