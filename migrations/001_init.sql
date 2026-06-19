CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE students (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name          TEXT NOT NULL,
    email         TEXT UNIQUE NOT NULL,
    phone         TEXT NOT NULL,
    roll_number   TEXT UNIQUE NOT NULL,
    department    TEXT NOT NULL,
    year          TEXT NOT NULL,
    semester      TEXT NOT NULL,
    ble_uuid      TEXT UNIQUE,
    device_id     TEXT UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE professors (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name          TEXT NOT NULL,
    email         TEXT UNIQUE NOT NULL,
    subject       TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE classrooms (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    room_name   TEXT UNIQUE NOT NULL,
    esp32_front TEXT,
    esp32_back  TEXT,
    year        TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE attendance_sessions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    room_id      UUID NOT NULL REFERENCES classrooms(id),
    professor_id UUID NOT NULL REFERENCES professors(id),
    subject      TEXT NOT NULL,
    active       BOOLEAN DEFAULT TRUE,
    start_time   TIMESTAMPTZ DEFAULT NOW(),
    end_time     TIMESTAMPTZ
);

CREATE TABLE attendance (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES attendance_sessions(id),
    student_id UUID NOT NULL REFERENCES students(id),
    status     TEXT NOT NULL DEFAULT 'present',
    marked_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(session_id, student_id)
);

CREATE TABLE timetable (
    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    year      TEXT NOT NULL,
    day       TEXT NOT NULL,
    time_slot TEXT NOT NULL,
    subject   TEXT NOT NULL,
    room_id   UUID REFERENCES classrooms(id)
);
