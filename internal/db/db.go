package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("K12EDU_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, initialSchema)
	return err
}

const initialSchema = `
CREATE TABLE IF NOT EXISTS users (
 id UUID PRIMARY KEY, email TEXT UNIQUE, display_name TEXT NOT NULL DEFAULT '',
 role TEXT NOT NULL DEFAULT 'student' CHECK (role IN ('student','parent','admin')),
 apple_subject TEXT UNIQUE, password_hash TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS devices (
 id UUID PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 platform TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS learning_progress (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 payload JSONB NOT NULL DEFAULT '{}'::jsonb, version BIGINT NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS sync_events (
 id UUID PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id UUID REFERENCES devices(id) ON DELETE SET NULL, event_type TEXT NOT NULL,
 payload JSONB NOT NULL, client_created_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sync_events_user_created ON sync_events(user_id, created_at);
CREATE TABLE IF NOT EXISTS ai_usage (
 id UUID PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 model TEXT NOT NULL DEFAULT '', provider_status INT NOT NULL,
 input_bytes INT NOT NULL DEFAULT 0, output_bytes INT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_ai_usage_user_created ON ai_usage(user_id, created_at);`
