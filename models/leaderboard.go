package models

// LeaderboardEntry - Lider tablosu girişi
type LeaderboardEntry struct {
	ID             int    `json:"id"`
	Username       string `json:"username"`
	Avatar         string `json:"avatar"`
	Country        string `json:"country"`
	Points         int    `json:"points"`
	Rank           int    `json:"rank"`
	IsVIP          bool   `json:"is_vip"`
	MachinesSolved int    `json:"machines_solved"`   // çözülen makine sayısı
	QuestionsSolved int   `json:"questions_solved"`  // çözülen soru sayısı
	Accuracy       int    `json:"accuracy"`          // başarı yüzdesi
}