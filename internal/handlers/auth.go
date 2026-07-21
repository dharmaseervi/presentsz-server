package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourusername/presentsz-server/internal/db"
	"github.com/yourusername/presentsz-server/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

// POST /students/:id/register-ble
// This endpoint is called ONCE from the mobile app on first login
func RegisterBLE(c *gin.Context) {
	studentID, _ := c.Get("user_id")

	var req struct {
		BLEUUID  string `json:"ble_uuid" binding:"required"`
		DeviceID string `json:"device_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if student already has BLE registered
	var existingUUID *string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT ble_uuid FROM students WHERE id = $1`, studentID,
	).Scan(&existingUUID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	// If already registered, don't allow re-registration (security)
	if existingUUID != nil && *existingUUID != "" {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "BLE device already registered for this student",
			"message": "Contact admin to reset device",
		})
		return
	}

	// Update
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE students 
         SET ble_uuid = $1, device_id = $2
         WHERE id = $3`,
		req.BLEUUID, req.DeviceID, studentID,
	)

	if err != nil {
		// If UUID unique constraint fails, someone else has this UUID
		c.JSON(http.StatusConflict, gin.H{"error": "device UUID already in use"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "device registered successfully",
		"ble_uuid":  req.BLEUUID,
		"device_id": req.DeviceID,
	})
}

func RegisterProfessor(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Subject  string `json:"subject" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	var id string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO professors (name, email, subject, password_hash)
         VALUES ($1, $2, $3, $4) RETURNING id`,
		req.Name, req.Email, req.Subject, string(hash),
	).Scan(&id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "email may already exist"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":    id,
		"email": req.Email,
		"name":  req.Name,
	})
}

// ============================================
// STUDENT LOGIN (USN + Password)
// ============================================
func LoginStudent(c *gin.Context) {
	var req struct {
		USN      string `json:"usn" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var (
		id, name, email, rollNumber, year, semester, department string
		passwordHash                                            string
		passwordResetRequired                                   bool
		passwordExpiresAt                                       *time.Time
		sectionID                                               *string
	)

	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email, password_hash, roll_number, 
		        year, semester, department, section_id,
		        password_reset_required, password_expires_at
		 FROM students WHERE roll_number = $1`,
		strings.ToUpper(req.USN),
	).Scan(&id, &name, &email, &passwordHash, &rollNumber,
		&year, &semester, &department, &sectionID,
		&passwordResetRequired, &passwordExpiresAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Check password expiry
	if passwordResetRequired && passwordExpiresAt != nil && time.Now().After(*passwordExpiresAt) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":                   "password_expired",
			"password_reset_required": true,
			"message":                 "Your temporary password has expired. Contact admin to reset.",
		})
		return
	}

	// Get section details
	var sectionCode, sectionLetter *string
	if sectionID != nil {
		db.Pool.QueryRow(context.Background(),
			`SELECT section_code, section_letter FROM sections WHERE id = $1`, *sectionID,
		).Scan(&sectionCode, &sectionLetter)
	}

	// Generate token
	token, err := middleware.GenerateToken(id, "student")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	response := gin.H{
		"token":                   token,
		"user_id":                 id,
		"name":                    name,
		"email":                   email,
		"roll_number":             rollNumber,
		"year":                    year,
		"semester":                semester,
		"department":              department,
		"section_id":              sectionID,
		"section_code":            sectionCode,
		"password_reset_required": passwordResetRequired,
	}

	if passwordResetRequired && passwordExpiresAt != nil {
		daysLeft := int(time.Until(*passwordExpiresAt).Hours() / 24)
		response["password_message"] = fmt.Sprintf("Please change your password within %d days.", daysLeft)
		response["days_until_expiry"] = daysLeft
	}

	c.JSON(http.StatusOK, response)
}

// ============================================
// PROFESSOR LOGIN (Faculty ID only)
// ============================================
func LoginProfessor(c *gin.Context) {
	var req struct {
		FacultyID string `json:"faculty_id" binding:"required"`
		Password  string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var (
		id, name, email, role, passwordHash, facultyID string
		department                                     *string
		passwordResetRequired                          bool
		passwordExpiresAt                              *time.Time
	)

	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email, COALESCE(role,'professor'), password_hash,
		        COALESCE(faculty_id,''), department,
		        COALESCE(password_reset_required, false), password_expires_at
		 FROM professors WHERE faculty_id = $1`,
		strings.ToUpper(strings.TrimSpace(req.FacultyID)),
	).Scan(&id, &name, &email, &role, &passwordHash, &facultyID, &department,
		&passwordResetRequired, &passwordExpiresAt)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if passwordResetRequired && passwordExpiresAt != nil && time.Now().After(*passwordExpiresAt) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":                   "password_expired",
			"password_reset_required": true,
			"message":                 "Your temporary password has expired. Contact admin to reset.",
		})
		return
	}

	token, err := middleware.GenerateToken(id, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	resp := gin.H{
		"token":                   token,
		"user_id":                 id,
		"name":                    name,
		"email":                   email,
		"faculty_id":              facultyID,
		"role":                    role,
		"department":              department,
		"password_reset_required": passwordResetRequired,
	}
	if passwordResetRequired && passwordExpiresAt != nil {
		daysLeft := int(time.Until(*passwordExpiresAt).Hours() / 24)
		resp["password_message"] = fmt.Sprintf("Please change your password within %d days.", daysLeft)
		resp["days_until_expiry"] = daysLeft
	}

	c.JSON(http.StatusOK, resp)
}

func LoginAdmin(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var (
		id, name, email, passwordHash, role string
	)

	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, name, email, password_hash, COALESCE(role, 'professor')
		 FROM professors WHERE email = $1 AND role = 'admin'`,
		strings.ToLower(strings.TrimSpace(req.Email)),
	).Scan(&id, &name, &email, &passwordHash, &role)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := middleware.GenerateToken(id, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":   token,
		"user_id": id,
		"name":    name,
		"email":   email,
		"role":    role,
	})
}

// ============================================
// CHANGE PASSWORD (Student)
// ============================================
func ChangePassword(c *gin.Context) {
	studentID, _ := c.Get("user_id")

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current password
	var currentHash string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT password_hash FROM students WHERE id = $1`, studentID,
	).Scan(&currentHash)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		fmt.Println("PASSWORD MISMATCH for student", studentID) //
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	// Prevent same password
	if req.CurrentPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new password must be different"})
		return
	}

	// Hash new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	// Update
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE students 
		 SET password_hash = $1, 
		     password_reset_required = false,
		     password_expires_at = NULL
		 WHERE id = $2`,
		string(newHash), studentID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}

// POST /professors/change-password
func ChangePasswordProfessor(c *gin.Context) {
	profID, _ := c.Get("user_id")

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var currentHash string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT password_hash FROM professors WHERE id = $1`, profID,
	).Scan(&currentHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "professor not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}
	if req.CurrentPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new password must be different"})
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

	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}
