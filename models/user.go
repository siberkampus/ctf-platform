package models

import (
	"database/sql"
	"time"
)

// User - Platform kullanıcıları (users tablosu ile tam uyumlu)
type User struct {
	ID               int          `json:"id"`
	Username         string       `json:"username"`
	Email            string       `json:"email"`
	PasswordHash     string       `json:"-"` // hash, JSON'da gösterilmez
	Avatar           string       `json:"avatar"`
	Bio              string       `json:"bio"`
	Country          string       `json:"country"` // location değil, country
	Website          string       `json:"website"`
	IsVIP            bool         `json:"is_vip"`
	VIPExpiryDate    sql.NullTime `json:"vip_expiry_date"`
	Points           int          `json:"points"`
	Rank             int          `json:"rank"`
	TwoFactorEnabled bool         `json:"two_factor_enabled"`
	CreatedAt        time.Time    `json:"created_at"`
	LastLogin        sql.NullTime `json:"last_login"`
	IsActive         bool         `json:"is_active"`
	FullName         string       `json:"fullname"` // fullname olarak veritabanında
	ReferralCode     string       `json:"referral_code"`
	Newsletter       bool         `json:"newsletter"`
	EmailVerified    bool         `json:"email_verified"`
}

// UserWithStats - İstatistikli kullanıcı modeli
type UserWithStats struct {
	User
	SolvedCount     int     `json:"solved_count"`     // user_solutions'dan hesaplanır
	HintUsedCount   int     `json:"hint_used_count"`  // hint kullanılan soru sayısı
	AttemptCount    int     `json:"attempt_count"`    // toplam deneme sayısı
	SuccessRate     float64 `json:"success_rate"`     // başarı oranı
	SubmissionCount int     `json:"submission_count"` // toplam submission
	MachineCount    int     `json:"machine_count"`    // çözülen makine sayısı
}

// UserSettings - Kullanıcı ayarları (user_settings tablosu ile uyumlu)
type UserSettings struct {
	UserID               int       `json:"user_id"`
	EmailNotifications   bool      `json:"email_notifications"`
	BrowserNotifications bool      `json:"browser_notifications"`
	SoundEnabled         bool      `json:"sound_enabled"`
	ProfilePublic        bool      `json:"profile_public"`
	ShowActivity         bool      `json:"show_activity"`
	ShowOnlineStatus     bool      `json:"show_online_status"`
	Theme                string    `json:"theme"`
	FontSize             string    `json:"font_size"`
	Language             string    `json:"language"`
	UpdatedAt            time.Time `json:"updated_at"`
}
