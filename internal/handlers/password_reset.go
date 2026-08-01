package handlers

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/yourusername/presentsz-server/internal/db"
	"github.com/yourusername/presentsz-server/internal/email"
	"github.com/yourusername/presentsz-server/internal/middleware"
)

var otpAttemptLimiter = middleware.NewRateLimiter(5, 10*time.Minute) // 5 guesses / 10 min / USN
func generateOTP() string {
	digits := "0123456789"
	b := make([]byte, 6)
	buf := make([]byte, 6)
	rand.Read(buf)
	for i, v := range buf {
		b[i] = digits[int(v)%10]
	}
	return string(b)
}

// POST /students/forgot-password
func ForgotPassword(c *gin.Context) {
	var req struct {
		USN string `json:"usn" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	usn := strings.ToUpper(strings.TrimSpace(req.USN))

	// Generic response either way — never reveal whether a USN exists,
	// or whether that student has a real vs. placeholder email.
	generic := gin.H{"message": "If that account exists and has an email on file, a reset code has been sent."}

	var studentID, name, studentEmail string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email FROM students WHERE roll_number = $1`, usn,
	).Scan(&studentID, &name, &studentEmail)
	if err != nil {
		c.JSON(http.StatusOK, generic) // don't leak that the USN doesn't exist
		return
	}

	// Placeholder emails were auto-generated for students with no real
	// email on file at bulk-upload time — nothing can be delivered there.
	if strings.HasSuffix(strings.ToLower(studentEmail), "@presenze.local") {
		c.JSON(http.StatusOK, generic)
		return
	}

	otp := generateOTP()
	hash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate code"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO password_reset_otps (student_id, otp_hash, expires_at)
		 VALUES ($1, $2, NOW() + INTERVAL '10 minutes')`,
		studentID, string(hash),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create reset request"})
		return
	}

	if err := email.SendOTP(studentEmail, name, otp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send email"})
		return
	}

	c.JSON(http.StatusOK, generic)
}

// POST /students/reset-with-otp
func ResetWithOTP(c *gin.Context) {
	var req struct {
		USN         string `json:"usn" binding:"required"`
		OTP         string `json:"otp" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	usn := strings.ToUpper(strings.TrimSpace(req.USN))

	if !otpAttemptLimiter.Allow("otp:" + usn) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many attempts for this account. Please request a new code and try again later.",
		})
		return
	}
	var studentID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM students WHERE roll_number = $1`, usn,
	).Scan(&studentID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid code"})
		return
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, otp_hash FROM password_reset_otps
		 WHERE student_id = $1 AND used = false AND expires_at > NOW()
		 ORDER BY created_at DESC`,
		studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var matchedID string
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			log.Println("ResetWithOTP scan error:", err)
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OTP)) == nil {
			matchedID = id
			break
		}
	}
	rows.Close()
	rows.Close()

	if matchedID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired code"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`UPDATE students
		 SET password_hash = $1, password_reset_required = false, password_expires_at = NULL
		 WHERE id = $2`,
		string(newHash), studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	db.Pool.Exec(context.Background(),
		`UPDATE password_reset_otps SET used = true WHERE id = $1`, matchedID)

	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}
