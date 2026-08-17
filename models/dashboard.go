package models

// DashboardData - Kullanıcı panel ana sayfa verileri
type DashboardData struct {
	Title           string
	User            *User
	IsAuthenticated bool
	IsAdmin         bool
	Stats           DashboardStats
	RecentActivity  []Activity
	InProgress      []InProgressMachine
	Achievements    []UserAchievement
	ChartData       ChartData
}

// DashboardStats - Kullanıcı panel istatistikleri
// models/dashboard.go içinde
type DashboardStats struct {
	TotalPoints        int `json:"total_points"`
	TotalSolved        int `json:"total_solved"`         // çözülen toplam soru
	TotalMachines      int `json:"total_machines"`       // çözülen toplam makine
	TotalMachinesCount int `json:"total_machines_count"` // toplam makine sayısı
	Rank               int `json:"rank"`
	DailyGoal          int `json:"daily_goal"`
	DailyProgress      int `json:"daily_progress"`
	Streak             int `json:"streak"`
	VIPCount           int `json:"vip_count"`
}

// InProgressMachine - Devam eden makineler
type InProgressMachine struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Difficulty string `json:"difficulty"`
	Solved     int    `json:"solved"` // çözülen soru sayısı
	Total      int    `json:"total"`  // toplam soru sayısı
}

// Dataset - Grafik dataset (Dashboard için)
type Dataset struct {
	Label string
	Data  []int
}
