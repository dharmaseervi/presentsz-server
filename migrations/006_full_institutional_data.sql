-- Migration 006: Full institutional data model

-- 1. HODs
CREATE TABLE IF NOT EXISTS hods (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hod_code    TEXT UNIQUE NOT NULL,
    name        TEXT NOT NULL,
    department  TEXT NOT NULL,
    email       TEXT,
    phone       TEXT,
    status      TEXT DEFAULT 'Active',
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- 2. SUBJECTS
CREATE TABLE IF NOT EXISTS subjects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_code  TEXT UNIQUE NOT NULL,
    subject_name  TEXT NOT NULL,
    department    TEXT NOT NULL,
    programme     TEXT,
    semester      TEXT,
    credits       INT DEFAULT 0,
    type          TEXT DEFAULT 'Theory',
    status        TEXT DEFAULT 'Active',
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- 3. FACULTY fields on professors
ALTER TABLE professors ADD COLUMN IF NOT EXISTS designation TEXT;
ALTER TABLE professors ADD COLUMN IF NOT EXISTS phone TEXT;
ALTER TABLE professors ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'Active';

-- 4. SUBJECT ALLOCATIONS
CREATE TABLE IF NOT EXISTS subject_allocations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_code    TEXT NOT NULL,
    faculty_code    TEXT NOT NULL,
    department      TEXT,
    programme       TEXT,
    semester        TEXT,
    section         TEXT DEFAULT 'A',
    academic_year   TEXT,
    status          TEXT DEFAULT 'Active',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(subject_code, faculty_code, section, semester, academic_year)
);

-- 5. CLASSROOMS extend
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS room_code TEXT UNIQUE;
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS building TEXT;
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS floor TEXT;
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS capacity INT DEFAULT 60;
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS room_type TEXT DEFAULT 'Classroom';
ALTER TABLE classrooms ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'Active';

-- 6. TIMETABLE rebuild
DROP TABLE IF EXISTS timetable CASCADE;
CREATE TABLE timetable (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    programme     TEXT,
    semester      TEXT,
    section       TEXT DEFAULT 'A',
    day           TEXT NOT NULL,
    start_time    TIME NOT NULL,
    end_time      TIME NOT NULL,
    subject_code  TEXT NOT NULL,
    faculty_code  TEXT NOT NULL,
    room_code     TEXT NOT NULL,
    academic_year TEXT,
    status        TEXT DEFAULT 'Active',
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- INDEXES
CREATE INDEX IF NOT EXISTS idx_subjects_code ON subjects(subject_code);
CREATE INDEX IF NOT EXISTS idx_subjects_dept_sem ON subjects(department, semester);
CREATE INDEX IF NOT EXISTS idx_hods_dept ON hods(department);
CREATE INDEX IF NOT EXISTS idx_alloc_subject ON subject_allocations(subject_code);
CREATE INDEX IF NOT EXISTS idx_alloc_faculty ON subject_allocations(faculty_code);
CREATE INDEX IF NOT EXISTS idx_alloc_section ON subject_allocations(section, semester);
CREATE INDEX IF NOT EXISTS idx_classrooms_code ON classrooms(room_code);
CREATE INDEX IF NOT EXISTS idx_timetable_lookup ON timetable(programme, semester, section, day);
CREATE INDEX IF NOT EXISTS idx_timetable_faculty ON timetable(faculty_code);
CREATE INDEX IF NOT EXISTS idx_timetable_subject ON timetable(subject_code);