package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourusername/presentsz-server/internal/db"
)

// GET /hod/stats
// Returns department-wide stats for the logged-in HOD's department
func GetHODStats(c *gin.Context) {
	hodID, _ := c.Get("user_id")

	var department string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(department, '') FROM professors WHERE id = $1`, hodID,
	).Scan(&department)
	if err != nil || department == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not determine HOD department"})
		return
	}

	// Total faculty in this department
	var facultyCount int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM professors
		 WHERE department = $1 AND COALESCE(role,'professor') IN ('professor','hod')`,
		department,
	).Scan(&facultyCount)

	// Total students in this department
	var studentCount int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM students WHERE department = $1`, department,
	).Scan(&studentCount)

	// Sessions run today, department-wide
	var sessionsToday int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM attendance_sessions s
		 JOIN professors p ON p.id = s.professor_id
		 WHERE p.department = $1 AND s.start_time::date = CURRENT_DATE`,
		department,
	).Scan(&sessionsToday)

	// Currently active sessions, department-wide
	var activeNow int
	db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM attendance_sessions s
		 JOIN professors p ON p.id = s.professor_id
		 WHERE p.department = $1 AND s.active = true`,
		department,
	).Scan(&activeNow)

	// Average attendance rate over last 30 days, department-wide
	var avgRate float64
	db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(
		    ROUND(
		      100.0 * COUNT(*) FILTER (WHERE a.status = 'present')
		      / NULLIF(COUNT(*), 0)
		    , 1), 0)
		 FROM attendance a
		 JOIN attendance_sessions s ON s.id = a.session_id
		 JOIN professors p ON p.id = s.professor_id
		 WHERE p.department = $1 AND s.start_time >= NOW() - INTERVAL '30 days'`,
		department,
	).Scan(&avgRate)

	// Per-faculty breakdown: sessions run + avg attendance, last 30 days
	rows, _ := db.Pool.Query(context.Background(),
		`SELECT p.name, p.faculty_id,
		        COUNT(DISTINCT s.id) as session_count,
		        COALESCE(ROUND(
		          100.0 * COUNT(a.*) FILTER (WHERE a.status = 'present')
		          / NULLIF(COUNT(a.*), 0)
		        , 1), 0) as avg_rate
		 FROM professors p
		 LEFT JOIN attendance_sessions s
		   ON s.professor_id = p.id AND s.start_time >= NOW() - INTERVAL '30 days'
		 LEFT JOIN attendance a ON a.session_id = s.id
		 WHERE p.department = $1 AND COALESCE(p.role,'professor') IN ('professor','hod')
		 GROUP BY p.id, p.name, p.faculty_id
		 ORDER BY p.name`,
		department,
	)
	defer rows.Close()

	var faculty []gin.H
	for rows.Next() {
		var name, facID string
		var sessionCount int
		var avgFacRate float64
		rows.Scan(&name, &facID, &sessionCount, &avgFacRate)
		faculty = append(faculty, gin.H{
			"name": name, "faculty_id": facID,
			"session_count": sessionCount, "avg_attendance_rate": avgFacRate,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"department":          department,
		"faculty_count":       facultyCount,
		"student_count":       studentCount,
		"sessions_today":      sessionsToday,
		"active_now":          activeNow,
		"avg_attendance_rate": avgRate,
		"faculty_breakdown":   faculty,
	})
}

// GET /hod/faculty
// Returns faculty roster for the logged-in HOD's department, shaped for the Faculty screen
func GetHODFaculty(c *gin.Context) {
	hodID, _ := c.Get("user_id")

	var department string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(department, '') FROM professors WHERE id = $1`, hodID,
	).Scan(&department)
	if err != nil || department == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not determine HOD department"})
		return
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT
		    p.id, p.name, p.email,
		    COUNT(DISTINCT s.id) FILTER (WHERE s.start_time::date = CURRENT_DATE) as sessions_today,
		    COALESCE(ROUND(
		      100.0 * COUNT(a.*) FILTER (WHERE a.status = 'present' AND s.start_time >= NOW() - INTERVAL '30 days')
		      / NULLIF(COUNT(a.*) FILTER (WHERE s.start_time >= NOW() - INTERVAL '30 days'), 0)
		    , 1), 0) as avg_attendance,
		    MAX(s.start_time) as last_session
		 FROM professors p
		 LEFT JOIN attendance_sessions s ON s.professor_id = p.id
		 LEFT JOIN attendance a ON a.session_id = s.id
		 WHERE p.department = $1 AND COALESCE(p.role,'professor') IN ('professor','hod')
		 GROUP BY p.id, p.name, p.email
		 ORDER BY p.name`,
		department,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var faculty []gin.H
	for rows.Next() {
		var id, name, email string
		var sessionsToday int
		var avgAttendance float64
		var lastSession *time.Time

		if err := rows.Scan(&id, &name, &email, &sessionsToday, &avgAttendance, &lastSession); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		entry := gin.H{
			"id":             id,
			"name":           name,
			"email":          email,
			"sessions_today": sessionsToday,
			"avg_attendance": avgAttendance,
		}
		if lastSession != nil {
			entry["last_session"] = lastSession.Format("Jan 2, 2006 · 3:04 PM")
		}

		faculty = append(faculty, entry)
	}

	// Return a bare array, matching what the frontend expects
	if faculty == nil {
		faculty = []gin.H{}
	}
	c.JSON(http.StatusOK, faculty)
}

// GET /hod/analytics
// Returns department-wide attendance trend analytics for the logged-in HOD
func GetHODAnalytics(c *gin.Context) {
	hodID, _ := c.Get("user_id")

	var department string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(department, '') FROM professors WHERE id = $1`, hodID,
	).Scan(&department)
	if err != nil || department == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not determine HOD department"})
		return
	}

	rateQuery := func(interval string) float64 {
		var rate float64
		db.Pool.QueryRow(context.Background(),
			fmt.Sprintf(`
				SELECT COALESCE(ROUND(
				  100.0 * COUNT(a.*) FILTER (WHERE a.status = 'present')
				  / NULLIF(COUNT(a.*), 0)
				, 1), 0)
				FROM attendance a
				JOIN attendance_sessions s ON s.id = a.session_id
				JOIN professors p ON p.id = s.professor_id
				WHERE p.department = $1 AND s.start_time >= NOW() - INTERVAL '%s'`, interval),
			department,
		).Scan(&rate)
		return rate
	}

	dailyAvg := rateQuery("1 day")
	weeklyAvg := rateQuery("7 days")
	monthlyAvg := rateQuery("30 days")

	// Trend: compare this week's rate to the week before it
	var prevWeekRate float64
	db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(ROUND(
		    100.0 * COUNT(a.*) FILTER (WHERE a.status = 'present')
		    / NULLIF(COUNT(a.*), 0)
		  , 1), 0)
		 FROM attendance a
		 JOIN attendance_sessions s ON s.id = a.session_id
		 JOIN professors p ON p.id = s.professor_id
		 WHERE p.department = $1
		   AND s.start_time >= NOW() - INTERVAL '14 days'
		   AND s.start_time < NOW() - INTERVAL '7 days'`,
		department,
	).Scan(&prevWeekRate)

	trend := "stable"
	if weeklyAvg > prevWeekRate+1 {
		trend = "up"
	} else if weeklyAvg < prevWeekRate-1 {
		trend = "down"
	}

	// Per-subject rates over last 30 days, department-wide
	rows, err := db.Pool.Query(context.Background(),
		`SELECT
		    COALESCE(sub.subject_name, s.subject) as subject_label,
		    ROUND(
		      100.0 * COUNT(a.*) FILTER (WHERE a.status = 'present')
		      / NULLIF(COUNT(a.*), 0)
		    , 1) as rate
		 FROM attendance_sessions s
		 JOIN professors p ON p.id = s.professor_id
		 LEFT JOIN attendance a ON a.session_id = s.id
		 LEFT JOIN subjects sub ON sub.subject_code = s.subject
		 WHERE p.department = $1 AND s.start_time >= NOW() - INTERVAL '30 days'
		 GROUP BY COALESCE(sub.subject_name, s.subject)
		 HAVING COUNT(a.*) > 0
		 ORDER BY rate DESC`,
		department,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type subjectRate struct {
		Subject string
		Rate    float64
	}
	var allSubjects []subjectRate
	for rows.Next() {
		var sr subjectRate
		rows.Scan(&sr.Subject, &sr.Rate)
		allSubjects = append(allSubjects, sr)
	}

	// Top performers: rate >= 75, best first, cap at 5
	// Needs attention: rate < 60, worst first, cap at 5
	var topPerformers, needsAttention []gin.H
	for _, sr := range allSubjects {
		if sr.Rate >= 75 && len(topPerformers) < 5 {
			topPerformers = append(topPerformers, gin.H{"subject": sr.Subject, "rate": sr.Rate})
		}
	}
	for i := len(allSubjects) - 1; i >= 0; i-- {
		if allSubjects[i].Rate < 60 && len(needsAttention) < 5 {
			needsAttention = append(needsAttention, gin.H{"subject": allSubjects[i].Subject, "rate": allSubjects[i].Rate})
		}
	}

	if topPerformers == nil {
		topPerformers = []gin.H{}
	}
	if needsAttention == nil {
		needsAttention = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"daily_average":   dailyAvg,
		"weekly_average":  weeklyAvg,
		"monthly_average": monthlyAvg,
		"trend":           trend,
		"top_performers":  topPerformers,
		"needs_attention": needsAttention,
	})
}
