-- 0001_initial.sql
-- persistent CLI help cache for overlay suggestions

CREATE TABLE tools (
    name TEXT PRIMARY KEY,
    bin_path TEXT NOT NULL DEFAULT '',
    bin_mtime INTEGER NOT NULL DEFAULT 0,
    bin_size INTEGER NOT NULL DEFAULT 0,
    version TEXT NOT NULL DEFAULT '',
    warmed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE pages (
    argv TEXT PRIMARY KEY,
    tool TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    probed_at INTEGER NOT NULL
);

CREATE INDEX pages_tool ON pages(tool);

CREATE TABLE entities (
    parent_argv TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    descr TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (parent_argv, name, kind),
    FOREIGN KEY (parent_argv) REFERENCES pages(argv) ON DELETE CASCADE
);

CREATE INDEX entities_parent ON entities(parent_argv);
