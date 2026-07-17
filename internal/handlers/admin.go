package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/yourusername/presentsz-server/internal/db"
)

// ============================================
// BULK UPLOAD STUDENTS FROM EXCEL
// ============================================
// Excel format expected (row 1 = headers):
// Name | USN | Email | Section Code | Semester | Phone (optional)
func BulkUploadStudents(c *gin.Context) {
	uploaderID, _ := c.Get("user_id")

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	// Check file type
	filename := strings.ToLower(file.Filename)
	if !strings.HasSuffix(filename, ".xlsx") && !strings.HasSuffix(filename, ".xls") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .xlsx and .xls files are supported"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})
		return
	}
	defer f.Close()

	xlsx, err := excelize.OpenReader(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to parse Excel file"})
		return
	}

	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read rows"})
		return
	}

	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows found"})
		return
	}

	type Result struct {
		Row    int    `json:"row"`
		Name   string `json:"name"`
		USN    string `json:"usn"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var results []Result
	successCount := 0
	errorCount := 0
	skippedCount := 0

	// Cache section lookups
	sectionCache := make(map[string]string) // section_code -> section_id

	// Get section info helper
	getSectionInfo := func(sectionCode string) (id, year, department string, err error) {
		sectionCode = strings.ToUpper(strings.TrimSpace(sectionCode))
		if cached, ok := sectionCache[sectionCode]; ok {
			// Fetch again for year/dept (optional optimization)
			err = db.Pool.QueryRow(context.Background(),
				`SELECT id, year, department FROM sections WHERE section_code = $1`, sectionCode,
			).Scan(&id, &year, &department)
			_ = cached
			return
		}
		err = db.Pool.QueryRow(context.Background(),
			`SELECT id, year, department FROM sections WHERE section_code = $1`, sectionCode,
		).Scan(&id, &year, &department)
		if err == nil {
			sectionCache[sectionCode] = id
		}
		return
	}

	// Process each row (skip header)
	for i, row := range rows[1:] {
		rowNum := i + 2
		result := Result{Row: rowNum}

		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}

		getField := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		name := getField(0)
		usn := strings.ToUpper(getField(1))
		email := strings.ToLower(getField(2))
		sectionCode := strings.ToUpper(getField(3))
		semester := getField(4)
		phone := getField(5)

		result.Name = name
		result.USN = usn

		// Validate required
		if name == "" || usn == "" || sectionCode == "" {
			result.Status = "error"
			result.Error = "missing required fields (name, USN, section)"
			results = append(results, result)
			errorCount++
			continue
		}

		// Auto-generate email if missing
		if email == "" {
			email = fmt.Sprintf("%s@presenze.local", strings.ToLower(usn))
		}

		// Default semester
		if semester == "" {
			semester = "1"
		}

		// Look up section
		sectionID, year, department, sectionErr := getSectionInfo(sectionCode)
		if sectionErr != nil {
			result.Status = "error"
			result.Error = fmt.Sprintf("section '%s' not found", sectionCode)
			results = append(results, result)
			errorCount++
			continue
		}

		// Password = USN (uppercase)
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(usn), bcrypt.DefaultCost)
		if err != nil {
			result.Status = "error"
			result.Error = "failed to hash password"
			results = append(results, result)
			errorCount++
			continue
		}

		// Check if exists
		var existingID string
		err = db.Pool.QueryRow(context.Background(),
			`SELECT id FROM students WHERE roll_number = $1 OR email = $2`, usn, email,
		).Scan(&existingID)

		if err == nil {
			result.Status = "skipped"
			result.Error = "student already exists"
			results = append(results, result)
			skippedCount++
			continue
		}

		// Insert
		_, err = db.Pool.Exec(context.Background(),
			`INSERT INTO students 
			(name, email, phone, password_hash, roll_number, year, semester, department, 
			 section_id, password_reset_required, password_expires_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true, NOW() + INTERVAL '7 days', $10)`,
			name, email, phone, string(hashedPassword), usn, year, semester, department,
			sectionID, uploaderID,
		)

		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			results = append(results, result)
			errorCount++
			continue
		}

		result.Status = "created"
		results = append(results, result)
		successCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "bulk upload complete",
		"total":   len(rows) - 1,
		"success": successCount,
		"errors":  errorCount,
		"skipped": skippedCount,
		"results": results,
	})
}

// ============================================
// DOWNLOAD EXCEL TEMPLATE
// ============================================
func DownloadStudentTemplate(c *gin.Context) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Sheet1"

	// Headers
	headers := []string{"Name", "USN", "Email", "Section Code", "Semester", "Phone"}
	for i, h := range headers {
		col, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, col, h)
	}

	// Bold header
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E0E0E0"}, Pattern: 1},
	})
	f.SetCellStyle(sheet, "A1", "F1", style)

	// Sample rows
	samples := [][]interface{}{
		{"Samarth Ravi", "19BTDJEO41", "samarth@example.com", "CSE-1A", "1", "9876543210"},
		{"Priya Sharma", "19BTDJEO42", "priya@example.com", "CSE-1A", "1", "9876543211"},
		{"Rahul Kumar", "19BTDJEO43", "", "CSE-1A", "1", ""},
	}
	for rowIdx, sample := range samples {
		for colIdx, val := range sample {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			f.SetCellValue(sheet, cell, val)
		}
	}

	// Column widths
	widths := map[string]float64{
		"A": 20, "B": 18, "C": 25, "D": 15, "E": 10, "F": 15,
	}
	for col, w := range widths {
		f.SetColWidth(sheet, col, col, w)
	}

	// Add instructions in separate sheet
	f.NewSheet("Instructions")
	instructions := [][]string{
		{"Field", "Required", "Notes"},
		{"Name", "Yes", "Full name of student"},
		{"USN", "Yes", "Unique student number, will also be initial password"},
		{"Email", "No", "Auto-generated if empty"},
		{"Section Code", "Yes", "Must match existing section (e.g. CSE-1A)"},
		{"Semester", "No", "Defaults to '1' if empty"},
		{"Phone", "No", "Optional contact number"},
	}
	for rowIdx, row := range instructions {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+1)
			f.SetCellValue("Instructions", cell, val)
		}
	}
	f.SetColWidth("Instructions", "A", "A", 20)
	f.SetColWidth("Instructions", "B", "B", 12)
	f.SetColWidth("Instructions", "C", "C", 50)

	// Response headers
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=student_upload_template.xlsx")

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate template"})
		return
	}
}

// ============================================
// LIST ALL STUDENTS (paginated)
// ============================================
func ListStudents(c *gin.Context) {
	// Optional filters
	sectionCode := c.Query("section")
	department := c.Query("department")
	year := c.Query("year")

	query := `
		SELECT s.id, s.name, s.email, s.roll_number, s.year, s.department, 
		       s.semester, s.section_id, sec.section_code,
		       s.password_reset_required, s.created_at
		FROM students s
		LEFT JOIN sections sec ON sec.id = s.section_id
		WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if sectionCode != "" {
		query += fmt.Sprintf(" AND sec.section_code = $%d", argIdx)
		args = append(args, strings.ToUpper(sectionCode))
		argIdx++
	}
	if department != "" {
		query += fmt.Sprintf(" AND s.department = $%d", argIdx)
		args = append(args, strings.ToUpper(department))
		argIdx++
	}
	if year != "" {
		query += fmt.Sprintf(" AND s.year = $%d", argIdx)
		args = append(args, year)
		argIdx++
	}

	query += " ORDER BY s.roll_number LIMIT 500"

	rows, err := db.Pool.Query(context.Background(), query, args...)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Student struct {
		ID                    string  `json:"id"`
		Name                  string  `json:"name"`
		Email                 string  `json:"email"`
		RollNumber            string  `json:"roll_number"`
		Year                  string  `json:"year"`
		Department            string  `json:"department"`
		Semester              string  `json:"semester"`
		SectionID             *string `json:"section_id"`
		SectionCode           *string `json:"section_code"`
		PasswordResetRequired bool    `json:"password_reset_required"`
		CreatedAt             string  `json:"created_at"`
	}

	var students []Student

	for rows.Next() {
		var s Student
		var createdAt time.Time
		err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.RollNumber, &s.Year, &s.Department,
			&s.Semester, &s.SectionID, &s.SectionCode,
			&s.PasswordResetRequired, &createdAt)
		if err != nil {
			fmt.Println("SCAN ERROR:", err)
			continue
		}
		s.CreatedAt = createdAt.Format(time.RFC3339)
		students = append(students, s)
	}

	c.JSON(http.StatusOK, gin.H{
		"students": students,
		"count":    len(students),
	})
}

// ============================================
// RESET STUDENT PASSWORD (Admin)
// ============================================
func ResetStudentPassword(c *gin.Context) {
	studentID := c.Param("id")

	// Get USN
	var usn string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT roll_number FROM students WHERE id = $1`, studentID,
	).Scan(&usn)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "student not found"})
		return
	}

	// Reset password to USN
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(usn), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`UPDATE students 
		 SET password_hash = $1,
		     password_reset_required = true,
		     password_expires_at = NOW() + INTERVAL '7 days'
		 WHERE id = $2`,
		string(hashedPassword), studentID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "password reset successfully",
		"new_password": usn,
		"expires_in":   "7 days",
	})
}

// ============================================
// CREATE PROFESSOR (Admin)
// ============================================
func CreateProfessor(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		FacultyID  string `json:"faculty_id" binding:"required"`
		Email      string `json:"email"` // Optional
		Password   string `json:"password" binding:"required,min=6"`
		Subject    string `json:"subject"`
		Department string `json:"department" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	adminID, _ := c.Get("user_id")

	// Normalize
	facultyID := strings.ToUpper(strings.TrimSpace(req.FacultyID))
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Auto-generate email if not provided
	if email == "" {
		email = fmt.Sprintf("%s@presenze.local", strings.ToLower(facultyID))
	}

	// Check if faculty_id or email already exists
	var existingID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM professors WHERE faculty_id = $1 OR email = $2`,
		facultyID, email,
	).Scan(&existingID)

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "faculty ID or email already exists"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	var profID string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO professors 
		 (name, email, faculty_id, subject, password_hash, role, department, created_by)
		 VALUES ($1, $2, $3, $4, $5, 'professor', $6, $7)
		 RETURNING id`,
		req.Name, email, facultyID, req.Subject, string(hashedPassword),
		strings.ToUpper(req.Department), adminID,
	).Scan(&profID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":      "professor created",
		"professor_id": profID,
		"faculty_id":   facultyID,
		"login_info":   fmt.Sprintf("Faculty can login with Faculty ID: %s", facultyID),
	})
}

// ============================================
// LIST ALL PROFESSORS (Admin)
// ============================================
func ListProfessors(c *gin.Context) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, name, email, subject, COALESCE(role, 'professor'), 
		        COALESCE(department, ''), created_at, faculty_id
		 FROM professors ORDER BY created_at DESC`)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Professor struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Email      string  `json:"email"`
		Subject    string  `json:"subject"`
		Role       string  `json:"role"`
		Department string  `json:"department"`
		CreatedAt  string  `json:"created_at"`
		FacultyID  *string `json:"faculty_id"`
	}

	var profs []Professor
	for rows.Next() {
		var p Professor
		var createdAt time.Time

		err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Email,
			&p.Subject,
			&p.Role,
			&p.Department,
			&createdAt,
			&p.FacultyID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		p.CreatedAt = createdAt.Format(time.RFC3339)

		profs = append(profs, p)
	}

	c.JSON(http.StatusOK, gin.H{
		"professors": profs,
		"count":      len(profs),
	})
}

// ============================================
// DELETE PROFESSOR (Admin)
// ============================================
func DeleteProfessor(c *gin.Context) {
	profID := c.Param("id")
	adminID, _ := c.Get("user_id")

	// Prevent deleting yourself
	if profID == adminID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete yourself"})
		return
	}

	// Prevent deleting other admins (only super admin can, but we skip for now)
	var role string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(role, 'professor') FROM professors WHERE id = $1`, profID,
	).Scan(&role)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "professor not found"})
		return
	}

	if role == "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete admin"})
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`DELETE FROM professors WHERE id = $1`, profID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "professor deleted"})
}

// Admin can reset a student's BLE registration
func ResetStudentDevice(c *gin.Context) {
	studentID := c.Param("id")

	_, err := db.Pool.Exec(context.Background(),
		`UPDATE students SET ble_uuid = NULL, device_id = NULL WHERE id = $1`,
		studentID,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "device reset, student can register new device on next login",
	})
}
