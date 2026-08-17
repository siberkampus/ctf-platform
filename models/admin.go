package models

import (
	"database/sql"
	"time"
)

// Admin - Admin kullanıcıları (admins tablosu ile uyumlu)
type Admin struct {
	ID           int          `json:"id"`
	Username     string       `json:"username"`
	Email        string       `json:"email"`
	PasswordHash string       `json:"-"`
	Avatar       string       `json:"avatar"`
	Role         string       `json:"role"` // 'admin' veya 'superadmin'
	LastLogin    sql.NullTime `json:"last_login"`
	CreatedAt    time.Time    `json:"created_at"`
	IsActive     bool         `json:"is_active"`
}

// AdminDashboardData - Admin panel ana sayfa verileri
type AdminDashboardData struct {
	Title             string
	Active            string
	Admin             Admin
	Stats             AdminStats
	RecentUsers       []User
	RecentSubmissions []Submission     // Bu alan eklendi
	PopularMachines   []PopularMachine // Bu alan eklendi
	SystemHealth      SystemHealth
	ActivityChart     ChartData
	CurrentDate       string
	ActivePercentage  float64
}

// AdminStats - Admin panel istatistikleri
type AdminStats struct {
	TotalUsers       int     `json:"total_users"`
	NewUsersToday    int     `json:"new_users_today"`
	ActiveUsers      int     `json:"active_users"` // son 24 saat aktif
	TotalMachines    int     `json:"total_machines"`
	TotalSubmissions int     `json:"total_submissions"`
	SubmissionsToday int     `json:"submissions_today"`
	TotalVIPUsers    int     `json:"total_vip_users"`
	VIPRevenue       float64 `json:"vip_revenue"` // vip_purchases'dan hesaplanır
	AveragePoints    float64 `json:"average_points"`
	TopUserPoints    int     `json:"top_user_points"`
	SuccessRate      float64 `json:"success_rate"` // kabul edilen submission oranı
}

// AdminSubmission - Admin panel için submission özeti
type AdminSubmission struct {
	ID            int       `json:"id"`
	Username      string    `json:"username"`
	MachineName   string    `json:"machine_name"`
	QuestionTitle string    `json:"question_title"`
	Status        string    `json:"status"` // pending, accepted, rejected
	SubmittedAt   time.Time `json:"submitted_at"`
}

// SystemHealth - Sistem sağlık durumu
type SystemHealth struct {
	Status              string  `json:"status"` // healthy, warning, critical
	CPUUsage            float64 `json:"cpu_usage"`
	MemoryUsage         float64 `json:"memory_usage"`
	DiskUsage           float64 `json:"disk_usage"`
	ActiveContainers    int     `json:"active_containers"` // machine_sessions'dan
	DatabaseConnections int     `json:"db_connections"`
}

// PopularMachine - Popüler makineler
type PopularMachine struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Difficulty  string `json:"difficulty"`
	Submissions int    `json:"submissions"`  // submission sayısı
	SuccessRate int    `json:"success_rate"` // başarı yüzdesi
}

// ChartData - Grafik verileri
type ChartData struct {
	Labels   []string       `json:"labels"`
	Datasets []ChartDataset `json:"datasets"`
}

// ChartDataset - Grafik dataset
type ChartDataset struct {
	Label string `json:"label"`
	Data  []int  `json:"data"`
}

// AdminLog - Admin işlem logları (admin_logs tablosu ile uyumlu)
type AdminLog struct {
	ID         int       `json:"id"`
	AdminID    int       `json:"admin_id"`
	ActionType string    `json:"action_type"`
	UserID     *int      `json:"user_id"`
	Username   string    `json:"username"`
	IPAddress  string    `json:"ip_address"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`

	// Join için
	AdminUsername string `json:"admin_username,omitempty"`
}
