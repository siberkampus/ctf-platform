package models

import (
	"database/sql"
	"time"
)

// Machine - CTF Makineleri (machines tablosu ile uyumlu)
type Machine struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Difficulty   string       `json:"difficulty"`    // easy, medium, hard, expert
	PointsReward int          `json:"points_reward"` // tamamlama puanı
	IsVIPOnly    bool         `json:"is_vip_only"`
	DockerImage  string       `json:"docker_image"`
	CreatorID    int          `json:"creator_id"` // int olarak, 0 = sistem/admin
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    sql.NullTime `json:"updated_at"`
	IsActive     bool         `json:"is_active"`

	// Join/hesaplanan alanlar
	CreatorName    string     `json:"creator,omitempty"`
	SolverCount    int        `json:"solver_count"`    // çözen kullanıcı sayısı
	TotalQuestions int        `json:"total_questions"` // toplam soru sayısı
	Questions      []Question `json:"questions,omitempty"`
	ImageURL       string     `json:"image_url,omitempty"`
	PlayCount      int        `json:"play_count,omitempty"`  // başlatılma sayısı
	SolveCount     int        `json:"solve_count,omitempty"` // çözülme sayısı
}

// Question - Makine soruları (machine_questions tablosu ile uyumlu)
type Question struct {
	ID            int    `json:"id"`
	MachineID     int    `json:"machine_id"`
	QuestionOrder int    `json:"question_order"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	FlagHash      string `json:"-"` // flag hash'i, JSON'da gösterilmez
	PointsReward  int    `json:"points_reward"`
	Hint          string `json:"hint"`
	HintCost      int    `json:"hint_cost"`
	IsActive      bool   `json:"is_active"`

	// Kullanıcıya özel alanlar
	Solved     bool `json:"solved,omitempty"`      // kullanıcı çözdü mü?
	HintUsed   bool `json:"hint_used,omitempty"`   // ipucu kullanıldı mı?
	SolveCount int  `json:"solve_count,omitempty"` // toplam çözülme sayısı
}

// MachineSession - Makine oturumları (machine_sessions tablosu ile uyumlu)
type MachineSession struct {
	ID          int          `json:"id"`
	UserID      int          `json:"user_id"`
	MachineID   string       `json:"machine_id"`
	ContainerID string       `json:"container_id"`
	StartedAt   time.Time    `json:"started_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	EndedAt     sql.NullTime `json:"ended_at"`
	HostPort    int          `json:"host_port"` // YENİ - Container'ın host portu

	// Join için
	MachineName string `json:"machine_name,omitempty"`
	Username    string `json:"username,omitempty"`
}

// UserSolution - Kullanıcı çözümleri (user_solutions tablosu ile uyumlu)
type UserSolution struct {
	ID           int       `json:"id"`
	UserID       int       `json:"user_id"`
	MachineID    int       `json:"machine_id"`
	QuestionID   int       `json:"question_id"`
	AttemptCount int       `json:"attempt_count"`
	UsedHint     bool      `json:"used_hint"`
	SolvedAt     time.Time `json:"solved_at"`
}

// SolvedMachine - Çözülen makineler
type SolvedMachine struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Difficulty string    `json:"difficulty"`
	Points     int       `json:"points"`
	SolvedAt   time.Time `json:"solved_at"`
}
type MachineStats struct {
	TotalSolves    int            `json:"total_solves"`     // Toplam çözen kişi sayısı
	TotalAttempts  int            `json:"total_attempts"`   // Toplam deneme sayısı
	SuccessRate    float64        `json:"success_rate"`     // Başarı oranı (%)
	AvgSolveTime   int            `json:"avg_solve_time"`   // Ortalama çözüm süresi (dakika)
	FirstBlood     string         `json:"first_blood"`      // İlk çözen kullanıcı
	FirstBloodTime *time.Time     `json:"first_blood_time"` // İlk çözüm zamanı
	LastSolve      *time.Time     `json:"last_solve"`       // Son çözüm zamanı
	DailySolves    []DailySolve   `json:"daily_solves"`     // Günlük çözüm istatistikleri
	QuestionStats  []QuestionStat `json:"question_stats"`   // Soru bazlı istatistikler
}
type DailySolve struct {
	Date        string `json:"date"`
	Count       int    `json:"count"`
	UniqueUsers int    `json:"unique_users"`
}

// QuestionStat - Soru bazlı istatistik
type QuestionStat struct {
	QuestionID   int     `json:"question_id"`
	Title        string  `json:"title"`
	SolveCount   int     `json:"solve_count"`
	AttemptCount int     `json:"attempt_count"`
	SuccessRate  float64 `json:"success_rate"`
	FirstBlood   string  `json:"first_blood"`
}
