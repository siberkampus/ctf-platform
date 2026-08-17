package models

import (
	"database/sql"
	"time"
)

// UserSession - Kullanıcı oturumları (user_sessions tablosu ile uyumlu)
type UserSession struct {
	ID           int          `json:"id"`
	UserID       int          `json:"user_id"`
	SessionToken string       `json:"-"`
	Device       string       `json:"device"`
	IPAddress    string       `json:"ip_address"`
	Location     string       `json:"location"`
	UserAgent    string       `json:"user_agent"`
	CreatedAt    time.Time    `json:"created_at"`
	LastActivity time.Time    `json:"last_activity"`
	ExpiresAt    time.Time    `json:"expires_at"`
	IsActive     bool         `json:"is_active"`
	TerminatedAt sql.NullTime `json:"terminated_at"`
}
