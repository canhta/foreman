package db

const schema = `
CREATE TABLE IF NOT EXISTS tickets (
    id TEXT PRIMARY KEY,
    external_id TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT,
    labels TEXT,
    priority TEXT,
    status TEXT NOT NULL DEFAULT 'queued',
    external_status TEXT,
    repo_url TEXT,
    branch_name TEXT,
    pr_url TEXT,
    pr_number INTEGER,
    pr_head_sha TEXT NOT NULL DEFAULT '',
    parent_ticket_id TEXT DEFAULT '',
    decompose_depth INTEGER DEFAULT 0,
    is_partial BOOLEAN DEFAULT FALSE,
    cost_usd REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,
    clarification_requested_at TIMESTAMP,
    channel_sender_id TEXT DEFAULT '',
    error_message TEXT,
    last_completed_task_seq INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    sequence INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    files_to_read TEXT,
    files_to_modify TEXT,
    test_assertions TEXT,
    estimated_complexity TEXT,
    depends_on TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    implementation_attempts INTEGER DEFAULT 0,
    spec_review_attempts INTEGER DEFAULT 0,
    quality_review_attempts INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,
    commit_sha TEXT,
    last_error_type TEXT NOT NULL DEFAULT '',
    cost_usd REAL DEFAULT 0.0,
    context_budget      INTEGER DEFAULT 0,
    context_used        INTEGER DEFAULT 0,
    files_selected      INTEGER DEFAULT 0,
    files_touched       INTEGER DEFAULT 0,
    context_cache_hits  INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS llm_calls (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),
    role TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    tokens_input INTEGER NOT NULL,
    tokens_output INTEGER NOT NULL,
    cost_usd REAL NOT NULL,
    duration_ms INTEGER NOT NULL,
    prompt_hash TEXT,
    response_summary TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS llm_call_details (
    llm_call_id TEXT PRIMARY KEY,
    full_prompt TEXT NOT NULL DEFAULT '',
    full_response TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS handoffs (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    from_role TEXT NOT NULL,
    to_role TEXT,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS progress_patterns (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    pattern_key TEXT NOT NULL,
    pattern_value TEXT NOT NULL,
    directories TEXT,
    discovered_by_task TEXT REFERENCES tasks(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_reservations (
    file_path TEXT NOT NULL,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    reserved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP,
    PRIMARY KEY (file_path, ticket_id)
);

CREATE TABLE IF NOT EXISTS cost_daily (
    date TEXT PRIMARY KEY,
    total_usd REAL DEFAULT 0.0,
    total_input_tokens INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    ticket_count INTEGER DEFAULT 0,
    task_count INTEGER DEFAULT 0,
    llm_call_count INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    ticket_id TEXT REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    details TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    token_hash TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked BOOLEAN DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS pending_pairings (
    code        TEXT PRIMARY KEY,
    sender_id   TEXT NOT NULL,
    channel     TEXT NOT NULL DEFAULT 'whatsapp',
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS distributed_locks (
    lock_name   TEXT PRIMARY KEY,
    acquired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TIMESTAMP NOT NULL,
    holder_id   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS embeddings (
    repo_path   TEXT NOT NULL,
    head_sha    TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    start_line  INTEGER NOT NULL,
    end_line    INTEGER NOT NULL,
    chunk_text  TEXT NOT NULL,
    vector      BLOB NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (repo_path, head_sha, file_path, start_line)
);

CREATE TABLE IF NOT EXISTS context_feedback (
    id          TEXT PRIMARY KEY,
    ticket_id   TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    files_selected TEXT NOT NULL DEFAULT '',
    files_touched  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_context_feedback_created ON context_feedback(created_at);

CREATE INDEX IF NOT EXISTS idx_embeddings_repo_sha
    ON embeddings(repo_path, head_sha);

CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_external_id ON tickets(external_id);
CREATE INDEX IF NOT EXISTS idx_tasks_ticket_id ON tasks(ticket_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_llm_calls_ticket_id ON llm_calls(ticket_id);
CREATE INDEX IF NOT EXISTS idx_events_ticket_id ON events(ticket_id);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
CREATE INDEX IF NOT EXISTS idx_file_reservations_ticket ON file_reservations(ticket_id);
CREATE INDEX IF NOT EXISTS idx_tickets_parent ON tickets(parent_ticket_id);
`
