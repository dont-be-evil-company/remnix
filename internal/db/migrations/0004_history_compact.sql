-- 0004_history_compact.sql
-- Intern repeated strings and hash indexes. History rewrite is in the Go post-hook.

CREATE TABLE intern_cwd (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL UNIQUE
);

CREATE TABLE intern_session (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL UNIQUE
);

CREATE TABLE intern_hostname (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL UNIQUE
);

CREATE TABLE intern_shell (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL UNIQUE
);
