package models

import "time"

type Student struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	RollNumber   string    `json:"roll_number"`
	Department   string    `json:"department"`
	Year         string    `json:"year"`
	Semester     string    `json:"semester"`
	BLEUUID      *string   `json:"ble_uuid,omitempty"`
	DeviceID     *string   `json:"device_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Professor struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

type Classroom struct {
	ID         string    `json:"id"`
	RoomName   string    `json:"room_name"`
	ESP32Front *string   `json:"esp32_front,omitempty"`
	ESP32Back  *string   `json:"esp32_back,omitempty"`
	Year       string    `json:"year"`
	CreatedAt  time.Time `json:"created_at"`
}

type AttendanceSession struct {
	ID          string     `json:"id"`
	RoomID      string     `json:"room_id"`
	ProfessorID string     `json:"professor_id"`
	Subject     string     `json:"subject"`
	Active      bool       `json:"active"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     *time.Time `json:"end_time,omitempty"`
}

type AttendanceRecord struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	StudentID string    `json:"student_id"`
	Status    string    `json:"status"`
	MarkedAt  time.Time `json:"marked_at"`
}
