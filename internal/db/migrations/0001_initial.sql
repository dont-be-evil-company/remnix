-- 0001_initial.sql
-- versioned local schema for syncsh v1
-- schema_migrations is owned by the migration runner

CREATE TABLE devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hostname TEXT,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    retired_at INTEGER
);

CREATE TABLE history (
    id TEXT PRIMARY KEY,
    command TEXT NOT NULL,
    start_ts INTEGER NOT NULL,
    end_ts INTEGER,
    duration_ms INTEGER,
    exit_status INTEGER,
    cwd TEXT,
    session_id TEXT,
    hostname TEXT,
    device_id TEXT NOT NULL,
    shell TEXT,
    deleted INTEGER NOT NULL DEFAULT 0,
    origin_device_id TEXT,
    origin_seq INTEGER,
    created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX history_origin
    ON history(origin_device_id, origin_seq)
    WHERE origin_device_id IS NOT NULL AND origin_seq IS NOT NULL;

CREATE UNIQUE INDEX history_dedup
    ON history(command, start_ts, cwd, device_id)
    WHERE deleted = 0;

CREATE INDEX history_start_ts ON history(start_ts DESC);
CREATE INDEX history_cwd ON history(cwd);
CREATE INDEX history_device ON history(device_id);
CREATE INDEX history_session ON history(session_id);

CREATE TABLE sync_events (
    device_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload BLOB NOT NULL,
    applied INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, seq)
);

CREATE TABLE sync_heads (
    device_id TEXT PRIMARY KEY,
    seq INTEGER NOT NULL
);

CREATE TABLE acks (
    device_id TEXT PRIMARY KEY,
    frontier_json TEXT NOT NULL,
    checkpoint_id TEXT,
    updated_at INTEGER NOT NULL
);

CREATE TABLE checkpoints (
    id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    frontier_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE key_generations (
    id TEXT PRIMARY KEY,
    seq INTEGER NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE key_slots (
    id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    slot_type TEXT NOT NULL,
    wrap_params BLOB,
    wrapped_smk BLOB NOT NULL,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (generation_id) REFERENCES key_generations(id)
);

CREATE TABLE trusted_manifests (
    kind TEXT PRIMARY KEY,
    generation_id TEXT,
    counter INTEGER NOT NULL,
    checkpoint_id TEXT,
    payload BLOB,
    updated_at INTEGER NOT NULL
);

CREATE TABLE transport_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
