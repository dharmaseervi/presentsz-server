ALTER TABLE students ADD COLUMN IF NOT EXISTS pending_email TEXT;
ALTER TABLE students ADD COLUMN IF NOT EXISTS pending_email_requested_at TIMESTAMPTZ;