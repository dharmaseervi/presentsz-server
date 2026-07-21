ALTER TABLE professors ADD COLUMN IF NOT EXISTS password_reset_required BOOLEAN DEFAULT TRUE;
ALTER TABLE professors ADD COLUMN IF NOT EXISTS password_expires_at TIMESTAMPTZ DEFAULT (NOW() + INTERVAL '7 days');

-- Don't force existing professors who already have real passwords set
UPDATE professors
SET password_reset_required = false, password_expires_at = NULL
WHERE created_at < NOW();