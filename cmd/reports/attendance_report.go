package reports

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/yourusername/presentsz-server/internal/db"
)

type StudentSummary struct {
	Name       string
	RollNumber string
	Email      string
	Present    int
	Late       int
	Absent     int
	Excused    int
	Total      int
	Rate       float64
}

type SessionLogRow struct {
	Date        string
	StudentName string
	RollNumber  string
	Email       string
	Status      string
	MarkedAt    string
	MarkedBy    string
}

// BuildAttendanceReport queries attendance for a professor's subject(s) within
// [startDate, endDate] and returns an in-memory xlsx with two sheets:
// "Summary" (one row per student, aggregated) and "Daily Log" (one row per
// attendance record). subjectCode == "" means all subjects this professor teaches.
func BuildAttendanceReport(professorID, department, facultyCode, subjectCode, startDate, endDate string) (*excelize.File, error) {
	ctx := context.Background()

	query := `
		SELECT s.name, s.roll_number, s.email, a.status, a.marked_at, a.marked_by,
		       asn.subject, DATE(asn.start_time) as class_date
		FROM attendance a
		JOIN attendance_sessions asn ON asn.id = a.session_id
		JOIN students s ON s.id = a.student_id
		JOIN professors p ON p.id = asn.professor_id
		WHERE DATE(asn.start_time) >= $1
		  AND DATE(asn.start_time) <= $2`
	args := []interface{}{startDate, endDate}
	argIdx := 3

	if department != "" {
		// HOD / department-wide scope
		query += fmt.Sprintf(" AND p.department = $%d", argIdx)
		args = append(args, department)
		argIdx++
	} else {
		// Professor scope — only their own sessions
		query += fmt.Sprintf(" AND asn.professor_id = $%d", argIdx)
		args = append(args, professorID)
		argIdx++
	}

	if subjectCode != "" {
		query += fmt.Sprintf(" AND asn.subject = $%d", argIdx)
		args = append(args, subjectCode)
	}
	query += " ORDER BY asn.start_time ASC, s.roll_number ASC"

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var logRows []SessionLogRow
	summaryMap := map[string]*StudentSummary{} // keyed by roll_number

	for rows.Next() {
		var name, roll, email, status, markedBy, subject string
		var markedAt, classDate time.Time
		if err := rows.Scan(&name, &roll, &email, &status, &markedAt, &markedBy, &subject, &classDate); err != nil {
			continue
		}

		logRows = append(logRows, SessionLogRow{
			Date:        classDate.Format("2006-01-02"),
			StudentName: name,
			RollNumber:  roll,
			Email:       email,
			Status:      status,
			MarkedAt:    markedAt.Format("15:04"),
			MarkedBy:    markedBy,
		})

		if _, ok := summaryMap[roll]; !ok {
			summaryMap[roll] = &StudentSummary{Name: name, RollNumber: roll, Email: email}
		}
		sum := summaryMap[roll]
		sum.Total++
		switch status {
		case "present":
			sum.Present++
		case "late":
			sum.Late++
		case "absent":
			sum.Absent++
		case "excused":
			sum.Excused++
		}
	}

	// Compute rates and produce a stable, roll-number-sorted summary slice
	var summary []StudentSummary
	for _, s := range summaryMap {
		if s.Total > 0 {
			s.Rate = float64(s.Present+s.Late) / float64(s.Total) * 100
		}
		summary = append(summary, *s)
	}
	sortSummary(summary)

	f := excelize.NewFile()
	defer func() { _ = f }() // caller closes after writing

	buildSummarySheet(f, summary, facultyCode, subjectCode, startDate, endDate)
	buildDailyLogSheet(f, logRows)

	f.DeleteSheet("Sheet1")
	f.SetActiveSheet(0)

	return f, nil
}

func sortSummary(s []StudentSummary) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].RollNumber < s[j-1].RollNumber; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func buildSummarySheet(f *excelize.File, summary []StudentSummary, facultyCode, subjectCode, startDate, endDate string) {
	sheet := "Summary"
	f.NewSheet(sheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"171717"}, Pattern: 1},
	})

	f.SetCellValue(sheet, "A1", "Attendance Report")
	f.SetCellValue(sheet, "A2", fmt.Sprintf("Faculty: %s", facultyCode))
	subj := subjectCode
	if subj == "" {
		subj = "All subjects"
	}
	f.SetCellValue(sheet, "A3", fmt.Sprintf("Subject: %s", subj))
	f.SetCellValue(sheet, "A4", fmt.Sprintf("Period: %s to %s", startDate, endDate))

	headers := []string{"Roll Number", "Name", "Email", "Present", "Late", "Absent", "Excused", "Total Classes", "Attendance %"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 6)
		f.SetCellValue(sheet, cell, h)
	}
	f.SetCellStyle(sheet, "A6", "I6", headerStyle)

	row := 7
	for _, s := range summary {
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), s.RollNumber)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), s.Name)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), s.Email)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), s.Present)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), s.Late)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), s.Absent)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), s.Excused)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), s.Total)
		f.SetCellValue(sheet, fmt.Sprintf("I%d", row), fmt.Sprintf("%.1f%%", s.Rate))
		row++
	}

	widths := map[string]float64{"A": 14, "B": 22, "C": 28, "D": 10, "E": 10, "F": 10, "G": 10, "H": 14, "I": 14}
	for col, w := range widths {
		f.SetColWidth(sheet, col, col, w)
	}
}

func buildDailyLogSheet(f *excelize.File, logRows []SessionLogRow) {
	sheet := "Daily Log"
	f.NewSheet(sheet)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"171717"}, Pattern: 1},
	})

	headers := []string{"Date", "Roll Number", "Name", "Email", "Status", "Marked At", "Marked By"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	f.SetCellStyle(sheet, "A1", "G1", headerStyle)

	for i, r := range logRows {
		row := i + 2
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), r.Date)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), r.RollNumber)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), r.StudentName)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), r.Email)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), r.Status)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), r.MarkedAt)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), r.MarkedBy)
	}

	widths := map[string]float64{"A": 12, "B": 14, "C": 22, "D": 28, "E": 10, "F": 10, "G": 12}
	for col, w := range widths {
		f.SetColWidth(sheet, col, col, w)
	}
}

// ToBytes serializes the workbook for HTTP response or email attachment.
func ToBytes(f *excelize.File) ([]byte, error) {
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var _ = bytes.NewBuffer // keep import if unused elsewhere
