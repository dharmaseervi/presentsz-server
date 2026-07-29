package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/yourusername/presentsz-server/internal/db"
	"github.com/yourusername/presentsz-server/internal/email"
	"github.com/yourusername/presentsz-server/internal/middleware"
)

var forgotPasswordProfLimiter = middleware.NewRateLimiter(3, 15*time.Minute)   // by IP
var forgotPasswordProfIdLimiter = middleware.NewRateLimiter(3, 30*time.Minute) // by faculty_id, silent
var otpAttemptProfLimiter = middleware.NewRateLimiter(5, 10*time.Minute)       // by faculty_id

// POST /professor/forgot-password
func ForgotPasswordProfessor(c *gin.Context) {
	var req struct {
		FacultyID string `json:"faculty_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	facultyID := strings.ToUpper(strings.TrimSpace(req.FacultyID))

	generic := gin.H{"message": "If that account exists and has an email on file, a reset code has been sent."}

	if !forgotPasswordProfIdLimiter.Allow("forgot-prof:" + facultyID) {
		c.JSON(http.StatusOK, generic) // silent — don't reveal rate limiting exists
		return
	}

	var profID, name, profEmail string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email FROM professors WHERE faculty_id = $1`, facultyID,
	).Scan(&profID, &name, &profEmail)
	if err != nil {
		c.JSON(http.StatusOK, generic) // don't leak that the faculty ID doesn't exist
		return
	}

	// Placeholder emails were auto-generated for professors/HODs with no
	// real email on file at bulk-upload/promotion time.
	if strings.HasSuffix(strings.ToLower(profEmail), "@presenze.local") {
		c.JSON(http.StatusOK, generic)
		return
	}

	otp := generateOTP() // reused from password_reset.go — same helper, no need to redefine
	hash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate code"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO professor_password_reset_otps (professor_id, otp_hash, expires_at)
		 VALUES ($1, $2, NOW() + INTERVAL '10 minutes')`,
		profID, string(hash),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create reset request"})
		return
	}

	if err := email.SendOTP(profEmail, name, otp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send email"})
		return
	}

	c.JSON(http.StatusOK, generic)
}

// POST /professor/reset-with-otp
func ResetProfessorWithOTP(c *gin.Context) {
	var req struct {
		FacultyID   string `json:"faculty_id" binding:"required"`
		OTP         string `json:"otp" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	facultyID := strings.ToUpper(strings.TrimSpace(req.FacultyID))

	if !otpAttemptProfLimiter.Allow("otp-prof:" + facultyID) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many attempts for this account. Please request a new code and try again later.",
		})
		return
	}

	var profID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM professors WHERE faculty_id = $1`, facultyID,
	).Scan(&profID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid code"})
		return
	}

	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, otp_hash FROM professor_password_reset_otps
		 WHERE professor_id = $1 AND used = false AND expires_at > NOW()
		 ORDER BY created_at DESC`,
		profID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var matchedID string
	for rows.Next() {
		var id, hash string
		rows.Scan(&id, &hash)
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OTP)) == nil {
			matchedID = id
			break
		}
	}
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
		`UPDATE professors
		 SET password_hash = $1, password_reset_required = false, password_expires_at = NULL
		 WHERE id = $2`,
		string(newHash), profID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	db.Pool.Exec(context.Background(),
		`UPDATE professor_password_reset_otps SET used = true WHERE id = $1`, matchedID)

	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}
