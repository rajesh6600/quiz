-- +goose Up
-- Base schema for core gameplay objects.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "citext";

CREATE TABLE users (
    user_id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email             CITEXT UNIQUE,
    password_hash     TEXT,
    display_name      TEXT NOT NULL,
    user_type         TEXT NOT NULL CHECK (user_type IN ('registered', 'guest')),
    status            TEXT NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at     TIMESTAMPTZ,
    metadata          JSONB NOT NULL DEFAULT '{}'::JSONB
);
CREATE INDEX idx_users_type ON users(user_type);

CREATE TABLE questions (
    question_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source            TEXT NOT NULL CHECK (source IN ('curated', 'opentdb', 'triviaapi', 'quizapi', 'kaggle', 'ai')),
    category          TEXT NOT NULL,
    difficulty        TEXT NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    type              TEXT NOT NULL CHECK (type IN ('mcq', 'true_false')),
    prompt            TEXT NOT NULL,
    options           TEXT[] NOT NULL,
    correct_answer    TEXT NOT NULL,
    metadata          JSONB NOT NULL DEFAULT '{}'::JSONB,
    verified          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_questions_category ON questions(category);
CREATE INDEX idx_questions_difficulty ON questions(difficulty);
CREATE INDEX idx_questions_verified ON questions(verified);

CREATE TABLE matches (
    match_id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode                    TEXT NOT NULL CHECK (mode IN ('random_1v1', 'private_room', 'bot_fill')),
    question_count          SMALLINT NOT NULL,
    per_question_seconds    SMALLINT NOT NULL,
    global_timeout_seconds  SMALLINT NOT NULL,
    seed_hash               TEXT NOT NULL,
    leaderboard_eligible    BOOLEAN NOT NULL DEFAULT TRUE,
    started_at              TIMESTAMPTZ,
    completed_at            TIMESTAMPTZ,
    status                  TEXT NOT NULL CHECK (status IN ('pending', 'active', 'completed', 'timeout', 'cancelled')),
    created_by              UUID REFERENCES users(user_id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata                JSONB NOT NULL DEFAULT '{}'::JSONB
);
CREATE INDEX idx_matches_mode ON matches(mode);
CREATE INDEX idx_matches_created_by ON matches(created_by);

CREATE TABLE player_match_state (
    match_id          UUID REFERENCES matches(match_id) ON DELETE CASCADE,
    user_id           UUID REFERENCES users(user_id),
    is_guest          BOOLEAN NOT NULL DEFAULT FALSE,
    joined_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at           TIMESTAMPTZ,
    final_score       INTEGER,
    status            TEXT NOT NULL CHECK (status IN ('queued', 'active', 'completed', 'left_early', 'timeout')),
    accuracy          NUMERIC(5,2),
    streak_bonus_pct  NUMERIC(5,2),
    answers           JSONB NOT NULL DEFAULT '[]'::JSONB,
    PRIMARY KEY(match_id, user_id)
);
CREATE INDEX idx_player_state_user ON player_match_state(user_id);
CREATE INDEX idx_player_state_status ON player_match_state(status);

CREATE TABLE leaderboard_snapshots (
    snapshot_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    time_window    TEXT NOT NULL CHECK (time_window IN ('daily', 'weekly', 'monthly', 'all_time')),
    generated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    entries        JSONB NOT NULL,
    source_hash    TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_leaderboard_snapshots_window ON leaderboard_snapshots(time_window);

CREATE TABLE audit_logs (
    audit_id       BIGSERIAL PRIMARY KEY,
    actor_id       UUID,
    entity_type    TEXT NOT NULL,
    entity_id      UUID,
    action         TEXT NOT NULL,
    payload        JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    trace_id       UUID NOT NULL DEFAULT gen_random_uuid()
);
CREATE INDEX idx_audit_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_actor ON audit_logs(actor_id);

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS leaderboard_snapshots;
DROP TABLE IF EXISTS player_match_state;
DROP TABLE IF EXISTS matches;
DROP TABLE IF EXISTS questions;
DROP TABLE IF EXISTS users;
DROP EXTENSION IF EXISTS "citext";
DROP EXTENSION IF EXISTS "uuid-ossp";

