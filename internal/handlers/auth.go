package handlers

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/yourusername/presentsz-server/internal/db"
	"github.com/yourusername/presentsz-server/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

// POST /auth/register
func RegisterStudent(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		Email      string `json:"email" binding:"required,email"`
		Phone      string `json:"phone" binding:"required"`
		RollNumber string `json:"roll_number" binding:"required"`
		Department string `json:"department" binding:"required"`
		Year       string `json:"year" binding:"required"`
		Semester   string `json:"semester" binding:"required"`
		Password   string `json:"password" binding:"required,min=8"`
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

	var studentID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO students (name, email, phone, roll_number, department, year, semester, password_hash)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.Name, req.Email, req.Phone, req.RollNumber,
		req.Department, req.Year, req.Semester, string(hash),
	).Scan(&studentID)

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email or roll number already exists"})
		return
	}

	token, err := generateToken(studentID, "student")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":      token,
		"student_id": studentID,
		"message":    "registered successfully",
	})
}

// POST /auth/login
func LoginStudent(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var studentID, passwordHash string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, password_hash FROM students WHERE email = $1`, req.Email,
	).Scan(&studentID, &passwordHash)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := generateToken(studentID, "student")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "student_id": studentID})
}

// POST /auth/professor/login
func LoginProfessor(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var profID, passwordHash string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id, password_hash FROM professors WHERE email = $1`, req.Email,
	).Scan(&profID, &passwordHash)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := generateToken(profID, "professor")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "professor_id": profID})
}

func generateToken(userID, role string) (string, error) {
	claims := middleware.Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(os.Getenv("JWT_SECRET")))
}

// POST /students/:id/register-ble
func RegisterBLE(c *gin.Context) {
	studentID := c.Param("id")
	var req struct {
		BLEUUID  string `json:"ble_uuid" binding:"required"`
		DeviceID string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Pool.Exec(context.Background(),
		`UPDATE students SET ble_uuid = $1, device_id = $2 WHERE id = $3`,
		req.BLEUUID, req.DeviceID, studentID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register BLE"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "BLE UUID registered"})
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
