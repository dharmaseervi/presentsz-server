package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/yourusername/presentsz-server/internal/handlers"
	"github.com/yourusername/presentsz-server/internal/middleware"
)

func Setup(r *gin.Engine) {
	// PUBLIC routes (NO AUTH)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.GET("/sessions/active", handlers.GetActiveSession)  // ← MOVED HERE (public for ESP32)
	r.POST("/attendance/ble", handlers.MarkAttendanceBLE) // ← Already public

	auth := r.Group("/auth")
	{
		auth.POST("/register", handlers.RegisterStudent)
		auth.POST("/login", handlers.LoginStudent)
		auth.POST("/professor/login", handlers.LoginProfessor)
		auth.POST("/professor/register", handlers.RegisterProfessor)
	}

	// STUDENT routes (AUTH REQUIRED)
	student := r.Group("/")
	student.Use(middleware.AuthMiddleware())
	{
		student.POST("/attendance/mark", handlers.MarkAttendance)
		student.GET("/students/:id", handlers.GetStudent)
		student.POST("/students/:id/register-ble", handlers.RegisterBLE)
		student.GET("/students/:id/attendance", handlers.GetStudentAttendance)
		student.GET("/classrooms/:room_name/count", handlers.GetClassroomCount)
		student.GET("/timetable", handlers.GetTimetable)
	}

	// PROFESSOR routes (AUTH + ROLE REQUIRED)
	professor := r.Group("/")
	professor.Use(middleware.AuthMiddleware(), middleware.RequireRole("professor"))
	{
		professor.POST("/sessions", handlers.StartSession)
		professor.GET("/classrooms", handlers.GetClassrooms)
		professor.POST("/sessions/:session_id/stop", handlers.StopSession)
		professor.GET("/sessions/:session_id/attendance", handlers.GetSessionAttendance)
		professor.POST("/timetable", handlers.AddTimetableEntry)
		professor.DELETE("/timetable/:id", handlers.DeleteTimetableEntry)
		professor.POST("/timetable/copy-week", handlers.CopyWeek)
		professor.GET("/timetable/week", handlers.GetTimetableWeek)
		professor.GET("/professor/:id/sessions", handlers.GetProfessorSessions)
		professor.GET("/sessions/:session_id/students", handlers.GetEligibleStudents)
		professor.POST("/sessions/:session_id/override", handlers.OverrideAttendance)
		professor.DELETE("/sessions/:session_id/attendance/:student_id", handlers.RemoveAttendance)
	}
}
