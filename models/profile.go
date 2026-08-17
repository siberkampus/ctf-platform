package models

// ProfileStats - Profil istatistikleri
type ProfileStats struct {
	TotalMachines   int     `json:"total_machines"`  // çözülen makine sayısı
	TotalQuestions  int     `json:"total_questions"` // çözülen soru sayısı
	TotalPoints     int     `json:"total_points"`
	Rank            int     `json:"rank"`
	Accuracy        float64 `json:"accuracy"` // float64 daha doğru (örn: 87.5%)
	FirstBloods     int     `json:"first_bloods"`
	VIPCount        int     `json:"vip_count"`
	HintUsedCount   int     `json:"hint_used_count"`
	SubmissionCount int     `json:"submission_count"`
	SuccessRate     float64 `json:"success_rate"`
	MemberDays      int     `json:"member_days"`
	Streak          int     `json:"streak"` // bunu da ekleyin, template'de kullanıyoruz
}

// ProfileData - Profil sayfası verileri
type ProfileData struct {
	Title           string
	ProfileUser     *User
	CurrentUser     *User // navbar için oturumdaki kullanıcı
	Stats           ProfileStats
	SolvedMachines  []SolvedMachine
	Badges          []Badge
	RecentActivity  []Activity
	IsOwnProfile    bool
	IsAuthenticated bool
	IsAdmin         bool // admin navbar linki için
	ChartData       ChartData
}
