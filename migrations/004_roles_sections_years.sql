-- ============================================
-- Migration 004: Roles + Sections + Academic Years
-- ============================================

-- ==========================================
-- 1. ACADEMIC YEARS
-- ==========================================
CREATE TABLE IF NOT EXISTS academic_years (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    year_name   TEXT UNIQUE NOT NULL,       -- e.g., "2025-2026"
    start_date  DATE NOT NULL,
    end_date    DATE NOT NULL,
    is_active   BOOLEAN DEFAULT true,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Insert current academic year
INSERT INTO academic_years (year_name, start_date, end_date, is_active)
VALUES ('2025-2026', '2025-06-01', '2026-05-31', true)
ON CONFLICT (year_name) DO NOTHING;

-- ==========================================
-- 2. SECTIONS (e.g., CSE-1A, IT-2B)
-- ==========================================
CREATE TABLE IF NOT EXISTS sections (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section_code        TEXT UNIQUE NOT NULL,  -- e.g., "CSE-1A"
    department          TEXT NOT NULL,          -- e.g., "CSE"
    year                TEXT NOT NULL,          -- e.g., "1st Year"
    section_letter      TEXT NOT NULL DEFAULT 'A',  -- A, B, C
    academic_year_id    UUID REFERENCES academic_years(id),
    capacity            INT DEFAULT 60,
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

-- Sample sections
INSERT INTO sections (section_code, department, year, section_letter, academic_year_id, capacity)
VALUES 
    ('CSE-1A', 'CSE', '1st Year', 'A', (SELECT id FROM academic_years WHERE year_name = '2025-2026'), 60)
ON CONFLICT (section_code) DO NOTHING;

-- ==========================================
-- 3. UPDATE STUDENTS
-- ==========================================
ALTER TABLE students ADD COLUMN IF NOT EXISTS section_id UUID REFERENCES sections(id);
ALTER TABLE students ADD COLUMN IF NOT EXISTS password_reset_required BOOLEAN DEFAULT TRUE;
ALTER TABLE students ADD COLUMN IF NOT EXISTS password_expires_at TIMESTAMPTZ DEFAULT (NOW() + INTERVAL '7 days');
ALTER TABLE students ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES professors(id);

-- Make phone optional (Excel upload might not have it)
ALTER TABLE students ALTER COLUMN phone DROP NOT NULL;

-- Update existing students to not require password reset
UPDATE students SET password_reset_required = false, password_expires_at = NULL 
WHERE created_at < NOW();

-- ==========================================
-- 4. UPDATE PROFESSORS
-- ==========================================
ALTER TABLE professors ADD COLUMN IF NOT EXISTS role TEXT DEFAULT 'professor'
    CHECK (role IN ('admin', 'professor'));
ALTER TABLE professors ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES professors(id);
ALTER TABLE professors ADD COLUMN IF NOT EXISTS department TEXT;

-- ==========================================
-- 5. UPDATE CLASSROOMS
-- ==========================================
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS section_id UUID REFERENCES sections(id);

-- ==========================================
-- 6. PROFESSOR_SUBJECTS (Many-to-Many)
-- Which prof teaches which subject to which section
-- ==========================================
CREATE TABLE IF NOT EXISTS professor_subjects (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professor_id        UUID NOT NULL REFERENCES professors(id) ON DELETE CASCADE,
    section_id          UUID NOT NULL REFERENCES sections(id) ON DELETE CASCADE,
    subject             TEXT NOT NULL,
    academic_year_id    UUID REFERENCES academic_years(id),
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(professor_id, section_id, subject)
);

-- ==========================================
-- 7. INDEXES for performance
-- ==========================================
CREATE INDEX IF NOT EXISTS idx_students_roll_number ON students(roll_number);
CREATE INDEX IF NOT EXISTS idx_students_section ON students(section_id);
CREATE INDEX IF NOT EXISTS idx_professors_role ON professors(role);
CREATE INDEX IF NOT EXISTS idx_professors_department ON professors(department);
CREATE INDEX IF NOT EXISTS idx_sections_dept_year ON sections(department, year);
CREATE INDEX IF NOT EXISTS idx_professor_subjects_prof ON professor_subjects(professor_id);
CREATE INDEX IF NOT EXISTS idx_professor_subjects_section ON professor_subjects(section_id);

-- ==========================================
-- 8. Add academic_year_id to sessions & timetable
-- ==========================================
ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS section_id UUID REFERENCES sections(id);
ALTER TABLE timetable ADD COLUMN IF NOT EXISTS section_id UUID REFERENCES sections(id);
ALTER TABLE timetable ADD COLUMN IF NOT EXISTS academic_year_id UUID REFERENCES academic_years(id);