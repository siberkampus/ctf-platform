package main

import (
	"encoding/gob"
	"log"
	"net/http"
	"os"
	"time"

	"ctf-platform/database"
	"ctf-platform/handlers"
	"ctf-platform/middleware"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

var (
	store = sessions.NewCookieStore([]byte("super-secret-key"))
)

func init() {
	gob.Register(int(0))
}

func main() {
	// Veritabanı bağlantısı
	db, err := database.Connect()
	if err != nil {
		log.Fatal("Veritabanı bağlantı hatası:", err)
	}
	defer db.Close()

	// Gerekli dizinleri oluştur
	dirs := []string{
		"uploads/avatars",
		"uploads/machines",
		"uploads/temp",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Dizin oluşturulamadı %s: %v", dir, err)
		}
	}

	// Temizlik scheduler'ını başlat
	handlers.StartCleanupScheduler(db)

	// Router oluştur
	r := mux.NewRouter()
	r.Use(middleware.Logger)

	// WebSocket
	r.HandleFunc("/ws/machines/{id}/terminal", handlers.TerminalWebSocket(db, store))

	// Statik dosyalar
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads/"))))

	// ==================== API ROUTER ====================
	api := r.PathPrefix("/api").Subrouter()
	api.Use(middleware.Logger)

	// Public API endpoints
	api.HandleFunc("/auth/login", handlers.Login(db, store)).Methods("POST", "OPTIONS")
	api.HandleFunc("/auth/register", handlers.Register(db)).Methods("POST", "OPTIONS")
	api.HandleFunc("/auth/logout", handlers.Logout(store)).Methods("POST", "OPTIONS")
	api.HandleFunc("/auth/refresh", handlers.RefreshToken(db)).Methods("POST", "OPTIONS")

	// Public endpoints
	api.HandleFunc("/machines", handlers.APIGetMachines(db)).Methods("GET")
	api.HandleFunc("/user/progress", handlers.APIUserProgress(db, store)).Methods("GET")
	api.HandleFunc("/machines/{id}", handlers.GetMachineDetail(db)).Methods("GET")
	api.HandleFunc("/leaderboard", handlers.GetLeaderboard(db)).Methods("GET")
	api.HandleFunc("/profile/{username}", handlers.GetPublicProfile(db)).Methods("GET")
	api.HandleFunc("/academy/lesson/{lessonId}/question/{questionId}/answer", handlers.APIAnswerSectionQuestion(db, store)).Methods("POST")
	api.HandleFunc("/academy/lesson/{id}/complete", handlers.APICompleteLesson(db, store)).Methods("POST")
	api.HandleFunc("/academy/practical/{questionId}/answer", handlers.APIAnswerPracticalQuestion(db, store)).Methods("POST")

	// ==================== PROTECTED API ENDPOINTS ====================
	protected := api.PathPrefix("/").Subrouter()
	protected.Use(middleware.Auth(store))

	// Makine işlemleri
	protected.HandleFunc("/machines/{id}/start", handlers.StartMachine(db, store)).Methods("POST")
	protected.HandleFunc("/machines/{id}/stop", handlers.StopMachine(db, store)).Methods("POST")
	protected.HandleFunc("/machines/{id}/submit", handlers.SubmitFlag(db, store)).Methods("POST")
	protected.HandleFunc("/machines/{id}/hints/{questionId}", handlers.GetHint(db, store)).Methods("GET")

	// VIP işlemleri
	protected.HandleFunc("/vip/purchase", handlers.PurchaseVIP(db)).Methods("POST")
	protected.HandleFunc("/vip/status", handlers.GetVIPStatus(db)).Methods("GET")

	// ============ AYARLAR API (TEK VE DOĞRU) ============
	protected.HandleFunc("/user/profile", handlers.UpdateProfileHandler(db, store)).Methods("PUT")
	protected.HandleFunc("/user/avatar", handlers.UploadAvatarHandler(db, store)).Methods("POST")
	protected.HandleFunc("/user/security", handlers.UpdateSecurityHandler(db, store)).Methods("PUT")
	protected.HandleFunc("/user/settings", handlers.UpdateSettingsHandler(db, store)).Methods("PUT")
	protected.HandleFunc("/user/delete", handlers.DeleteAccountHandler(db, store)).Methods("DELETE")

	// Oturum işlemleri
	protected.HandleFunc("/sessions", handlers.GetSessionsHandler(db, store)).Methods("GET")
	protected.HandleFunc("/sessions/{id}", handlers.TerminateSessionHandler(db, store)).Methods("DELETE")
	protected.HandleFunc("/sessions/terminate-all", handlers.TerminateAllSessionsHandler(db, store)).Methods("POST")

	// ==================== PAGE ROUTES (HTML) ====================
	r.HandleFunc("/", handlers.HomePage(db, store)).Methods("GET")
	r.HandleFunc("/login", handlers.LoginPage(store)).Methods("GET")
	r.HandleFunc("/register", handlers.RegisterPage()).Methods("GET")
	r.HandleFunc("/machines", handlers.MachinesPage(db, store)).Methods("GET")
	r.HandleFunc("/machine/{id}", handlers.MachineDetailPage(db, store)).Methods("GET")
	r.HandleFunc("/leaderboard", handlers.LeaderboardPage(db, store)).Methods("GET")
	r.HandleFunc("/profile/{username}", handlers.ProfilePage(db, store)).Methods("GET")
	r.HandleFunc("/dashboard", handlers.DashboardPage(db, store)).Methods("GET")
	r.HandleFunc("/vip", handlers.VIPPage(db, store)).Methods("GET")
	r.HandleFunc("/settings", handlers.SettingsPage(db, store)).Methods("GET")
	r.HandleFunc("/academy", handlers.AcademyHomePage(db, store)).Methods("GET")
	r.HandleFunc("/academy/lesson/{slug}", handlers.AcademyLessonPage(db, store)).Methods("GET")

	// ==================== ADMIN PANEL ====================
	admin := r.PathPrefix("/admin").Subrouter()

	// Public admin routes
	admin.HandleFunc("/login", handlers.AdminLoginPage).Methods("GET")
	admin.HandleFunc("/login", handlers.AdminLogin(db, store)).Methods("POST")

	// Protected admin routes
	adminProtected := admin.PathPrefix("/").Subrouter()
	adminProtected.Use(middleware.AdminAuth(store))

	// ============ ADMIN HTML SAYFALARI ============
	adminProtected.HandleFunc("/dashboard", handlers.AdminDashboard(db, store)).Methods("GET")
	adminProtected.HandleFunc("/users", handlers.AdminUsersPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/users/add", handlers.AdminAddUserForm(db, store)).Methods("GET")
	adminProtected.HandleFunc("/users/edit/{id}", handlers.AdminEditUserForm(db, store)).Methods("GET")
	adminProtected.HandleFunc("/machines", handlers.AdminMachinesPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/machines/add", handlers.AdminAddMachineForm(db, store)).Methods("GET")
	adminProtected.HandleFunc("/machines/edit/{id}", handlers.AdminEditMachineForm(db, store)).Methods("GET")
	adminProtected.HandleFunc("/machines/{id}", handlers.AdminMachineDetail(db, store)).Methods("GET")
	adminProtected.HandleFunc("/questions", handlers.AdminQuestionsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/vip", handlers.AdminVIPPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/stats", handlers.AdminStatsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/logs", handlers.AdminLogsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/settings", handlers.AdminSettingsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/submissions", handlers.AdminSubmissionsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/backup", handlers.AdminBackupPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/academy/lessons/{id}/questions", handlers.AdminAcademyQuestionsPage(db, store)).Methods("GET")
	adminProtected.HandleFunc("/academy", handlers.AdminAcademyPage(db, store)).Methods("GET")

	// ============ ADMIN API ENDPOINT'LERİ ============
	adminAPI := adminProtected.PathPrefix("/api").Subrouter()

	// User API
	adminAPI.HandleFunc("/users", handlers.AdminUsers(db, store)).Methods("GET")
	adminAPI.HandleFunc("/users/{id}", handlers.AdminUserDetailHandler(db, store)).Methods("GET")
	adminAPI.HandleFunc("/users/{id}", handlers.AdminUpdateUser(db)).Methods("PUT")
	adminAPI.HandleFunc("/users/{id}/toggle-status", handlers.AdminToggleUserStatus(db)).Methods("POST")
	adminAPI.HandleFunc("/users/{id}/toggle-vip", handlers.AdminToggleUserVIP(db)).Methods("POST")
	adminAPI.HandleFunc("/users/{id}/reset-password", handlers.AdminResetPassword(db)).Methods("POST")
	adminAPI.HandleFunc("/users/{id}", handlers.AdminDeleteUser(db)).Methods("DELETE")
	adminAPI.HandleFunc("/users/create", handlers.AdminCreateUser(db)).Methods("POST")

	// Machine API
	adminAPI.HandleFunc("/machines", handlers.AdminMachines(db)).Methods("GET")
	adminAPI.HandleFunc("/machines", handlers.AdminCreateMachine(db, store)).Methods("POST")
	adminAPI.HandleFunc("/machines/{id}", handlers.AdminUpdateMachine(db, store)).Methods("POST")
	adminAPI.HandleFunc("/machines/{id}/toggle", handlers.AdminToggleMachine(db)).Methods("POST")
	adminAPI.HandleFunc("/machines/{id}", handlers.AdminDeleteMachine(db)).Methods("DELETE")
	adminAPI.HandleFunc("/machines/{id}", handlers.AdminGetMachine(db)).Methods("GET")

	// Question API
	adminAPI.HandleFunc("/questions", handlers.AdminQuestions(db)).Methods("GET")
	adminAPI.HandleFunc("/questions", handlers.AdminCreateQuestion(db)).Methods("POST")
	adminAPI.HandleFunc("/questions/{id}", handlers.AdminUpdateQuestion(db, store)).Methods("PUT")
	adminAPI.HandleFunc("/questions/{id}/toggle", handlers.AdminToggleQuestion(db)).Methods("POST")
	adminAPI.HandleFunc("/questions/{id}", handlers.AdminDeleteQuestion(db)).Methods("DELETE")
	adminAPI.HandleFunc("/questions/{id}", handlers.AdminGetQuestion(db)).Methods("GET")
	adminAPI.HandleFunc("/machines/{id}/questions", handlers.AdminGetMachineQuestions(db)).Methods("GET")

	// VIP API
	adminAPI.HandleFunc("/vip", handlers.AdminVIPManagement(db)).Methods("GET")
	adminAPI.HandleFunc("/vip/plans", handlers.AdminVIPPlans(db)).Methods("GET")
	adminAPI.HandleFunc("/vip/plans", handlers.AdminCreateVIPPlan(db)).Methods("POST")
	adminAPI.HandleFunc("/vip/plans/{id}", handlers.AdminUpdateVIPPlan(db)).Methods("PUT")
	adminAPI.HandleFunc("/vip/plans/{id}", handlers.AdminDeleteVIPPlan(db)).Methods("DELETE")
	adminAPI.HandleFunc("/vip/purchases", handlers.AdminVIPPurchases(db)).Methods("GET")

	// Stats API
	adminAPI.HandleFunc("/stats", handlers.AdminSystemStats(db)).Methods("GET")
	adminAPI.HandleFunc("/stats/machines/{id}", handlers.AdminMachineStats(db)).Methods("GET")
	adminAPI.HandleFunc("/stats/dashboard", handlers.AdminDashboardStats(db)).Methods("GET")
	adminAPI.HandleFunc("/stats/profile/{id}", handlers.AdminProfileStats(db)).Methods("GET")
	adminAPI.HandleFunc("/stats/users", handlers.AdminUsersWithStats(db)).Methods("GET")

	// Logs API
	adminAPI.HandleFunc("/logs", handlers.AdminLogs(db)).Methods("GET")
	adminAPI.HandleFunc("/logs/export", handlers.AdminExportLogs(db)).Methods("GET")

	// Backup API
	adminAPI.HandleFunc("/backup/list", handlers.AdminBackupList(db)).Methods("GET")
	adminAPI.HandleFunc("/backup/create", handlers.AdminCreateBackup(db)).Methods("POST")
	adminAPI.HandleFunc("/backup/upload", handlers.AdminUploadBackup(db)).Methods("POST")
	adminAPI.HandleFunc("/backup/{id}/download", handlers.AdminDownloadBackup(db)).Methods("GET")
	adminAPI.HandleFunc("/backup/{id}/delete", handlers.AdminDeleteBackup(db)).Methods("DELETE")
	adminAPI.HandleFunc("/backup/{id}/restore", handlers.AdminRestoreBackup(db)).Methods("POST")

	// Academy API (Admin)
	adminAPI.HandleFunc("/academy/categories", handlers.AdminGetCategories(db)).Methods("GET")
	adminAPI.HandleFunc("/academy/categories", handlers.AdminCreateCategory(db)).Methods("POST")
	adminAPI.HandleFunc("/academy/categories/{id}", handlers.AdminUpdateCategory(db)).Methods("PUT")
	adminAPI.HandleFunc("/academy/categories/{id}", handlers.AdminDeleteCategory(db)).Methods("DELETE")

	adminAPI.HandleFunc("/academy/lessons", handlers.AdminGetLessons(db)).Methods("GET")
	adminAPI.HandleFunc("/academy/lessons", handlers.AdminCreateLesson(db)).Methods("POST")
	adminAPI.HandleFunc("/academy/lessons/{id}", handlers.AdminGetLesson(db)).Methods("GET")
	adminAPI.HandleFunc("/academy/lessons/{id}", handlers.AdminUpdateLesson(db)).Methods("PUT")
	adminAPI.HandleFunc("/academy/lessons/{id}", handlers.AdminDeleteLesson(db)).Methods("DELETE")

	adminAPI.HandleFunc("/academy/lessons/{id}/questions", handlers.AdminGetQuestions(db)).Methods("GET")
	adminAPI.HandleFunc("/academy/questions", handlers.AdminCreateQuestionAcademy(db)).Methods("POST")
	adminAPI.HandleFunc("/academy/questions/{id}", handlers.AdminUpdateQuestionAcademy(db)).Methods("PUT")
	adminAPI.HandleFunc("/academy/questions/{id}", handlers.AdminDeleteQuestionAcademy(db)).Methods("DELETE")

	adminAPI.HandleFunc("/academy/lessons/{id}/practical", handlers.AdminGetPracticalQuestions(db)).Methods("GET")
	adminAPI.HandleFunc("/academy/practical", handlers.AdminCreatePracticalQuestion(db)).Methods("POST")
	adminAPI.HandleFunc("/academy/practical/{id}", handlers.AdminUpdatePracticalQuestion(db)).Methods("PUT")
	adminAPI.HandleFunc("/academy/practical/{id}", handlers.AdminDeletePracticalQuestion(db)).Methods("DELETE")

	// Settings API
	adminAPI.HandleFunc("/settings", handlers.AdminSettings(db)).Methods("GET")

	// Admin logout
	adminAPI.HandleFunc("/logout", handlers.AdminLogout(store)).Methods("POST")

	// ==================== SUNUCUYU BAŞLAT ====================
	srv := &http.Server{
		Handler:      r,
		Addr:         ":8181",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Println("Sunucu başlatılıyor: http://localhost:8181")
	log.Println("Admin panel: http://localhost:8181/admin/login")
	log.Fatal(srv.ListenAndServe())
}