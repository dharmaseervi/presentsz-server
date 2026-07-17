package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"github.com/yourusername/presentsz-server/internal/db"
)

// Helper to read Excel and return rows
func parseExcel(c *gin.Context) ([][]string, error) {
	file, err := c.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("file is required")
	}
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	xlsx, err := excelize.OpenReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Excel")
	}

	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func cellAt(row []string, idx int) string {
	if idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

// ============================================
// HODs
// ============================================
// Columns: HOD Code | Name | Department | Email | Phone | Status
func BulkUploadHODs(c *gin.Context) {
	rows, err := parseExcel(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows"})
		return
	}

	created, skipped, errors := 0, 0, 0
	var results []gin.H

	for i, row := range rows[1:] {
		if len(row) == 0 || cellAt(row, 0) == "" {
			continue
		}
		hodCode := strings.ToUpper(cellAt(row, 0))
		name := cellAt(row, 1)
		dept := cellAt(row, 2)
		email := cellAt(row, 3)
		phone := cellAt(row, 4)
		status := cellAt(row, 5)
		if status == "" {
			status = "Active"
		}

		if hodCode == "" || name == "" || dept == "" {
			results = append(results, gin.H{"row": i + 2, "code": hodCode, "status": "error", "error": "missing required fields"})
			errors++
			continue
		}

		_, err := db.Pool.Exec(context.Background(),
			`INSERT INTO hods (hod_code, name, department, email, phone, status)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (hod_code) DO NOTHING`,
			hodCode, name, dept, nullIfEmpty(email), nullIfEmpty(phone), status,
		)
		if err != nil {
			results = append(results, gin.H{"row": i + 2, "code": hodCode, "status": "error", "error": err.Error()})
			errors++
			continue
		}
		created++
		results = append(results, gin.H{"row": i + 2, "code": hodCode, "name": name, "status": "created"})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "upload complete", "total": len(rows) - 1,
		"success": created, "skipped": skipped, "errors": errors, "results": results,
	})
}

func ListHODs(c *gin.Context) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, hod_code, name, department, COALESCE(email,''), COALESCE(phone,''), status
		 FROM hods ORDER BY department`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var list []gin.H
	for rows.Next() {
		var id, code, name, dept, email, phone, status string
		rows.Scan(&id, &code, &name, &dept, &email, &phone, &status)
		list = append(list, gin.H{
			"id": id, "hod_code": code, "name": name, "department": dept,
			"email": email, "phone": phone, "status": status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"hods": list, "count": len(list)})
}

func CreateHOD(c *gin.Context) {
	var req struct {
		HODCode    string `json:"hod_code" binding:"required"`
		Name       string `json:"name" binding:"required"`
		Department string `json:"department" binding:"required"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO hods (hod_code, name, department, email, phone, status)
		 VALUES ($1,$2,$3,$4,$5,'Active') RETURNING id`,
		strings.ToUpper(req.HODCode), req.Name, req.Department,
		nullIfEmpty(req.Email), nullIfEmpty(req.Phone),
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "HOD code may already exist: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "HOD created", "id": id})
}

func DeleteHOD(c *gin.Context) {
	id := c.Param("id")
	db.Pool.Exec(context.Background(), `DELETE FROM hods WHERE id = $1`, id)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ============================================
// SUBJECTS
// ============================================
// Columns: Subject Code | Subject Name | Department | Programme | Semester | Credits | Type | Status
func BulkUploadSubjects(c *gin.Context) {
	rows, err := parseExcel(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows"})
		return
	}

	created, skipped, errors := 0, 0, 0
	var results []gin.H

	for i, row := range rows[1:] {
		if len(row) == 0 || cellAt(row, 0) == "" {
			continue
		}
		code := strings.ToUpper(cellAt(row, 0))
		name := cellAt(row, 1)
		dept := cellAt(row, 2)
		programme := cellAt(row, 3)
		semester := cellAt(row, 4)
		creditsStr := cellAt(row, 5)
		subType := cellAt(row, 6)
		status := cellAt(row, 7)

		if code == "" || name == "" {
			results = append(results, gin.H{"row": i + 2, "code": code, "status": "error", "error": "missing code or name"})
			errors++
			continue
		}
		credits, _ := strconv.Atoi(creditsStr)
		if subType == "" {
			subType = "Theory"
		}
		if status == "" {
			status = "Active"
		}

		_, err := db.Pool.Exec(context.Background(),
			`INSERT INTO subjects (subject_code, subject_name, department, programme, semester, credits, type, status)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			 ON CONFLICT (subject_code) DO UPDATE SET
			   subject_name = EXCLUDED.subject_name,
			   department = EXCLUDED.department,
			   semester = EXCLUDED.semester,
			   credits = EXCLUDED.credits`,
			code, name, dept, programme, semester, credits, subType, status,
		)
		if err != nil {
			results = append(results, gin.H{"row": i + 2, "code": code, "status": "error", "error": err.Error()})
			errors++
			continue
		}
		created++
		results = append(results, gin.H{"row": i + 2, "code": code, "name": name, "status": "created"})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "upload complete", "total": len(rows) - 1,
		"success": created, "skipped": skipped, "errors": errors, "results": results,
	})
}

func ListSubjects(c *gin.Context) {
	dept := c.Query("department")
	semester := c.Query("semester")

	query := `SELECT id, subject_code, subject_name, department, COALESCE(programme,''), 
	          COALESCE(semester,''), credits, type, status FROM subjects WHERE 1=1`
	args := []interface{}{}
	idx := 1
	if dept != "" {
		query += fmt.Sprintf(" AND department = $%d", idx)
		args = append(args, dept)
		idx++
	}
	if semester != "" {
		query += fmt.Sprintf(" AND semester = $%d", idx)
		args = append(args, semester)
		idx++
	}
	query += " ORDER BY semester, subject_code"

	rows, err := db.Pool.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var list []gin.H
	for rows.Next() {
		var id, code, name, dept, prog, sem, typ, status string
		var credits int
		rows.Scan(&id, &code, &name, &dept, &prog, &sem, &credits, &typ, &status)
		list = append(list, gin.H{
			"id": id, "subject_code": code, "subject_name": name, "department": dept,
			"programme": prog, "semester": sem, "credits": credits, "type": typ, "status": status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"subjects": list, "count": len(list)})
}

func CreateSubject(c *gin.Context) {
	var req struct {
		SubjectCode string `json:"subject_code" binding:"required"`
		SubjectName string `json:"subject_name" binding:"required"`
		Department  string `json:"department" binding:"required"`
		Programme   string `json:"programme"`
		Semester    string `json:"semester"`
		Credits     int    `json:"credits"`
		Type        string `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Type == "" {
		req.Type = "Theory"
	}
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO subjects (subject_code, subject_name, department, programme, semester, credits, type, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'Active') RETURNING id`,
		strings.ToUpper(req.SubjectCode), req.SubjectName, req.Department,
		req.Programme, req.Semester, req.Credits, req.Type,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "subject created", "id": id})
}

func DeleteSubject(c *gin.Context) {
	id := c.Param("id")
	db.Pool.Exec(context.Background(), `DELETE FROM subjects WHERE id = $1`, id)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ============================================
// CLASSROOMS
// ============================================
// Columns: Room Code | Room Name | Building | Floor | Capacity | Room Type | Status
func BulkUploadClassrooms(c *gin.Context) {
	rows, err := parseExcel(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows"})
		return
	}

	created, skipped, errors := 0, 0, 0
	var results []gin.H

	for i, row := range rows[1:] {
		if len(row) == 0 || cellAt(row, 0) == "" {
			continue
		}
		roomCode := strings.ToUpper(cellAt(row, 0))
		roomName := cellAt(row, 1)
		building := cellAt(row, 2)
		floor := cellAt(row, 3)
		capStr := cellAt(row, 4)
		roomType := cellAt(row, 5)
		status := cellAt(row, 6)

		if roomCode == "" || roomName == "" {
			results = append(results, gin.H{"row": i + 2, "code": roomCode, "status": "error", "error": "missing code or name"})
			errors++
			continue
		}
		capacity, _ := strconv.Atoi(capStr)
		if capacity == 0 {
			capacity = 60
		}
		if roomType == "" {
			roomType = "Classroom"
		}
		if status == "" {
			status = "Active"
		}

		_, err := db.Pool.Exec(context.Background(),
			`INSERT INTO classrooms (room_code, room_name, building, floor, capacity, room_type, status, year)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,'')
			 ON CONFLICT (room_code) DO UPDATE SET
			   room_name = EXCLUDED.room_name,
			   building = EXCLUDED.building,
			   capacity = EXCLUDED.capacity`,
			roomCode, roomName, building, floor, capacity, roomType, status,
		)
		if err != nil {
			results = append(results, gin.H{"row": i + 2, "code": roomCode, "status": "error", "error": err.Error()})
			errors++
			continue
		}
		created++
		results = append(results, gin.H{"row": i + 2, "code": roomCode, "name": roomName, "status": "created"})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "upload complete", "total": len(rows) - 1,
		"success": created, "skipped": skipped, "errors": errors, "results": results,
	})
}

func ListClassrooms(c *gin.Context) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT id, COALESCE(room_code,''), room_name, COALESCE(building,''), 
		        COALESCE(floor,''), COALESCE(capacity,60), COALESCE(room_type,'Classroom'), 
		        COALESCE(status,'Active')
		 FROM classrooms ORDER BY room_code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var list []gin.H
	for rows.Next() {
		var id, code, name, building, floor, roomType, status string
		var capacity int
		rows.Scan(&id, &code, &name, &building, &floor, &capacity, &roomType, &status)
		list = append(list, gin.H{
			"id": id, "room_code": code, "room_name": name, "building": building,
			"floor": floor, "capacity": capacity, "room_type": roomType, "status": status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"classrooms": list, "count": len(list)})
}

func CreateClassroom(c *gin.Context) {
	var req struct {
		RoomCode string `json:"room_code" binding:"required"`
		RoomName string `json:"room_name" binding:"required"`
		Building string `json:"building"`
		Floor    string `json:"floor"`
		Capacity int    `json:"capacity"`
		RoomType string `json:"room_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Capacity == 0 {
		req.Capacity = 60
	}
	if req.RoomType == "" {
		req.RoomType = "Classroom"
	}
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO classrooms (room_code, room_name, building, floor, capacity, room_type, status, year)
		 VALUES ($1,$2,$3,$4,$5,$6,'Active','') RETURNING id`,
		strings.ToUpper(req.RoomCode), req.RoomName, req.Building, req.Floor, req.Capacity, req.RoomType,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "classroom created", "id": id})
}

func DeleteClassroom(c *gin.Context) {
	id := c.Param("id")
	db.Pool.Exec(context.Background(), `DELETE FROM classrooms WHERE id = $1`, id)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// Helper: returns nil for empty strings (for nullable columns)
func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// ============================================
// SUBJECT ALLOCATIONS
// ============================================
// Columns: Subject Code | Faculty Code | Department | Programme | Semester | Section | Academic Year | Status
func BulkUploadAllocations(c *gin.Context) {
	rows, err := parseExcel(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data rows"})
		return
	}

	created, skipped, errors := 0, 0, 0
	var results []gin.H

	for i, row := range rows[1:] {
		if len(row) == 0 || cellAt(row, 0) == "" {
			continue
		}
		subjectCode := strings.ToUpper(cellAt(row, 0))
		facultyCode := strings.ToUpper(cellAt(row, 1))
		dept := cellAt(row, 2)
		programme := cellAt(row, 3)
		semester := cellAt(row, 4)
		section := strings.ToUpper(cellAt(row, 5))
		academicYear := cellAt(row, 6)
		status := cellAt(row, 7)
		if status == "" {
			status = "Active"
		}
		if section == "" {
			section = "A"
		}

		if subjectCode == "" || facultyCode == "" {
			results = append(results, gin.H{"row": i + 2, "code": subjectCode, "status": "error", "error": "missing subject or faculty code"})
			errors++
			continue
		}

		_, err := db.Pool.Exec(context.Background(),
			`INSERT INTO subject_allocations 
			 (subject_code, faculty_code, department, programme, semester, section, academic_year, status)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			 ON CONFLICT (subject_code, faculty_code, section, semester, academic_year) DO NOTHING`,
			subjectCode, facultyCode, dept, programme, semester, section, academicYear, status,
		)
		if err != nil {
			results = append(results, gin.H{"row": i + 2, "code": subjectCode, "status": "error", "error": err.Error()})
			errors++
			continue
		}
		created++
		results = append(results, gin.H{"row": i + 2, "code": subjectCode, "name": facultyCode, "status": "created"})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "allocations upload complete", "total": len(rows) - 1,
		"success": created, "skipped": skipped, "errors": errors, "results": results,
	})
}

// List with resolved subject + faculty names
func ListAllocations(c *gin.Context) {
	semester := c.Query("semester")
	section := c.Query("section")

	query := `
		SELECT a.id, a.subject_code, COALESCE(s.subject_name, ''),
		       a.faculty_code, COALESCE(p.name, ''),
		       a.department, a.programme, a.semester, a.section,
		       COALESCE(a.academic_year, ''), a.status
		FROM subject_allocations a
		LEFT JOIN subjects s ON s.subject_code = a.subject_code
		LEFT JOIN professors p ON p.faculty_id = a.faculty_code
		WHERE 1=1`
	args := []interface{}{}
	idx := 1
	if semester != "" {
		query += fmt.Sprintf(" AND a.semester = $%d", idx)
		args = append(args, semester)
		idx++
	}
	if section != "" {
		query += fmt.Sprintf(" AND a.section = $%d", idx)
		args = append(args, section)
		idx++
	}
	query += " ORDER BY a.semester, a.subject_code"

	rows, err := db.Pool.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var list []gin.H
	for rows.Next() {
		var id, subCode, subName, facCode, facName, dept, prog, sem, sec, ay, status string
		rows.Scan(&id, &subCode, &subName, &facCode, &facName, &dept, &prog, &sem, &sec, &ay, &status)
		list = append(list, gin.H{
			"id": id, "subject_code": subCode, "subject_name": subName,
			"faculty_code": facCode, "faculty_name": facName,
			"department": dept, "programme": prog, "semester": sem,
			"section": sec, "academic_year": ay, "status": status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"allocations": list, "count": len(list)})
}

func CreateAllocation(c *gin.Context) {
	var req struct {
		SubjectCode  string `json:"subject_code" binding:"required"`
		FacultyCode  string `json:"faculty_code" binding:"required"`
		Department   string `json:"department"`
		Programme    string `json:"programme"`
		Semester     string `json:"semester"`
		Section      string `json:"section"`
		AcademicYear string `json:"academic_year"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Section == "" {
		req.Section = "A"
	}
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO subject_allocations 
		 (subject_code, faculty_code, department, programme, semester, section, academic_year, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'Active') RETURNING id`,
		strings.ToUpper(req.SubjectCode), strings.ToUpper(req.FacultyCode),
		req.Department, req.Programme, req.Semester, strings.ToUpper(req.Section), req.AcademicYear,
	).Scan(&id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "allocation created", "id": id})
}

func DeleteAllocation(c *gin.Context) {
	id := c.Param("id")
	db.Pool.Exec(context.Background(), `DELETE FROM subject_allocations WHERE id = $1`, id)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
