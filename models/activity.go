package models

import (
	"time"
)

// Activity - Kullanıcı aktiviteleri (activity_logs tablosu ile uyumlu)
type Activity struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	ActionType string    `json:"action_type"` // activity_logs'daki action_type
	MachineID  *int      `json:"machine_id"`  // nullable
	QuestionID *int      `json:"question_id"` // nullable
	IPAddress  string    `json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`

	// Join için ek alanlar
	MachineName   string `json:"machine_name,omitempty"`
	QuestionTitle string `json:"question_title,omitempty"`
	Username      string `json:"username,omitempty"`
}

// Achievement - Başarımlar (achievements tablosu ile uyumlu)
type Achievement struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	PointsReward int    `json:"points_reward"` // points_reward alanı
}

// UserAchievement - Kullanıcı başarımları (user_achievements tablosu ile uyumlu)
type UserAchievement struct {
	ID            int       `json:"id"`
	UserID        int       `json:"user_id"`
	AchievementID int       `json:"achievement_id"`
	EarnedAt      time.Time `json:"earned_at"`

	// Join için
	Achievement *Achievement `json:"achievement,omitempty"`
}

// Badge - Achievement ile aynı (opsiyonel, kullanmıyorsanız silebilirsiniz)
type Badge struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Icon         string    `json:"icon"`
	PointsReward int       `json:"points_reward"` // bunu ekleyin
	EarnedAt     time.Time `json:"earned_at"`
}

// Follower - Takipçi sistemi (veritabanında tablo yok, opsiyonel)
type Follower struct {
	UserID     int       `json:"user_id"`
	FollowerID int       `json:"follower_id"`
	CreatedAt  time.Time `json:"created_at"`

	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

// Solver - Soru çözen kullanıcılar
type Solver struct {
	UserID   int       `json:"user_id"`
	Username string    `json:"username"`
	Avatar   string    `json:"avatar"`
	SolvedAt time.Time `json:"solved_at"`
	UsedHint bool      `json:"used_hint"`
	Points   int       `json:"points"`
}

// UserProgress - Kullanıcı ilerlemesi
type UserProgress struct {
	SolvedQuestions int `json:"solved_questions"`
	TotalQuestions  int `json:"total_questions"`
	SolvedMachines  int `json:"solved_machines"`
	TotalMachines   int `json:"total_machines"`
	HintUsedCount   int `json:"hint_used_count"`
}
