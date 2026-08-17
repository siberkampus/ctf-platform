package models

import (
	"time"
)

// SystemSettings - Sistem ayarları (system_settings tablosu ile uyumlu)
type SystemSettings struct {
	SiteName          string           `json:"site_name"`
	SiteDescription   string           `json:"site_description"`
	SiteKeywords      string           `json:"site_keywords"`
	MaintenanceMode   bool             `json:"maintenance_mode"`
	RegistrationOpen  bool             `json:"registration_open"`
	DefaultUserPoints int              `json:"default_user_points"`
	SessionTimeout    int              `json:"session_timeout"` // dakika
	MaxUploadSize     int64            `json:"max_upload_size"` // MB
	AllowedFileTypes  []string         `json:"allowed_file_types"`
	EmailSettings     EmailSettings    `json:"email_settings"`
	SecuritySettings  SecuritySettings `json:"security_settings"`
	CTFSettings       CTFSettings      `json:"ctf_settings"`
}

// EmailSettings - E-posta ayarları
type EmailSettings struct {
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	FromEmail    string `json:"from_email"`
}

// SecuritySettings - Güvenlik ayarları
type SecuritySettings struct {
	TwoFactorAuth   bool     `json:"two_factor_auth"`
	PasswordMinLen  int      `json:"password_min_len"`
	PasswordComplex bool     `json:"password_complex"`
	LoginAttempts   int      `json:"login_attempts"`
	BlockDuration   int      `json:"block_duration"` // dakika
	AllowedIPs      []string `json:"allowed_ips"`
	BlockedIPs      []string `json:"blocked_ips"`
}

// CTFSettings - CTF ayarları
type CTFSettings struct {
	EnableCTF        bool      `json:"enable_ctf"`
	CTFStartTime     time.Time `json:"ctf_start_time"`
	CTFEndTime       time.Time `json:"ctf_end_time"`
	MaxTeamSize      int       `json:"max_team_size"`
	EnableScoreboard bool      `json:"enable_scoreboard"`
	FlagFormat       string    `json:"flag_format"`
}

