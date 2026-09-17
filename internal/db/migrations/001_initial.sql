CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'student' CHECK (role IN ('student', 'parent', 'admin')),
    apple_subject TEXT UNIQUE,
    password_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS devices (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS learning_progress (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sync_events (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    client_created_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);
ALTER TABLE sync_events ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE sync_events ADD COLUMN IF NOT EXISTS retry_count INT NOT NULL DEFAULT 0;
ALTER TABLE sync_events ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_events ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sync_events_user_created ON sync_events(user_id, created_at);
CREATE TABLE IF NOT EXISTS ai_usage (
 id UUID PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 model TEXT NOT NULL DEFAULT '', provider_status INT NOT NULL,
 input_bytes INT NOT NULL DEFAULT 0, output_bytes INT NOT NULL DEFAULT 0,
 input_tokens INT NOT NULL DEFAULT 0, output_tokens INT NOT NULL DEFAULT 0, cost_micros BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE ai_usage ADD COLUMN IF NOT EXISTS input_tokens INT NOT NULL DEFAULT 0;
ALTER TABLE ai_usage ADD COLUMN IF NOT EXISTS output_tokens INT NOT NULL DEFAULT 0;
ALTER TABLE ai_usage ADD COLUMN IF NOT EXISTS cost_micros BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_ai_usage_user_created ON ai_usage(user_id, created_at);
CREATE TABLE IF NOT EXISTS content_items (
 id UUID PRIMARY KEY, kind TEXT NOT NULL, external_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL,
 payload JSONB NOT NULL, published BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(kind, external_id)
);
CREATE INDEX IF NOT EXISTS idx_content_items_kind_published ON content_items(kind, published);
CREATE TABLE IF NOT EXISTS ai_policies (id UUID PRIMARY KEY,user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,period TEXT NOT NULL,request_count INT NOT NULL DEFAULT 0,input_tokens INT NOT NULL DEFAULT 0,output_tokens INT NOT NULL DEFAULT 0,cost_micros BIGINT NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(user_id,period));
CREATE TABLE IF NOT EXISTS ai_safety_events (id UUID PRIMARY KEY,user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,reason TEXT NOT NULL,content_hash TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'open',reviewer_id UUID REFERENCES users(id),decision TEXT NOT NULL DEFAULT '',reviewed_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
ALTER TABLE ai_safety_events ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open';
ALTER TABLE ai_safety_events ADD COLUMN IF NOT EXISTS reviewer_id UUID REFERENCES users(id);
ALTER TABLE ai_safety_events ADD COLUMN IF NOT EXISTS decision TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_safety_events ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_ai_policies_period ON ai_policies(period);
CREATE TABLE IF NOT EXISTS auth_sessions (id UUID PRIMARY KEY,user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,refresh_token_hash TEXT NOT NULL UNIQUE,device_id UUID REFERENCES devices(id) ON DELETE SET NULL,expires_at TIMESTAMPTZ NOT NULL,revoked_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id,revoked_at);
CREATE TABLE IF NOT EXISTS audit_logs (id UUID PRIMARY KEY,user_id UUID REFERENCES users(id) ON DELETE SET NULL,action TEXT NOT NULL,resource TEXT NOT NULL DEFAULT '',metadata JSONB NOT NULL DEFAULT '{}'::jsonb,ip TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);
CREATE TABLE IF NOT EXISTS sync_cursors (user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,cursor_at TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01',updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(user_id,device_id));
CREATE TABLE IF NOT EXISTS ai_plans (id UUID PRIMARY KEY,name TEXT NOT NULL UNIQUE,monthly_requests INT NOT NULL DEFAULT 500,input_cost_micros_per_1k INT NOT NULL DEFAULT 100,output_cost_micros_per_1k INT NOT NULL DEFAULT 300,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_plan_id UUID REFERENCES ai_plans(id);
CREATE TABLE IF NOT EXISTS app_configs (key TEXT PRIMARY KEY,value JSONB NOT NULL DEFAULT '{}'::jsonb,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS user_files (id UUID PRIMARY KEY,user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,storage_key TEXT NOT NULL UNIQUE,name TEXT NOT NULL,size_bytes BIGINT NOT NULL,content_type TEXT NOT NULL DEFAULT 'application/octet-stream',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_user_files_user ON user_files(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS account_tokens (id UUID PRIMARY KEY,user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,token_hash TEXT NOT NULL UNIQUE,purpose TEXT NOT NULL,expires_at TIMESTAMPTZ NOT NULL,used_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_account_tokens_lookup ON account_tokens(token_hash,purpose,used_at,expires_at);
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS login_failures INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
