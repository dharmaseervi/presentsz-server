-- Add 'late' as a valid status
ALTER TABLE attendance DROP CONSTRAINT IF EXISTS attendance_status_check;
ALTER TABLE attendance ADD CONSTRAINT attendance_status_check
  CHECK (status IN ('present', 'late', 'absent', 'excused'));

-- Track how attendance was marked
ALTER TABLE attendance ADD COLUMN IF NOT EXISTS marked_by TEXT
  CHECK (marked_by IN ('student', 'professor', 'ble', 'system'))
  DEFAULT 'student';

-- For audit log
ALTER TABLE attendance ADD COLUMN IF NOT EXISTS marked_by_user_id UUID;