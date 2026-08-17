package handlers

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"time"

	"ctf-platform/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// GetPublicProfile - Kullanıcının public profilini JSON olarak getir (API)
func GetPublicProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		username := vars["username"]

		var user models.User
		var avatar, bio, country, website sql.NullString
		var vipExpiryDate sql.NullTime
		var lastLogin sql.NullTime
		var fullName, referralCode sql.NullString

		err := db.QueryRow(`
            SELECT 
                id, username, email, 
                COALESCE(avatar, '/static/images/avatar.png') as avatar,
                COALESCE(bio, '') as bio,
                COALESCE(country, '') as country,
                COALESCE(website, '') as website,
                is_vip, vip_expiry_date, points, rank, two_factor_enabled,
                created_at, last_login, is_active,
                COALESCE(fullname, '') as fullname,
                COALESCE(referral_code, '') as referral_code,
                newsletter, email_verified
            FROM users
            WHERE username = $1 AND is_active = true
        `, username).Scan(
			&user.ID, &user.Username, &user.Email,
			&avatar, &bio, &country, &website,
			&user.IsVIP, &vipExpiryDate, &user.Points, &user.Rank,
			&user.TwoFactorEnabled, &user.CreatedAt, &lastLogin, &user.IsActive,
			&fullName, &referralCode, &user.Newsletter, &user.EmailVerified,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Kullanıcı bulunamadı")
			} else {
				log.Printf("profil sorgu hatası: %v", err)
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			}
			return
		}

		// Null değerleri ata
		user.Avatar = avatar.String
		user.Bio = bio.String
		user.Country = country.String
		user.Website = website.String
		user.FullName = fullName.String
		user.ReferralCode = referralCode.String

		// VIPExpiryDate'i ata
		if vipExpiryDate.Valid {
			user.VIPExpiryDate = vipExpiryDate
		}

		// LastLogin'i ata - DÜZELTİLDİ: sql.NullTime.Time kullan
		if lastLogin.Valid {
			user.LastLogin = lastLogin
		}

		// İstatistikleri hesapla
		stats := calculateUserStats(db, user.ID)

		// Çözülen makineler
		solvedMachines, err := getUserSolvedMachines(db, user.ID, 20)
		if err != nil {
			log.Printf("Çözülen makineler sorgu hatası: %v", err)
			solvedMachines = []models.SolvedMachine{}
		}

		// Rozetler (başarımlar)
		badges, err := getUserBadges(db, user.ID)
		if err != nil {
			log.Printf("Rozet sorgu hatası: %v", err)
			badges = []models.Badge{}
		}

		// Son aktiviteler
		activities, err := getUserRecentActivities(db, user.ID, 20)
		if err != nil {
			log.Printf("Aktivite sorgu hatası: %v", err)
			activities = []models.Activity{}
		}

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"user":            user,
			"stats":           stats,
			"solved_machines": solvedMachines,
			"badges":          badges,
			"activities":      activities,
		})
	}
}


func ProfilePage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := vars["username"]

		// ─── Oturum kontrolü ───────────────────────────────────────────
		session, _ := store.Get(r, "session")
		isAuth := false
		currentUserID := 0
		var currentUser *models.User

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			if id, ok := session.Values["user_id"].(int); ok {
				currentUserID = id

				// Oturumu açık kullanıcıyı çek (navbar için)
				u := &models.User{}
				var av, bi, co, we sql.NullString
				err := db.QueryRow(`
					SELECT id, username, email,
						COALESCE(avatar, '/static/images/avatar.png'),
						COALESCE(bio,''), COALESCE(country,''), COALESCE(website,''),
						is_vip, points, rank, created_at
					FROM users WHERE id = $1
				`, currentUserID).Scan(
					&u.ID, &u.Username, &u.Email,
					&av, &bi, &co, &we,
					&u.IsVIP, &u.Points, &u.Rank, &u.CreatedAt,
				)
				if err == nil {
					u.Avatar = av.String
					u.Bio = bi.String
					u.Country = co.String
					u.Website = we.String
					currentUser = u
				}
			}
		}

		// ─── Profil kullanıcısını getir ────────────────────────────────
		var profileUser models.User
		var avatar, bio, country, website sql.NullString

		err := db.QueryRow(`
			SELECT 
				id, username, email,
				COALESCE(avatar, '/static/images/avatar.png'),
				COALESCE(bio, ''),
				COALESCE(country, ''),
				COALESCE(website, ''),
				is_vip, points, rank, created_at
			FROM users
			WHERE username = $1 AND is_active = true
		`, username).Scan(
			&profileUser.ID, &profileUser.Username, &profileUser.Email,
			&avatar, &bio, &country, &website,
			&profileUser.IsVIP, &profileUser.Points, &profileUser.Rank,
			&profileUser.CreatedAt,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Kullanıcı bulunamadı", http.StatusNotFound)
			} else {
				log.Printf("Profil sorgu hatası: %v", err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}
		profileUser.Avatar = avatar.String
		profileUser.Bio = bio.String
		profileUser.Country = country.String
		profileUser.Website = website.String

		// ─── Admin kontrolü ────────────────────────────────────────────
		isAdmin := false
		if currentUserID > 0 {
			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, currentUserID).Scan(&isAdmin)
		}

		// ─── İstatistikler ─────────────────────────────────────────────
		stats := calculateUserStats(db, profileUser.ID)

		// ─── Çözülen makineler ─────────────────────────────────────────
		solvedMachines, err := getUserSolvedMachines(db, profileUser.ID, 20)
		if err != nil {
			log.Printf("Çözülen makineler hatası: %v", err)
			solvedMachines = []models.SolvedMachine{}
		}

		// ─── Rozetler ──────────────────────────────────────────────────
		badges, err := getUserBadges(db, profileUser.ID)
		if err != nil {
			log.Printf("Rozet hatası: %v", err)
			badges = []models.Badge{}
		}

		// ─── Aktiviteler ───────────────────────────────────────────────
		activities, err := getUserRecentActivities(db, profileUser.ID, 10)
		if err != nil {
			log.Printf("Aktivite hatası: %v", err)
			activities = []models.Activity{}
		}

		// ─── Grafik verisi ─────────────────────────────────────────────
		weeklyRaw := getWeeklyChartData(db, profileUser.ID)
		chartData := models.ChartData{
			Labels: []string{"Pzt", "Sal", "Çar", "Per", "Cum", "Cmt", "Paz"},
			Datasets: []models.ChartDataset{
				{
					Label: "Çözülen Sorular",
					Data:  weeklyRaw[:],
				},
			},
		}

		// ─── Template verisi ───────────────────────────────────────────
		data := models.ProfileData{
			Title:           profileUser.Username + " - Kullanıcı Profili - CTF HACK PLATFORMU",
			ProfileUser:     &profileUser,
			CurrentUser:     currentUser,
			Stats:           stats,
			SolvedMachines:  solvedMachines,
			Badges:          badges,
			RecentActivity:  activities,
			IsOwnProfile:    isAuth && currentUserID == profileUser.ID,
			IsAuthenticated: isAuth,
			IsAdmin:         isAdmin,
			ChartData:       chartData,
		}

		// ─── Template fonksiyonları ────────────────────────────────────
		funcMap := template.FuncMap{
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006")
			},
			"formatDateTime": func(t time.Time) string {
				return t.Format("02.01.2006 15:04")
			},
			"truncate": func(s string, n int) string {
				if len(s) > n {
					return s[:n] + "..."
				}
				return s
			},
			"first": func(n int, items interface{}) interface{} { // ← YENİ: first fonksiyonu eklendi
				// items bir slice olmalı
				switch v := items.(type) {
				case []models.Badge:
					if n > len(v) {
						n = len(v)
					}
					return v[:n]
				case []interface{}:
					if n > len(v) {
						n = len(v)
					}
					return v[:n]
				default:
					return items
				}
			},
			"percentage": func(solved, total int) int {
				if total == 0 {
					return 0
				}
				return solved * 100 / total
			},
			"add": func(a, b int) int { return a + b },
			"difficultyLabel": func(d string) string {
				switch d {
				case "easy":
					return "Kolay"
				case "medium":
					return "Orta"
				case "hard":
					return "Zor"
				case "expert":
					return "Uzman"
				}
				return d
			},
			"actionIcon": func(actionType string) string {
				switch actionType {
				case "solve":
					return "fa-flag"
				case "badge":
					return "fa-medal"
				case "comment":
					return "fa-comment"
				case "follow":
					return "fa-user-plus"
				case "login":
					return "fa-sign-in-alt"
				}
				return "fa-circle"
			},
			"actionTitle": func(a models.Activity) string {
				switch a.ActionType {
				case "solve":
					return a.MachineName + " çözüldü"
				case "badge":
					return "Rozet kazanıldı"
				case "comment":
					return a.MachineName + " hakkında yorum yapıldı"
				case "login":
					return "Giriş yapıldı"
				}
				return a.ActionType
			},
		}

		tmpl, err := template.New("profile.html").Funcs(funcMap).ParseFiles(
			"templates/profile.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Sayfa yüklenemedi", http.StatusInternalServerError)
			return
		}

		// Buffer ile güvenli render (superfluous WriteHeader önlenir)
		var buf bytes.Buffer
		if err = tmpl.Execute(&buf, data); err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
			http.Error(w, "Sayfa oluşturulamadı", http.StatusInternalServerError)
			return
		}
		buf.WriteTo(w)
	}
}



func calculateUserStats(db *sql.DB, userID int) models.ProfileStats {
	stats := models.ProfileStats{}

	// Toplam çözülen soru sayısı ve puan
	err := db.QueryRow(`
		SELECT 
			COUNT(DISTINCT us.question_id),
			COALESCE(SUM(mq.points_reward), 0)
		FROM user_solutions us
		LEFT JOIN machine_questions mq ON us.question_id = mq.id
		WHERE us.user_id = $1
	`, userID).Scan(&stats.TotalQuestions, &stats.TotalPoints)
	if err != nil {
		log.Printf("Stats sorgu hatası: %v", err)
	}

	// Çözülen makine sayısı
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT machine_id) FROM user_solutions WHERE user_id = $1
	`, userID).Scan(&stats.TotalMachines)
	if err != nil {
		log.Printf("MachinesSolved sorgu hatası: %v", err)
	}

	// Sıralama
	err = db.QueryRow(`
		SELECT COUNT(*) + 1 FROM users 
		WHERE points > (SELECT points FROM users WHERE id = $1) AND is_active = true
	`, userID).Scan(&stats.Rank)
	if err != nil {
		log.Printf("Rank sorgu hatası: %v", err)
		stats.Rank = 0
	}

	// VIP makine sayısı
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT us.machine_id)
		FROM user_solutions us
		JOIN machines m ON us.machine_id = m.id
		WHERE us.user_id = $1 AND m.is_vip_only = true
	`, userID).Scan(&stats.VIPCount)
	if err != nil {
		log.Printf("VIPCount sorgu hatası: %v", err)
	}

	// Başarı yüzdesi: (çözülen soru / toplam aktif soru) * 100
	var totalQuestions int
	err = db.QueryRow(`SELECT COUNT(*) FROM machine_questions WHERE is_active = true`).Scan(&totalQuestions)
	if err == nil && totalQuestions > 0 {
		stats.Accuracy = float64(stats.TotalQuestions) * 100.0 / float64(totalQuestions)
		if stats.Accuracy > 100 {
			stats.Accuracy = 100
		}
	}

	// Streak (son 7 gün)
	err = db.QueryRow(`
		WITH daily_solves AS (
			SELECT DISTINCT DATE(solved_at) as solve_day
			FROM user_solutions
			WHERE user_id = $1
			ORDER BY solve_day DESC
		)
		SELECT COUNT(*) FROM daily_solves
		WHERE solve_day > CURRENT_DATE - INTERVAL '7 days'
	`, userID).Scan(&stats.Streak)
	if err != nil {
		log.Printf("Streak sorgu hatası: %v", err)
	}

	return stats
}

// getUserSolvedMachines - kullanıcının çözdüğü makineleri getir
func getUserSolvedMachines(db *sql.DB, userID int, limit int) ([]models.SolvedMachine, error) {
	rows, err := db.Query(`
		SELECT DISTINCT ON (m.id)
			m.id, m.name, m.difficulty,
			COALESCE(SUM(mq.points_reward) OVER (PARTITION BY m.id), 0) as points,
			MAX(us.solved_at) OVER (PARTITION BY m.id) as solved_at
		FROM user_solutions us
		JOIN machines m ON us.machine_id = m.id
		JOIN machine_questions mq ON us.question_id = mq.id
		WHERE us.user_id = $1
			AND NOT EXISTS (
				SELECT 1 FROM machine_questions mq2
				WHERE mq2.machine_id = m.id AND mq2.is_active = true
				AND NOT EXISTS (
					SELECT 1 FROM user_solutions us2
					WHERE us2.user_id = $1 AND us2.question_id = mq2.id
				)
			)
		ORDER BY m.id, solved_at DESC
		LIMIT $2
	`, userID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machines []models.SolvedMachine
	for rows.Next() {
		var sm models.SolvedMachine
		err := rows.Scan(&sm.ID, &sm.Name, &sm.Difficulty, &sm.Points, &sm.SolvedAt)
		if err != nil {
			log.Printf("SolvedMachine scan hatası: %v", err)
			continue
		}
		machines = append(machines, sm)
	}
	return machines, nil
}

// getUserBadges - kullanıcının rozetlerini getir
func getUserBadges(db *sql.DB, userID int) ([]models.Badge, error) {
	rows, err := db.Query(`
		SELECT 
			a.id, a.name, COALESCE(a.description, ''), 
			COALESCE(a.icon, 'fa-medal'), a.points_reward,
			ua.earned_at
		FROM user_achievements ua
		JOIN achievements a ON ua.achievement_id = a.id
		WHERE ua.user_id = $1
		ORDER BY ua.earned_at DESC
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var badges []models.Badge
	for rows.Next() {
		var b models.Badge
		err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.Icon, &b.PointsReward, &b.EarnedAt)
		if err != nil {
			log.Printf("Badge scan hatası: %v", err)
			continue
		}
		badges = append(badges, b)
	}
	return badges, nil
}

// getUserRecentActivities - son aktiviteleri getir
func getUserRecentActivities(db *sql.DB, userID int, limit int) ([]models.Activity, error) {
	rows, err := db.Query(`
		SELECT 
			al.id, al.user_id, al.action_type,
			COALESCE(al.machine_id, 0),
			COALESCE(al.question_id, 0),
			al.created_at,
			COALESCE(m.name, '') as machine_name,
			COALESCE(mq.title, '') as question_title
		FROM activity_logs al
		LEFT JOIN machines m ON al.machine_id = m.id
		LEFT JOIN machine_questions mq ON al.question_id = mq.id
		WHERE al.user_id = $1
		ORDER BY al.created_at DESC
		LIMIT $2
	`, userID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []models.Activity
	for rows.Next() {
		var a models.Activity
		err := rows.Scan(
			&a.ID, &a.UserID, &a.ActionType,
			&a.MachineID, &a.QuestionID, &a.CreatedAt,
			&a.MachineName, &a.QuestionTitle,
		)
		if err != nil {
			log.Printf("Activity scan hatası: %v", err)
			continue
		}
		activities = append(activities, a)
	}
	return activities, nil
}

// getWeeklyChartData - haftalık aktivite grafik verisi
func getWeeklyChartData(db *sql.DB, userID int) [7]int {
	var data [7]int
	rows, err := db.Query(`
		SELECT EXTRACT(DOW FROM solved_at)::int as day, COUNT(*)::int as count
		FROM user_solutions
		WHERE user_id = $1 AND solved_at > NOW() - INTERVAL '7 days'
		GROUP BY EXTRACT(DOW FROM solved_at)
	`, userID)
	if err != nil {
		return data
	}
	defer rows.Close()
	for rows.Next() {
		var day, count int
		if err := rows.Scan(&day, &count); err == nil {
			index := (day + 6) % 7
			data[index] = count
		}
	}
	return data
}
