CREATE TABLE IF NOT EXISTS report_subscriptions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professor_id  UUID NOT NULL REFERENCES professors(id) ON DELETE CASCADE,
    subject_code  TEXT,                    -- NULL = all subjects this professor teaches
    frequency     TEXT NOT NULL CHECK (frequency IN ('weekly', 'monthly')),
    enabled       BOOLEAN DEFAULT true,
    last_sent_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(professor_id, subject_code, frequency)
);

CREATE INDEX IF NOT EXISTS idx_report_subs_active ON report_subscriptions(enabled, frequency);