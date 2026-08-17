// handlers/leaderboard.go
package handlers

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"

	"ctf-platform/models"

	"github.com/gorilla/sessions"
)

type LeaderboardData struct {
	Title           string
	User            *models.User
	IsAuthenticated bool
	Entries         []models.LeaderboardEntry
	Stats           LeaderboardStats
	UserRank        int
}

type LeaderboardStats struct {
	TotalUsers         int
	TotalSolutions     int
	TotalPoints        int
	FastestRising      string // En hızlı yükselen kullanıcı adı
	FastestRisingDelta int    // Kaç sıra yükseldi
	MostVIPUser        string // En çok VIP çözen kullanıcı
	MostVIPCount       int    // Kaç VIP makine çözdü
	Last24hSolutions   int    // Son 24 saatte çözüm sayısı
	NewUsersToday      int    // Bugün kayıt olan kullanıcı sayısı
}

type LeaderboardFilters struct {
	Timeframe string
	Country   string
	SortBy    string
	Page      int
	Limit     int
}

func GetLeaderboard(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		timeframe := r.URL.Query().Get("timeframe")
		country := r.URL.Query().Get("country")
		sortBy := r.URL.Query().Get("sort")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 50
		}

		// Ana sorgu
		query := `
            SELECT 
                u.id,
                u.username,
                COALESCE(u.avatar, '') as avatar,
                COALESCE(u.country, '') as country,
                u.points,
                u.is_vip,
                COUNT(DISTINCT us.machine_id) as machines_solved,
                COUNT(DISTINCT us.question_id) as questions_solved,
                COALESCE(ROUND(COUNT(DISTINCT us.question_id) * 100.0 / 
                    NULLIF((SELECT COUNT(*) FROM machine_questions mq WHERE mq.machine_id IN 
                        (SELECT id FROM machines)), 0)), 0) as accuracy
            FROM users u
            LEFT JOIN user_solutions us ON u.id = us.user_id
            WHERE u.is_active = true
        `

		var args []interface{}
		argCount := 1

		// Ülke filtresi
		if country != "" && country != "all" {
			query += ` AND u.country = $` + strconv.Itoa(argCount)
			args = append(args, country)
			argCount++
		}

		// Zaman filtresi
		if timeframe != "" && timeframe != "all" {
			switch timeframe {
			case "today":
				query += ` AND us.solved_at > NOW() - INTERVAL '1 day'`
			case "week":
				query += ` AND us.solved_at > NOW() - INTERVAL '7 days'`
			case "month":
				query += ` AND us.solved_at > NOW() - INTERVAL '30 days'`
			case "year":
				query += ` AND us.solved_at > NOW() - INTERVAL '1 year'`
			}
		}

		query += ` GROUP BY u.id, u.username, u.avatar, u.country, u.points, u.is_vip`

		// Sıralama
		switch sortBy {
		case "points":
			query += ` ORDER BY u.points DESC`
		case "solved":
			query += ` ORDER BY questions_solved DESC`
		case "accuracy":
			query += ` ORDER BY accuracy DESC`
		default:
			query += ` ORDER BY u.points DESC`
		}

		// Sayfalama
		query += ` LIMIT $` + strconv.Itoa(argCount) + ` OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		rows, err := db.Query(query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Liderlik tablosu getirilemedi: "+err.Error())
			return
		}
		defer rows.Close()

		var entries []models.LeaderboardEntry
		for rows.Next() {
			var e models.LeaderboardEntry
			err := rows.Scan(
				&e.ID, &e.Username, &e.Avatar, &e.Country,
				&e.Points, &e.IsVIP,
				&e.MachinesSolved, &e.QuestionsSolved, &e.Accuracy,
			)
			if err != nil {
				continue
			}
			entries = append(entries, e)
		}

		// Rank'leri hesapla (sayfaya göre)
		for i := range entries {
			entries[i].Rank = (page-1)*limit + i + 1
		}

		// Toplam kullanıcı sayısı (filtrelere göre)
		var totalUsers int
		countQuery := `SELECT COUNT(*) FROM users WHERE is_active = true`
		var countArgs []interface{}

		if country != "" && country != "all" {
			countQuery += ` AND country = $1`
			countArgs = append(countArgs, country)
		}

		if err := db.QueryRow(countQuery, countArgs...).Scan(&totalUsers); err != nil {
			writeError(w, http.StatusInternalServerError, "Toplam kullanıcı sayısı alınamadı")
			return
		}

		// Toplam çözüm sayısı (filtrelere göre)
		var totalSolutions int
		solutionsQuery := `SELECT COUNT(*) FROM user_solutions`
		if timeframe != "" && timeframe != "all" {
			switch timeframe {
			case "today":
				solutionsQuery += ` WHERE solved_at > NOW() - INTERVAL '1 day'`
			case "week":
				solutionsQuery += ` WHERE solved_at > NOW() - INTERVAL '7 days'`
			case "month":
				solutionsQuery += ` WHERE solved_at > NOW() - INTERVAL '30 days'`
			case "year":
				solutionsQuery += ` WHERE solved_at > NOW() - INTERVAL '1 year'`
			}
		}
		if err := db.QueryRow(solutionsQuery).Scan(&totalSolutions); err != nil {
			totalSolutions = 0
		}

		// Toplam puan
		var totalPoints int
		if err := db.QueryRow("SELECT COALESCE(SUM(points), 0) FROM users WHERE is_active = true").Scan(&totalPoints); err != nil {
			totalPoints = 0
		}

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"entries":     entries,
			"total":       totalUsers,
			"page":        page,
			"limit":       limit,
			"total_pages": (totalUsers + limit - 1) / limit,
			"stats": map[string]interface{}{
				"total_users":     totalUsers,
				"total_solutions": totalSolutions,
				"total_points":    totalPoints,
			},
		})
	}
}

func LeaderboardPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			User    *models.User
			IsAdmin bool
			Title   string
			Entries []models.LeaderboardEntry
			Stats   LeaderboardStats
		}{
			IsAdmin: false,
			Title:   "Liderlik Tablosu - CTF HACK PLATFORMU",
		}

		// Session kontrolü (HomePage ile aynı pattern)
		session, _ := store.Get(r, "session")
		if userID, ok := session.Values["user_id"]; ok {
			var user models.User
			err := db.QueryRow(`SELECT id, username, email, points, is_vip FROM users WHERE id = $1`, userID).Scan(
				&user.ID, &user.Username, &user.Email, &user.Points, &user.IsVIP,
			)
			if err == nil {
				data.User = &user

				var isAdmin bool
				err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, userID).Scan(&isAdmin)
				if err == nil {
					data.IsAdmin = isAdmin
				}
			}
		}

		// İlk 100 kullanıcıyı getir
		rows, err := db.Query(`
            SELECT 
                u.id,
                u.username,
                u.avatar,
                u.country,
                u.points,
                u.rank,
                u.is_vip,
                COUNT(DISTINCT us.machine_id) as machines_solved,
                ROUND(AVG(CASE WHEN us.id IS NOT NULL THEN 100 ELSE 0 END)) as accuracy
            FROM users u
            LEFT JOIN user_solutions us ON u.id = us.user_id
            WHERE u.is_active = true
            GROUP BY u.id
            ORDER BY u.points DESC
            LIMIT 100
        `)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var e models.LeaderboardEntry
			rows.Scan(
				&e.ID, &e.Username, &e.Avatar, &e.Country,
				&e.Points, &e.Rank, &e.IsVIP,
				&e.MachinesSolved, &e.Accuracy,
			)
			data.Entries = append(data.Entries, e)
		}

		// İstatistikler
		db.QueryRow("SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&data.Stats.TotalUsers)
		db.QueryRow("SELECT COUNT(*) FROM user_solutions").Scan(&data.Stats.TotalSolutions)
		db.QueryRow("SELECT COALESCE(SUM(points), 0) FROM users").Scan(&data.Stats.TotalPoints)

		db.QueryRow(`
    SELECT u.username, COALESCE(COUNT(us.id), 0) as recent_solves
    FROM users u
    LEFT JOIN user_solutions us ON u.id = us.user_id
        AND us.solved_at > NOW() - INTERVAL '7 days'
    WHERE u.is_active = true
    GROUP BY u.id, u.username
    ORDER BY recent_solves DESC
    LIMIT 1
`).Scan(&data.Stats.FastestRising, &data.Stats.FastestRisingDelta)

		db.QueryRow(`
    SELECT u.username, COUNT(DISTINCT us.machine_id) as vip_count
    FROM users u
    JOIN user_solutions us ON u.id = us.user_id
    JOIN machines m ON us.machine_id = m.id AND m.is_vip_only = true
    WHERE u.is_active = true
    GROUP BY u.id, u.username
    ORDER BY vip_count DESC
    LIMIT 1
`).Scan(&data.Stats.MostVIPUser, &data.Stats.MostVIPCount)

		db.QueryRow(`
    SELECT COUNT(*) FROM user_solutions
    WHERE solved_at > NOW() - INTERVAL '24 hours'
`).Scan(&data.Stats.Last24hSolutions)

		// Bugün kayıt olan kullanıcı sayısı
		db.QueryRow(`
    SELECT COUNT(*) FROM users
    WHERE DATE(created_at) = CURRENT_DATE AND is_active = true
`).Scan(&data.Stats.NewUsersToday)

		tmpl, err := template.ParseFiles("templates/leaderboard.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, data)
	}
}
