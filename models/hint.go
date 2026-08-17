package models

import (
	"time"
)

// HintUsage - İpucu kullanımı (hint_usage tablosu ile uyumlu)
type HintUsage struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	MachineID  int       `json:"machine_id"`
	QuestionID int       `json:"question_id"`
	UsedAt     time.Time `json:"used_at"`

	// Join için
	Username      string `json:"username,omitempty"`
	MachineName   string `json:"machine_name,omitempty"`
	QuestionTitle string `json:"question_title,omitempty"`
}
