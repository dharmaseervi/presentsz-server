-- migrations/002_timetable_add_date.sql
ALTER TABLE timetable DROP COLUMN IF EXISTS day;
ALTER TABLE timetable ADD COLUMN IF NOT EXISTS class_date DATE NOT NULL DEFAULT CURRENT_DATE;
-- Index for fast date queries
CREATE INDEX IF NOT EXISTS idx_timetable_date ON timetable(class_date);