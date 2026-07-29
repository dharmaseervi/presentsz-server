CREATE TABLE IF NOT EXISTS professor_password_reset_otps (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professor_id UUID NOT NULL REFERENCES professors(id) ON DELETE CASCADE,
    otp_hash     TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    used         BOOLEAN DEFAULT false,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_prof_reset_otps_professor ON professor_password_reset_otps(professor_id, used, expires_at);