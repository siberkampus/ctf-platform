package models

import (
	"database/sql"
	"time"
)

// Submission - Flag gönderimleri (submissions tablosu ile uyumlu)
type Submission struct {
	ID            int           `json:"id"`
	UserID        int           `json:"user_id"`
	MachineID     int           `json:"machine_id"`
	QuestionID    int           `json:"question_id"`
	SubmittedFlag string        `json:"-"`      // flag metni, JSON'da gösterilmez
	Status        string        `json:"status"` // pending, accepted, rejected
	PointsAwarded int           `json:"points_awarded"`
	AttemptCount  int           `json:"attempt_count"`
	IPAddress     string        `json:"ip_address"`
	UserAgent     string        `json:"user_agent"`
	CreatedAt     time.Time     `json:"created_at"`
	ReviewedAt    sql.NullTime  `json:"reviewed_at"`
	ReviewedBy    sql.NullInt64 `json:"reviewed_by"`
	ReviewNote    string        `json:"review_note"`
	SubmittedAt   time.Time     `json:"submitted_at"`
	// Join için ek alanlar
	Username      string `json:"username,omitempty"`
	MachineName   string `json:"machine_name,omitempty"`
	QuestionTitle string `json:"question_title,omitempty"`
	ReviewerName  string `json:"reviewer_name,omitempty"`
}
