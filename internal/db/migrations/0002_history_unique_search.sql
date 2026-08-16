-- 0002_history_unique_search.sql
-- Supports Atuin-style unique Ctrl+R: scan by recency and probe
-- (command, start_ts, id) until LIMIT distinct commands, without GROUP BY.

CREATE INDEX history_command_start
    ON history(command, start_ts DESC, id)
    WHERE deleted = 0;
