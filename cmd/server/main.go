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
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

	// Auto-create sessions for timetable entries starting now
	rows, err := db.Pool.Query(context.Background(),
		`SELECT t.id, t.subject, t.room_id, t.time_slot,
			LEAD(t.time_slot) OVER (
				PARTITION BY t.room_id, t.class_date
				ORDER BY t.time_slot
			) as next_slot
		 FROM timetable t
		 WHERE t.class_date = $1 AND t.time_slot = $2`,
		today, currentTime,
	)
	if err != nil {
		log.Println("Scheduler query error:", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var entryID, subject, roomID, timeSlot string
		var nextSlot *string
		rows.Scan(&entryID, &subject, &roomID, &timeSlot, &nextSlot)

		// Calculate end time
		var endTime time.Time
		if nextSlot != nil {
			parsed, err := time.ParseInLocation("15:04", *nextSlot, now.Location())
			if err == nil {
				endTime = time.Date(now.Year(), now.Month(), now.Day(),
					parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
			}
		} else {
			endTime = now.Add(1 * time.Hour)
		}

		// Skip if session already exists for this slot today
		var exists bool
		db.Pool.QueryRow(context.Background(),
			`SELECT EXISTS(
				SELECT 1 FROM attendance_sessions
				WHERE room_id = $1 AND subject = $2
				AND DATE(start_time) = CURRENT_DATE
			)`, roomID, subject,
		).Scan(&exists)
		if exists {
			continue
		}

		// Find a professor for this subject
		var professorID string
		err := db.Pool.QueryRow(context.Background(),
			`SELECT id FROM professors WHERE subject = $1 LIMIT 1`, subject,
		).Scan(&professorID)
		if err != nil {
			log.Printf("No professor found for subject %s, skipping\n", subject)
			continue
		}

		// Deactivate any existing active session in this room
		db.Pool.Exec(context.Background(),
			`UPDATE attendance_sessions SET active = false, end_time = NOW()
			 WHERE room_id = $1 AND active = true`, roomID)

		// Create session
		var sessionID string
		err = db.Pool.QueryRow(context.Background(),
			`INSERT INTO attendance_sessions (room_id, professor_id, subject, active, end_time)
			 VALUES ($1, $2, $3, true, $4) RETURNING id`,
			roomID, professorID, subject, endTime,
		).Scan(&sessionID)
		if err != nil {
			log.Println("Failed to create session:", err)
			continue
		}

		log.Printf("✓ Auto-created session: %s | %s | ends %s\n",
			subject, timeSlot, endTime.Format("15:04"))
	}

	// Auto-end expired sessions
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
