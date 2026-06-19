package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourusername/presentsz-server/internal/db"
)

// POST /sessions
func StartSession(c *gin.Context) {
	professorID, _ := c.Get("user_id")
	var req struct {
		RoomID  string `json:"room_id" binding:"required"`
		Subject string `json:"subject" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Stop any existing active session in this room
	db.Pool.Exec(context.Background(),
		`UPDATE attendance_sessions SET active = false, end_time = NOW()
		 WHERE room_id = $1 AND active = true`, req.RoomID)

	// Session lasts 1 hour by default
	endTime := time.Now().Add(1 * time.Hour)

	var sessionID string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO attendance_sessions (room_id, professor_id, subject, active, end_time)
		 VALUES ($1, $2, $3, true, $4) RETURNING id`,
		req.RoomID, professorID, req.Subject, endTime,
	).Scan(&sessionID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start session"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"session_id": sessionID,
		"end_time":   endTime.Format(time.RFC3339),
		"message":    "attendance session started",
	})
}

// GET /sessions/active?year=2nd Year
func GetActiveSession(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year required"})
		return
	}

	studentID, _ := c.Get("user_id")

	var sessionID, subject, roomName string
	var endTime *time.Time

	err := db.Pool.QueryRow(context.Background(),
		`SELECT s.id, s.subject, r.room_name, s.end_time
		 FROM attendance_sessions s
		 JOIN classrooms r ON r.id = s.room_id
		 WHERE r.year = $1 AND s.active = true
		 ORDER BY s.start_time DESC LIMIT 1`,
		year,
	).Scan(&sessionID, &subject, &roomName, &endTime)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	// Auto-expire if past end_time (only when set)
	if endTime != nil && time.Now().After(*endTime) {
		db.Pool.Exec(context.Background(),
			`UPDATE attendance_sessions SET active = false WHERE id = $1`, sessionID)
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	// Check if student already marked attendance
	var alreadyMarked bool
	db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM attendance
			WHERE session_id = $1 AND student_id = $2
		)`, sessionID, studentID,
	).Scan(&alreadyMarked)

	resp := gin.H{
		"active":         true,
		"session_id":     sessionID,
		"subject":        subject,
		"room_name":      roomName,
		"already_marked": alreadyMarked,
	}
	if endTime != nil {
		resp["end_time"] = endTime.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

// POST /sessions/:session_id/stop
func StopSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE attendance_sessions SET active = false, end_time = NOW() WHERE id = $1`, sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stop session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "session stopped"})
}

// POST /attendance/mark
func MarkAttendance(c *gin.Context) {
	studentID, _ := c.Get("user_id")
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var active bool
	err := db.Pool.QueryRow(context.Background(),
		`SELECT active FROM attendance_sessions WHERE id = $1`, req.SessionID,
	).Scan(&active)

	if err != nil || !active {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session is not active"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO attendance (session_id, student_id, status)
		 VALUES ($1, $2, 'present')
		 ON CONFLICT (session_id, student_id) DO NOTHING`,
		req.SessionID, studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark attendance"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "attendance marked"})
}

// POST /attendance/ble  (called by ESP32)
func MarkAttendanceBLE(c *gin.Context) {
	var req struct {
		BLEUUID   string `json:"ble_uuid" binding:"required"`
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var studentID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM students WHERE ble_uuid = $1`, req.BLEUUID,
	).Scan(&studentID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO attendance (session_id, student_id, status)
		 VALUES ($1, $2, 'present')
		 ON CONFLICT (session_id, student_id) DO NOTHING`,
		req.SessionID, studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark attendance"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "attendance marked via BLE"})
}

// GET /sessions/:session_id/attendance
func GetSessionAttendance(c *gin.Context) {
	sessionID := c.Param("session_id")

	rows, err := db.Pool.Query(context.Background(),
		`SELECT s.name, s.roll_number, s.department, a.status, a.marked_at
		 FROM attendance a
		 JOIN students s ON s.id = a.student_id
		 WHERE a.session_id = $1
		 ORDER BY a.marked_at ASC`,
		sessionID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch attendance"})
		return
	}
	defer rows.Close()

	type Record struct {
		Name       string `json:"name"`
		RollNumber string `json:"roll_number"`
		Department string `json:"department"`
		Status     string `json:"status"`
		MarkedAt   string `json:"marked_at"`
	}

	var records []Record
	for rows.Next() {
		var r Record
		rows.Scan(&r.Name, &r.RollNumber, &r.Department, &r.Status, &r.MarkedAt)
		records = append(records, r)
	}

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID, "count": len(records), "records": records})
}

// GET /students/:id
func GetStudent(c *gin.Context) {
	studentID := c.Param("id")

	var s struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		RollNumber string `json:"roll_number"`
		Department string `json:"department"`
		Year       string `json:"year"`
		Semester   string `json:"semester"`
	}

	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email, phone, roll_number, department, year, semester
		 FROM students WHERE id = $1`, studentID,
	).Scan(&s.ID, &s.Name, &s.Email, &s.Phone, &s.RollNumber, &s.Department, &s.Year, &s.Semester)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	c.JSON(http.StatusOK, s)
}

// GET /classrooms
func GetClassrooms(c *gin.Context) {
	rows, _ := db.Pool.Query(context.Background(),
		`SELECT id, room_name, year FROM classrooms ORDER BY room_name`)
	defer rows.Close()

	type Room struct {
		ID       string `json:"id"`
		RoomName string `json:"room_name"`
		Year     string `json:"year"`
	}
	var rooms []Room
	for rows.Next() {
		var r Room
		rows.Scan(&r.ID, &r.RoomName, &r.Year)
		rooms = append(rooms, r)
	}
	if rooms == nil {
		rooms = []Room{}
	}
	c.JSON(200, gin.H{"classrooms": rooms})
}

// GET /timetable?year=2nd Year&day=Monday
func GetTimetable(c *gin.Context) {
	year := c.Query("year")
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT t.id, t.time_slot, t.subject, r.room_name
         FROM timetable t
         JOIN classrooms r ON r.id = t.room_id
         WHERE t.year = $1 AND t.class_date = $2
         ORDER BY t.time_slot`,
		year, dateStr,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch timetable"})
		return
	}
	defer rows.Close()

	type Entry struct {
		ID       string `json:"id"`
		TimeSlot string `json:"time_slot"`
		Subject  string `json:"subject"`
		RoomName string `json:"room_name"`
	}
	var entries []Entry
	for rows.Next() {
		var e Entry
		rows.Scan(&e.ID, &e.TimeSlot, &e.Subject, &e.RoomName)
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []Entry{}
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "date": dateStr})
}

// POST /timetable
func AddTimetableEntry(c *gin.Context) {
	var req struct {
		Year      string `json:"year" binding:"required"`
		ClassDate string `json:"class_date" binding:"required"`
		TimeSlot  string `json:"time_slot" binding:"required"`
		Subject   string `json:"subject" binding:"required"`
		RoomID    string `json:"room_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO timetable (year, class_date, time_slot, subject, room_id)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		req.Year, req.ClassDate, req.TimeSlot, req.Subject, req.RoomID,
	).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add entry"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// DELETE /timetable/:id
func DeleteTimetableEntry(c *gin.Context) {
	id := c.Param("id")
	_, err := db.Pool.Exec(context.Background(),
		`DELETE FROM timetable WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete entry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "entry deleted"})
}

// GET /students/:id/attendance
func GetStudentAttendance(c *gin.Context) {
	studentID := c.Param("id")

	rows, err := db.Pool.Query(context.Background(),
		`SELECT s.id, s.subject, r.room_name, a.status, a.marked_at
		 FROM attendance a
		 JOIN attendance_sessions s ON s.id = a.session_id
		 JOIN classrooms r ON r.id = s.room_id
		 WHERE a.student_id = $1
		 ORDER BY a.marked_at DESC`,
		studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch attendance"})
		return
	}
	defer rows.Close()

	type Record struct {
		SessionID string `json:"session_id"`
		Subject   string `json:"subject"`
		RoomName  string `json:"room_name"`
		Status    string `json:"status"`
		MarkedAt  string `json:"marked_at"`
	}

	var records []Record
	for rows.Next() {
		var r Record
		var markedAt time.Time // ← scan as time.Time
		rows.Scan(&r.SessionID, &r.Subject, &r.RoomName, &r.Status, &markedAt)
		r.MarkedAt = markedAt.Format(time.RFC3339) // ← format as RFC3339
		records = append(records, r)
	}

	if records == nil {
		records = []Record{}
	}

	c.JSON(http.StatusOK, gin.H{
		"student_id": studentID,
		"count":      len(records),
		"records":    records,
	})
}

// POST /timetable/copy-week
func CopyWeek(c *gin.Context) {
	var req struct {
		FromDate string `json:"from_date" binding:"required"` // any date in source week
		ToDate   string `json:"to_date" binding:"required"`   // any date in target week
		Year     string `json:"year" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fromDate, _ := time.Parse("2006-01-02", req.FromDate)
	toDate, _ := time.Parse("2006-01-02", req.ToDate)

	// Get Monday of each week
	fromWd := int(fromDate.Weekday())
	if fromWd == 0 {
		fromWd = 7
	}
	toWd := int(toDate.Weekday())
	if toWd == 0 {
		toWd = 7
	}
	fromMonday := fromDate.AddDate(0, 0, -(fromWd - 1))
	toMonday := toDate.AddDate(0, 0, -(toWd - 1))
	diff := toMonday.Sub(fromMonday)

	// Get source week entries
	rows, err := db.Pool.Query(context.Background(),
		`SELECT time_slot, subject, room_id, class_date
         FROM timetable
         WHERE year = $1
           AND class_date >= $2
           AND class_date < $2 + INTERVAL '6 days'`,
		req.Year, fromMonday.Format("2006-01-02"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read source week"})
		return
	}
	defer rows.Close()

	type Entry struct {
		TimeSlot  string
		Subject   string
		RoomID    string
		ClassDate time.Time
	}
	var entries []Entry
	for rows.Next() {
		var e Entry
		rows.Scan(&e.TimeSlot, &e.Subject, &e.RoomID, &e.ClassDate)
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no entries found in source week"})
		return
	}

	copied := 0
	for _, e := range entries {
		newDate := e.ClassDate.Add(diff)

		// Skip if already exists
		var exists bool
		db.Pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM timetable WHERE class_date=$1 AND time_slot=$2 AND room_id=$3)`,
			newDate, e.TimeSlot, e.RoomID,
		).Scan(&exists)
		if exists {
			continue
		}

		_, err := db.Pool.Exec(context.Background(),
			`INSERT INTO timetable (year, class_date, time_slot, subject, room_id)
             VALUES ($1,$2,$3,$4,$5)`,
			req.Year, newDate, e.TimeSlot, e.Subject, e.RoomID,
		)
		if err == nil {
			copied++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Copied %d entries to week of %s", copied, toMonday.Format("Jan 2")),
		"copied":  copied,
	})
}

// GET /timetable/week?year=2nd Year
func GetTimetableWeek(c *gin.Context) {
	year := c.Query("year")
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	// Find Monday of the week containing the given date
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	// Get Monday of that week
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7
	} // Sunday = 7
	monday := date.AddDate(0, 0, -(weekday - 1))
	saturday := monday.AddDate(0, 0, 5)

	rows, err := db.Pool.Query(context.Background(),
		`SELECT t.id, t.class_date, t.time_slot, t.subject, t.room_id, r.room_name
         FROM timetable t
         JOIN classrooms r ON r.id = t.room_id
         WHERE t.year = $1
           AND t.class_date >= $2
           AND t.class_date <= $3
         ORDER BY t.class_date, t.time_slot`,
		year, monday.Format("2006-01-02"), saturday.Format("2006-01-02"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch timetable"})
		return
	}
	defer rows.Close()

	type Entry struct {
		ID        string `json:"id"`
		ClassDate string `json:"class_date"`
		TimeSlot  string `json:"time_slot"`
		Subject   string `json:"subject"`
		RoomID    string `json:"room_id"`
		RoomName  string `json:"room_name"`
	}
	var entries []Entry
	for rows.Next() {
		var e Entry
		var classDate time.Time
		rows.Scan(&e.ID, &classDate, &e.TimeSlot, &e.Subject, &e.RoomID, &e.RoomName)
		e.ClassDate = classDate.Format("2006-01-02")
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []Entry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"entries":    entries,
		"week_start": monday.Format("2006-01-02"),
		"week_end":   saturday.Format("2006-01-02"),
	})
}

// GET /classrooms/:room_name/count
func GetClassroomCount(c *gin.Context) {
	roomName := c.Param("room_name")

	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM students s
         JOIN classrooms r ON r.year = s.year
         WHERE r.room_name = $1`,
		roomName,
	).Scan(&count)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get count"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"room_name": roomName, "count": count})
}

// GET /professor/:id/sessions
func GetProfessorSessions(c *gin.Context) {
	professorID := c.Param("id")

	rows, err := db.Pool.Query(context.Background(),
		`SELECT s.id, s.subject, r.room_name, s.active,
		        s.start_time, s.end_time,
		        COUNT(a.id) as attendance_count
		 FROM attendance_sessions s
		 JOIN classrooms r ON r.id = s.room_id
		 LEFT JOIN attendance a ON a.session_id = s.id
		 WHERE s.professor_id = $1
		 GROUP BY s.id, r.room_name
		 ORDER BY s.start_time DESC
		 LIMIT 50`,
		professorID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch sessions"})
		return
	}
	defer rows.Close()

	type Session struct {
		ID              string  `json:"id"`
		Subject         string  `json:"subject"`
		RoomName        string  `json:"room_name"`
		Active          bool    `json:"active"`
		StartTime       string  `json:"start_time"`
		EndTime         *string `json:"end_time"`
		AttendanceCount int     `json:"attendance_count"`
	}

	var sessions []Session
	for rows.Next() {
		var s Session
		var startTime time.Time
		var endTime *time.Time
		rows.Scan(&s.ID, &s.Subject, &s.RoomName, &s.Active,
			&startTime, &endTime, &s.AttendanceCount)
		s.StartTime = startTime.Format(time.RFC3339)
		if endTime != nil {
			t := endTime.Format(time.RFC3339)
			s.EndTime = &t
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		sessions = []Session{}
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}
