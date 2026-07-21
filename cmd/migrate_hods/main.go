package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	rows, err := pool.Query(ctx,
		`SELECT hod_code, name, department, email FROM hods`)
	if err != nil {
		log.Fatalf("failed to query hods: %v", err)
	}
	defer rows.Close()

	type hodRow struct {
		code, name, dept, email string
	}
	var hods []hodRow
	for rows.Next() {
		var h hodRow
		var email *string
		if err := rows.Scan(&h.code, &h.name, &h.dept, &email); err != nil {
			log.Fatalf("scan error: %v", err)
		}
		if email != nil {
			h.email = *email
		}
		hods = append(hods, h)
	}

	fmt.Printf("Found %d HODs to promote\n\n", len(hods))

	promoted, skipped := 0, 0

	for _, h := range hods {
		facultyID := strings.ToUpper(strings.TrimSpace(h.code))

		var existingID string
		err := pool.QueryRow(ctx,
			`SELECT id FROM professors WHERE faculty_id = $1`, facultyID,
		).Scan(&existingID)
		if err == nil {
			fmt.Printf("SKIP  %-12s already exists as professor (id=%s)\n", facultyID, existingID)
			skipped++
			continue
		}

		email := h.email
		if email == "" {
			email = strings.ToLower(facultyID) + "@presenze.local"
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(facultyID), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("failed to hash password for %s: %v", facultyID, err)
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO professors
     (name, email, faculty_id, department, role, subject, password_hash,
      password_reset_required, password_expires_at, status)
     VALUES ($1, $2, $3, $4, 'hod', '', $5, true, $6, 'Active')`,
			h.name, email, facultyID, h.dept, string(hash),
			time.Now().Add(7*24*time.Hour),
		)
		if err != nil {
			fmt.Printf("ERROR %-12s failed to insert: %v\n", facultyID, err)
			continue
		}

		fmt.Printf("OK    %-12s promoted (login: %s / %s)\n", facultyID, facultyID, facultyID)
		promoted++
	}

	fmt.Printf("\nDone. Promoted: %d, Skipped: %d\n", promoted, skipped)
}
