-- 0005_key_rotation.sql
-- Durable in-progress key rotation identity. No SMK material is stored here.

CREATE TABLE key_rotation (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    phase TEXT NOT NULL,
    old_generation_id TEXT NOT NULL,
    new_generation_id TEXT NOT NULL,
    old_seq INTEGER NOT NULL,
    new_seq INTEGER NOT NULL,
    recovery_bech32 TEXT,
    updated_at INTEGER NOT NULL
);
