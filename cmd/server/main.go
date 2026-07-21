package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/yourusername/presentsz-server/internal/db"
	"github.com/yourusername/presentsz-server/internal/routes"
)

func runMigrations() error {
	_, err := db.Pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		return fmt.Errorf("failed to read migrations folder: %w", err)
	}
	sort.Strings(files)

	applied := 0
	for _, file := range files {
		filename := filepath.Base(file)

		var exists bool
		db.Pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM migrations WHERE filename = $1)`, filename,
		).Scan(&exists)

		if exists {
			fmt.Printf("  ↩ skipping %s (already applied)\n", filename)
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filename, err)
		}

		_, err = db.Pool.Exec(context.Background(), string(content))
		if err != nil {
			return fmt.Errorf("failed to apply %s: %w", filename, err)
		}

		db.Pool.Exec(context.Background(),
			`INSERT INTO migrations (filename) VALUES ($1)`, filename)

		fmt.Printf("  ✓ applied %s\n", filename)
		applied++
	}

	if applied == 0 {
		fmt.Println("✓ Migrations — all up to date")
	} else {
		fmt.Printf("✓ Migrations — %d file(s) applied\n", applied)
	}
	return nil
}

func runSessionScheduler() {
	now := time.Now()
	currentTime := now.Format("15:04:05")
	windowStart := now.Add(-2 * time.Minute).Format("15:04:05")
	today := now.Weekday().String() // "Monday", "Tuesday", ...

	rows, err := db.Pool.Query(context.Background(),
		`SELECT t.id, t.subject_code, t.faculty_code, t.room_code,
		        t.start_time::text, t.end_time::text, t.section, t.semester
		 FROM timetable t
		 WHERE t.day = $1
		   AND t.start_time <= $2
		   AND t.start_time >= $3
		   AND t.status = 'Active'`,
		today, currentTime, windowStart,
	)
	if err != nil {
		log.Println("Scheduler query error:", err)
		return
	}

	type entry struct {
		id, subjectCode, facultyCode, roomCode string
		startTime, endTime, section, semester  string
	}
	var toProcess []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.subjectCode, &e.facultyCode, &e.roomCode,
			&e.startTime, &e.endTime, &e.section, &e.semester); err != nil {
			log.Println("Scheduler scan error:", err)
			continue
		}
		toProcess = append(toProcess, e)
	}
	rows.Close()

	for _, e := range toProcess {
		// Resolve room_code -> classrooms.id
		var roomID string
		err := db.Pool.QueryRow(context.Background(),
			`SELECT id FROM classrooms WHERE room_code = $1`, e.roomCode,
		).Scan(&roomID)
		if err != nil {
			log.Printf("Scheduler: room not found for code %s, skipping\n", e.roomCode)
			continue
		}

		// Resolve faculty_code -> professors.id
		var professorID string
		err = db.Pool.QueryRow(context.Background(),
			`SELECT id FROM professors WHERE faculty_id = $1`, e.facultyCode,
		).Scan(&professorID)
		if err != nil {
			log.Printf("Scheduler: professor not found for faculty_code %s, skipping\n", e.facultyCode)
			continue
		}

		// Skip if a session already exists for this room+subject+section+semester today
		var exists bool
		db.Pool.QueryRow(context.Background(),
			`SELECT EXISTS(
				SELECT 1 FROM attendance_sessions
				WHERE room_id = $1 AND subject = $2 AND section = $3 AND semester = $4
				AND DATE(start_time) = CURRENT_DATE
			)`, roomID, e.subjectCode, e.section, e.semester,
		).Scan(&exists)
		if exists {
			continue
		}

		// Compute today's end_time as a real timestamp
		var endTime time.Time
		endParsed, err := time.Parse("15:04:05", e.endTime)
		if err == nil {
			endTime = time.Date(now.Year(), now.Month(), now.Day(),
				endParsed.Hour(), endParsed.Minute(), 0, 0, now.Location())
		} else {
			endTime = now.Add(1 * time.Hour)
		}

		// Deactivate any existing active session in this room
		db.Pool.Exec(context.Background(),
			`UPDATE attendance_sessions SET active = false, end_time = NOW()
			 WHERE room_id = $1 AND active = true`, roomID)

		// Create session
		var sessionID string
		err = db.Pool.QueryRow(context.Background(),
			`INSERT INTO attendance_sessions
			    (room_id, professor_id, subject, section, semester, active, end_time)
			 VALUES ($1, $2, $3, $4, $5, true, $6)
			 RETURNING id`,
			roomID, professorID, e.subjectCode, e.section, e.semester, endTime,
		).Scan(&sessionID)
		if err != nil {
			log.Println("Scheduler: failed to create session:", err)
			continue
		}

		log.Printf("✓ Auto-created session: %s | %s | section %s sem %s | ends %s\n",
			e.subjectCode, e.startTime, e.section, e.semester, endTime.Format("15:04"))
	}

	// Auto-end expired sessions — unchanged, schema-agnostic
	result, _ := db.Pool.Exec(context.Background(),
		`UPDATE attendance_sessions
		 SET active = false
		 WHERE active = true AND end_time < NOW()`)
	if n := result.RowsAffected(); n > 0 {
		log.Printf("✓ Auto-ended %d expired session(s)\n", n)
	}
}
func startScheduler() {
	// Run immediately on startup
	runSessionScheduler()

	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			runSessionScheduler()
		}
	}()
	fmt.Println("✓ Session scheduler started")
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	if err := db.Connect(); err != nil {
		log.Fatalf("DB connection failed: %v", err)
	}
	defer db.Pool.Close()

	if err := runMigrations(); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// Start background scheduler
	startScheduler()

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	routes.Setup(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 Server running on :%s\n", port)
	log.Fatal(r.Run(":" + port))
}
