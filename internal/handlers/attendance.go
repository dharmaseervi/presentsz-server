package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourusername/presentsz-server/internal/db"
)

// POST /sessions
// POST /sessions
func StartSession(c *gin.Context) {
	professorID, _ := c.Get("user_id")

	var req struct {
		RoomCode    string `json:"room_code" binding:"required"`
		SubjectCode string `json:"subject_code" binding:"required"`
		Section     string `json:"section" binding:"required"`
		Semester    string `json:"semester" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var roomID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM classrooms WHERE room_code = $1`, strings.ToUpper(req.RoomCode),
	).Scan(&roomID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "room not found for code " + req.RoomCode})
		return
	}

	// Stop any existing active session for this professor (not just this room —
	// a professor can only run one class at a time)
	db.Pool.Exec(context.Background(),
		`UPDATE attendance_sessions SET active = false, end_time = NOW()
		 WHERE professor_id = $1 AND active = true`, professorID)

	endTime := time.Now().Add(1 * time.Hour)

	var sessionID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO attendance_sessions
		    (room_id, professor_id, subject, section, semester, active, end_time)
		 VALUES ($1, $2, $3, $4, $5, true, $6)
		 RETURNING id`,
		roomID, professorID, strings.ToUpper(req.SubjectCode),
		strings.ToUpper(req.Section), req.Semester, endTime,
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
// GET /sessions/active?section=A&semester=5
func GetActiveSession(c *gin.Context) {
	section := c.Query("section")
	semester := c.Query("semester")
	if section == "" || semester == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "section and semester required"})
		return
	}

	studentID, _ := c.Get("user_id")

	var sessionID, subject, roomName string
	var endTime *time.Time

	err := db.Pool.QueryRow(context.Background(),
		`SELECT s.id, s.subject, r.room_name, s.end_time
		 FROM attendance_sessions s
		 JOIN classrooms r ON r.id = s.room_id
		 WHERE s.section = $1 AND s.semester = $2 AND s.active = true
		 ORDER BY s.start_time DESC LIMIT 1`,
		strings.ToUpper(section), semester,
	).Scan(&sessionID, &subject, &roomName, &endTime)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	if endTime != nil && time.Now().After(*endTime) {
		db.Pool.Exec(context.Background(),
			`UPDATE attendance_sessions SET active = false WHERE id = $1`, sessionID)
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	var alreadyMarked bool
	db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM attendance WHERE session_id = $1 AND student_id = $2)`,
		sessionID, studentID,
	).Scan(&alreadyMarked)

	resp := gin.H{
		"active": true, "session_id": sessionID, "subject": subject,
		"room_name": roomName, "already_marked": alreadyMarked,
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
const LATE_THRESHOLD_MINUTES = 15

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

	var (
		active         bool
		sessionSection string
		sessionSem     string
		startTime      time.Time
	)
	err := db.Pool.QueryRow(context.Background(),
		`SELECT active, section, semester, start_time FROM attendance_sessions WHERE id = $1`,
		req.SessionID,
	).Scan(&active, &sessionSection, &sessionSem, &startTime)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if !active {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session is not active"})
		return
	}

	var studentSectionLetter, studentSemester string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(sec.section_letter, ''), s.semester
		 FROM students s
		 LEFT JOIN sections sec ON sec.id = s.section_id
		 WHERE s.id = $1`,
		studentID,
	).Scan(&studentSectionLetter, &studentSemester)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	if strings.ToUpper(studentSectionLetter) != sessionSection || studentSemester != sessionSem {
		c.JSON(http.StatusForbidden, gin.H{"error": "student not assigned to this class"})
		return
	}

	var alreadyMarked bool
	db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM attendance WHERE session_id = $1 AND student_id = $2)`,
		req.SessionID, studentID,
	).Scan(&alreadyMarked)
	if alreadyMarked {
		c.JSON(http.StatusConflict, gin.H{"error": "attendance already marked"})
		return
	}

	status := "present"
	if time.Since(startTime).Minutes() > LATE_THRESHOLD_MINUTES {
		status = "late"
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO attendance (session_id, student_id, status, marked_by, marked_by_user_id)
		 VALUES ($1, $2, $3, 'student', $2)`,
		req.SessionID, studentID, status,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark attendance"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "attendance marked", "session": req.SessionID, "status": status})
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
		`SELECT s.name, s.roll_number, s.department, a.status, a.marked_at, a.marked_by
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
		MarkedBy   string `json:"marked_by"`
	}

	var records []Record
	for rows.Next() {
		var r Record
		rows.Scan(&r.Name, &r.RollNumber, &r.Department, &r.Status, &r.MarkedAt, &r.MarkedBy)
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
		`SELECT id, room_name, COALESCE(room_code, ''), year FROM classrooms ORDER BY room_name`)
	defer rows.Close()

	type Room struct {
		ID       string `json:"id"`
		RoomName string `json:"room_name"`
		RoomCode string `json:"room_code"`
		Year     string `json:"year"`
	}
	var rooms []Room
	for rows.Next() {
		var r Room
		rows.Scan(&r.ID, &r.RoomName, &r.RoomCode, &r.Year)
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
		Day         string `json:"day" binding:"required"`
		StartTime   string `json:"start_time" binding:"required"`
		EndTime     string `json:"end_time" binding:"required"`
		SubjectCode string `json:"subject_code" binding:"required"`
		FacultyCode string `json:"faculty_code" binding:"required"`
		RoomCode    string `json:"room_code" binding:"required"`
		Section     string `json:"section" binding:"required"`
		Semester    string `json:"semester" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO timetable
		    (day, start_time, end_time, subject_code, faculty_code, room_code, section, semester, status)
		 VALUES ($1, $2::time, $3::time, $4, $5, $6, $7, $8, 'Active')
		 RETURNING id`,
		req.Day, req.StartTime, req.EndTime,
		strings.ToUpper(req.SubjectCode), strings.ToUpper(req.FacultyCode), strings.ToUpper(req.RoomCode),
		strings.ToUpper(req.Section), req.Semester,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "class added"})
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

// GET /timetable/week?semester=3&section=A
// Returns the full recurring weekly timetable (Mon–Sat) for a given semester/section
func GetTimetableWeek(c *gin.Context) {
	semester := c.Query("semester")
	section := c.Query("section")

	if semester == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "semester is required"})
		return
	}
	if section == "" {
		section = "A"
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT t.id, t.day, t.start_time::text, t.end_time::text,
		        t.subject_code, COALESCE(s.subject_name,''),
		        t.faculty_code, COALESCE(p.name,''),
		        t.room_code, t.section, t.semester
		 FROM timetable t
		 LEFT JOIN subjects s ON s.subject_code = t.subject_code
		 LEFT JOIN professors p ON p.faculty_id = t.faculty_code
		 WHERE t.semester = $1 AND t.section = $2
		 ORDER BY
		    CASE t.day
		      WHEN 'Monday' THEN 1 WHEN 'Tuesday' THEN 2 WHEN 'Wednesday' THEN 3
		      WHEN 'Thursday' THEN 4 WHEN 'Friday' THEN 5 WHEN 'Saturday' THEN 6
		      ELSE 7
		    END,
		    t.start_time`,
		semester, strings.ToUpper(section),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var entries []gin.H
	for rows.Next() {
		var id, day, start, end, subCode, subName, facCode, facName, room, sec, sem string
		if err := rows.Scan(&id, &day, &start, &end, &subCode, &subName, &facCode, &facName, &room, &sec, &sem); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		entries = append(entries, gin.H{
			"id": id, "day": day, "start_time": start, "end_time": end,
			"subject_code": subCode, "subject_name": subName,
			"faculty_code": facCode, "faculty_name": facName,
			"room_code": room, "section": sec, "semester": sem,
		})
	}
	if entries == nil {
		entries = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"entries":  entries,
		"semester": semester,
		"section":  section,
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

// GET /sessions/:session_id/students
// Returns all students eligible for this session (matched by year)
func GetEligibleStudents(c *gin.Context) {
	sessionID := c.Param("session_id")

	rows, err := db.Pool.Query(context.Background(),
		`SELECT s.id, s.name, s.roll_number, s.department,
		        EXISTS(SELECT 1 FROM attendance a WHERE a.session_id = $1 AND a.student_id = s.id) as marked,
		        COALESCE((SELECT a.status FROM attendance a WHERE a.session_id = $1 AND a.student_id = s.id), '') as status,
		        COALESCE((SELECT a.marked_by FROM attendance a WHERE a.session_id = $1 AND a.student_id = s.id), '') as marked_by
		 FROM students s
		 JOIN attendance_sessions sess ON sess.id = $1
		 JOIN classrooms r ON r.id = sess.room_id
		 WHERE s.year = r.year
		 ORDER BY s.roll_number`,
		sessionID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Student struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		RollNumber string `json:"roll_number"`
		Department string `json:"department"`
		Marked     bool   `json:"marked"`
		Status     string `json:"status"`
		MarkedBy   string `json:"marked_by"`
	}

	var students []Student
	for rows.Next() {
		var s Student
		rows.Scan(&s.ID, &s.Name, &s.RollNumber, &s.Department, &s.Marked, &s.Status, &s.MarkedBy)
		students = append(students, s)
	}
	if students == nil {
		students = []Student{}
	}

	c.JSON(http.StatusOK, gin.H{"students": students, "count": len(students)})
}

// POST /sessions/:session_id/override
// Professor manually marks a student present/late/absent/excused
func OverrideAttendance(c *gin.Context) {
	sessionID := c.Param("session_id")
	professorID, _ := c.Get("user_id")

	var req struct {
		StudentID string `json:"student_id" binding:"required"`
		Status    string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Status != "present" && req.Status != "late" && req.Status != "absent" && req.Status != "excused" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	// Upsert — overwrite existing attendance
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO attendance (session_id, student_id, status, marked_by, marked_by_user_id)
		 VALUES ($1, $2, $3, 'professor', $4)
		 ON CONFLICT (session_id, student_id) DO UPDATE
		 SET status = EXCLUDED.status,
		     marked_by = 'professor',
		     marked_by_user_id = EXCLUDED.marked_by_user_id,
		     marked_at = NOW()`,
		sessionID, req.StudentID, req.Status, professorID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Override: prof=%v marked student=%s as %s in session=%s", professorID, req.StudentID, req.Status, sessionID)
	c.JSON(http.StatusOK, gin.H{"message": "attendance updated"})
}

// DELETE /sessions/:session_id/attendance/:student_id
// Professor removes a student's attendance mark
func RemoveAttendance(c *gin.Context) {
	sessionID := c.Param("session_id")
	studentID := c.Param("student_id")

	_, err := db.Pool.Exec(context.Background(),
		`DELETE FROM attendance WHERE session_id = $1 AND student_id = $2`,
		sessionID, studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "attendance removed"})
}

// GET /esp32/sessions/active?year=2nd Year
func GetESP32ActiveSession(c *gin.Context) {
	year := c.Query("year")
	if year == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year required"})
		return
	}

	var sessionID, subject, roomName string
	var endTime *time.Time

	err := db.Pool.QueryRow(
		context.Background(),
		`SELECT s.id, s.subject, r.room_name, s.end_time
         FROM attendance_sessions s
         JOIN classrooms r ON r.id = s.room_id
         WHERE r.year = $1 AND s.active = true
         ORDER BY s.start_time DESC
         LIMIT 1`,
		year,
	).Scan(&sessionID, &subject, &roomName, &endTime)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	resp := gin.H{
		"active":     true,
		"session_id": sessionID,
		"subject":    subject,
		"room_name":  roomName,
	}

	if endTime != nil {
		resp["end_time"] = endTime.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

// GET /professor/subjects — subjects/sections this professor is allocated to teach
func GetProfessorSubjects(c *gin.Context) {
	profID, _ := c.Get("user_id")

	var facultyID string
	db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(faculty_id,'') FROM professors WHERE id = $1`, profID).Scan(&facultyID)

	rows, _ := db.Pool.Query(context.Background(),
		`SELECT DISTINCT sa.subject_code, COALESCE(s.subject_name,''), sa.section, sa.semester
		 FROM subject_allocations sa
		 LEFT JOIN subjects s ON s.subject_code = sa.subject_code
		 WHERE sa.faculty_code = $1
		 ORDER BY sa.semester, sa.subject_code`, facultyID)
	defer rows.Close()

	var subjects []gin.H
	for rows.Next() {
		var code, name, section, sem string
		rows.Scan(&code, &name, &section, &sem)
		subjects = append(subjects, gin.H{
			"subject_code": code, "subject_name": name, "section": section, "semester": sem,
		})
	}
	c.JSON(http.StatusOK, gin.H{"subjects": subjects})
}

// GET /professor/me — the logged-in professor/HOD's own profile
func GetMyProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var (
		name, email, role, status          string
		facultyID, department, designation *string
	)
	err := db.Pool.QueryRow(context.Background(),
		`SELECT name, email, COALESCE(role,'professor'), COALESCE(status,'Active'),
		        faculty_id, department, designation
		 FROM professors WHERE id = $1`,
		userID,
	).Scan(&name, &email, &role, &status, &facultyID, &department, &designation)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name": name, "email": email, "role": role, "status": status,
		"faculty_id": facultyID, "department": department, "designation": designation,
	})
}
