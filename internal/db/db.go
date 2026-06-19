package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	Pool = pool
	fmt.Println("✓ Database connected")
	return nil
}
