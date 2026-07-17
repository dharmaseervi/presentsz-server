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

	r.GET("/esp32/sessions/active", handlers.GetESP32ActiveSession)
	r.POST("/attendance/ble", handlers.MarkAttendanceBLE) // ← Already public

	// AUTH routes
	auth := r.Group("/auth")
	{
		// REMOVED: auth.POST("/register", handlers.RegisterStudent)  // No self-registration
		auth.POST("/login", handlers.LoginStudent)             // USN-based
		auth.POST("/professor/login", handlers.LoginProfessor) // Email-based
		auth.POST("/admin/login", handlers.LoginAdmin)         // Email + Password
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
		student.GET("/sessions/active", handlers.GetActiveSession)
		student.POST("/students/change-password", handlers.ChangePassword)
	}

	// PROFESSOR routes (AUTH + ROLE REQUIRED)
	professor := r.Group("/")
	professor.Use(middleware.AuthMiddleware(), middleware.RequireRole("professor", "admin"))
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

	// ADMIN routes (admin only)
	admin := r.Group("/admin")
	admin.Use(middleware.AuthMiddleware(), middleware.RequireRole("admin"))
	{
		// Student management
		admin.POST("/students/bulk-upload", handlers.BulkUploadStudents)
		admin.GET("/students/template", handlers.DownloadStudentTemplate)
		admin.GET("/students", handlers.ListStudents)
		admin.POST("/students/:id/reset-password", handlers.ResetStudentPassword)
		admin.POST("/students/:id/reset-device", handlers.ResetStudentDevice)

		// Professor management
		admin.POST("/professors", handlers.CreateProfessor)
		admin.GET("/professors", handlers.ListProfessors)
		admin.DELETE("/professors/:id", handlers.DeleteProfessor)

		// Section management
		admin.GET("/sections", handlers.ListSections)
		admin.POST("/sections", handlers.CreateSection)

		// HODs
		admin.POST("/hods/bulk-upload", handlers.BulkUploadHODs)
		admin.GET("/hods", handlers.ListHODs)
		admin.POST("/hods", handlers.CreateHOD)
		admin.DELETE("/hods/:id", handlers.DeleteHOD)

		// Subjects
		admin.POST("/subjects/bulk-upload", handlers.BulkUploadSubjects)
		admin.GET("/subjects", handlers.ListSubjects)
		admin.POST("/subjects", handlers.CreateSubject)
		admin.DELETE("/subjects/:id", handlers.DeleteSubject)

		// Classrooms
		admin.POST("/classrooms/bulk-upload", handlers.BulkUploadClassrooms)
		admin.GET("/classrooms", handlers.ListClassrooms)
		admin.POST("/classrooms", handlers.CreateClassroom)
		admin.DELETE("/classrooms/:id", handlers.DeleteClassroom)

		admin.POST("/professors/bulk-upload", handlers.BulkUploadFaculty)
		admin.POST("/sections/bulk-upload", handlers.BulkUploadSections)
		admin.POST("/timetable/bulk-upload", handlers.BulkUploadTimetable)
		admin.GET("/timetable", handlers.ListTimetable)
		admin.POST("/timetable", handlers.CreateTimetableEntry)
		admin.DELETE("/timetable/:id", handlers.DeleteTimetableEntry)

		admin.POST("/allocations/bulk-upload", handlers.BulkUploadAllocations)
		admin.GET("/allocations", handlers.ListAllocations)
		admin.POST("/allocations", handlers.CreateAllocation)
		admin.DELETE("/allocations/:id", handlers.DeleteAllocation)
	}
	// Legacy public endpoints (Sections list can be public for dropdown)
	r.GET("/sections", handlers.ListSections)
}
