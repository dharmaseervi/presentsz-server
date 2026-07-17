-- Add faculty_id for professor login
ALTER TABLE professors ADD COLUMN IF NOT EXISTS faculty_id TEXT UNIQUE;

-- Index
CREATE INDEX IF NOT EXISTS idx_professors_faculty_id ON professors(faculty_id);

-- Give existing admin a faculty ID (in case admin needs it too, but admin uses email)
-- Skip if you want admin to only use email