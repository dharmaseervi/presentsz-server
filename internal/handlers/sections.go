package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yourusername/presentsz-server/internal/db"
)

// GET /sections
func ListSections(c *gin.Context) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT s.id, s.section_code, s.department, s.year, s.section_letter, 
		        s.capacity, ay.year_name,
		        (SELECT COUNT(*) FROM students WHERE section_id = s.id) as student_count
		 FROM sections s
		 LEFT JOIN academic_years ay ON ay.id = s.academic_year_id
		 ORDER BY s.department, s.year, s.section_letter`)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Section struct {
		ID            string `json:"id"`
		SectionCode   string `json:"section_code"`
		Department    string `json:"department"`
		Year          string `json:"year"`
		SectionLetter string `json:"section_letter"`
		Capacity      int    `json:"capacity"`
		AcademicYear  string `json:"academic_year"`
		StudentCount  int    `json:"student_count"`
	}

	var sections []Section
	for rows.Next() {
		var s Section
		rows.Scan(&s.ID, &s.SectionCode, &s.Department, &s.Year, &s.SectionLetter,
			&s.Capacity, &s.AcademicYear, &s.StudentCount)
		sections = append(sections, s)
	}

	c.JSON(http.StatusOK, gin.H{
		"sections": sections,
		"count":    len(sections),
	})
}

// POST /sections (Admin only)
func CreateSection(c *gin.Context) {
	var req struct {
		SectionCode    string `json:"section_code" binding:"required"`
		Department     string `json:"department" binding:"required"`
		Year           string `json:"year" binding:"required"`
		SectionLetter  string `json:"section_letter"`
		Capacity       int    `json:"capacity"`
		AcademicYearID string `json:"academic_year_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.SectionLetter == "" {
		req.SectionLetter = "A"
	}
	if req.Capacity == 0 {
		req.Capacity = 60
	}

	// Get active academic year if not provided
	if req.AcademicYearID == "" {
		err := db.Pool.QueryRow(context.Background(),
			`SELECT id FROM academic_years WHERE is_active = true LIMIT 1`,
		).Scan(&req.AcademicYearID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active academic year"})
			return
		}
	}

	var sectionID string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO sections (section_code, department, year, section_letter, capacity, academic_year_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		strings.ToUpper(req.SectionCode), strings.ToUpper(req.Department),
		req.Year, strings.ToUpper(req.SectionLetter), req.Capacity, req.AcademicYearID,
	).Scan(&sectionID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "section created",
		"section_id": sectionID,
	})
}
