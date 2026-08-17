// handlers/pages.go
package handlers

import (
	"ctf-platform/models"
	"database/sql"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/sessions"
)

func HomePage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Template verileri için struct
		data := struct {
			User           *models.User
			IsAdmin        bool
			Stats          map[string]interface{}
			Machines       []map[string]interface{}
			Leaderboard    []map[string]interface{}
			SolvedMachines map[int]bool
			RandomIP       string
			CurrentYear    int
		}{
			CurrentYear:    time.Now().Year(),
			RandomIP:       fmt.Sprintf("10.10.10.%d", rand.Intn(254)+1),
			SolvedMachines: make(map[int]bool),
			IsAdmin:        false,
		}

		// Session kontrolü
		session, _ := store.Get(r, "session")
		if userID, ok := session.Values["user_id"]; ok {
			var user models.User
			err := db.QueryRow(`SELECT id, username, email, points, is_vip FROM users WHERE id = $1`, userID).Scan(
				&user.ID, &user.Username, &user.Email, &user.Points, &user.IsVIP,
			)
			if err == nil {
				data.User = &user

				// Admin kontrolü - admins tablosunda var mı?
				var isAdmin bool
				err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, userID).Scan(&isAdmin)
				if err == nil {
					data.IsAdmin = isAdmin
				}

				// Kullanıcının çözdüğü makineleri al
				rows, _ := db.Query(`
            SELECT DISTINCT machine_id FROM user_solutions WHERE user_id = $1
        `, user.ID)
				defer rows.Close()
				for rows.Next() {
					var machineID int
					rows.Scan(&machineID)
					data.SolvedMachines[machineID] = true
				}
			}
		}
		// İstatistikler
		stats := make(map[string]interface{})
		var totalUsers, totalMachines, totalQuestions int
		db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)
		db.QueryRow(`SELECT COUNT(*) FROM machines WHERE is_active = true`).Scan(&totalMachines)
		db.QueryRow(`SELECT COUNT(*) FROM machine_questions WHERE is_active = true`).Scan(&totalQuestions)
		stats["TotalUsers"] = totalUsers
		stats["TotalMachines"] = totalMachines
		stats["TotalQuestions"] = totalQuestions
		data.Stats = stats

		// Aktif makineler
		rows, err := db.Query(`
            SELECT 
                m.id, m.name, m.description, m.difficulty, m.points_reward,
                m.is_vip_only, m.is_active,
                COUNT(DISTINCT q.id) as question_count,
                COUNT(DISTINCT s.user_id) as solve_count
            FROM machines m
            LEFT JOIN machine_questions q ON m.id = q.machine_id AND q.is_active = true
            LEFT JOIN submissions s ON m.id = s.machine_id AND s.status = 'accepted'
            WHERE m.is_active = true
            GROUP BY m.id
            ORDER BY m.created_at DESC
            LIMIT 6
        `)

		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var machine struct {
					ID            int
					Name          string
					Description   string
					Difficulty    string
					PointsReward  int
					IsVIPOnly     bool
					IsActive      bool
					QuestionCount int
					SolveCount    int
				}
				rows.Scan(
					&machine.ID, &machine.Name, &machine.Description, &machine.Difficulty,
					&machine.PointsReward, &machine.IsVIPOnly, &machine.IsActive,
					&machine.QuestionCount, &machine.SolveCount,
				)

				data.Machines = append(data.Machines, map[string]interface{}{
					"ID":            machine.ID,
					"Name":          machine.Name,
					"Description":   machine.Description,
					"Difficulty":    machine.Difficulty,
					"PointsReward":  machine.PointsReward,
					"IsVIPOnly":     machine.IsVIPOnly,
					"QuestionCount": machine.QuestionCount,
					"SolveCount":    machine.SolveCount,
				})
			}
		}

		// Leaderboard
		rows, err = db.Query(`
            SELECT 
                u.id, u.username, u.points, u.is_vip,
                COUNT(DISTINCT s.question_id) as solved_count
            FROM users u
            LEFT JOIN submissions s ON u.id = s.user_id AND s.status = 'accepted'
            GROUP BY u.id
            ORDER BY u.points DESC
            LIMIT 10
        `)

		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var user struct {
					ID          int
					Username    string
					Points      int
					IsVIP       bool
					SolvedCount int
				}
				rows.Scan(&user.ID, &user.Username, &user.Points, &user.IsVIP, &user.SolvedCount)

				data.Leaderboard = append(data.Leaderboard, map[string]interface{}{
					"ID":          user.ID,
					"Username":    user.Username,
					"Points":      user.Points,
					"IsVIP":       user.IsVIP,
					"SolvedCount": user.SolvedCount,
				})
			}
		}

		// Template'i render et
		tmpl, err := template.New("index.html").Funcs(template.FuncMap{
			"add": func(a, b int) int { return a + b },
			"formatNumber": func(n int) string {
				if n >= 1000 {
					return fmt.Sprintf("%.1fk", float64(n)/1000)
				}
				return strconv.Itoa(n)
			},
		}).ParseFiles("templates/index.html")

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, data)
	}
}

func LoginPage(store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}

		data := map[string]interface{}{
			"Title": "Giriş Yap - CTF HACK PLATFORMU",
		}

		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, data)
	}
}

func RegisterPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := map[string]interface{}{
			"Title": "Kayıt Ol - CTF HACK PLATFORMU",
		}

		tmpl := template.Must(template.ParseFiles("templates/register.html"))
		tmpl.Execute(w, data)
	}
}
