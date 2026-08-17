// handlers/machines.go
package handlers

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"ctf-platform/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

type MachineFilter struct {
	Difficulty string
	Status     string
	Access     string
	Search     string
	Sort       string
	Page       int
	Limit      int
}

// API: /api/machines - Tüm makineleri getir
func APIGetMachines(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query(`
            SELECT 
                m.id, m.name, m.description, m.difficulty, m.points_reward,
                m.is_vip_only, m.created_at,
                COUNT(DISTINCT q.id) as question_count,
                COUNT(DISTINCT s.user_id) as solve_count,
                COALESCE(u.username, 'system') as creator
            FROM machines m
            LEFT JOIN machine_questions q ON m.id = q.machine_id AND q.is_active = true
            LEFT JOIN submissions s ON m.id = s.machine_id AND s.status = 'accepted'
            LEFT JOIN users u ON m.creator_id = u.id
            WHERE m.is_active = true
            GROUP BY m.id, u.username
            ORDER BY m.created_at DESC
        `)

		if err != nil {
			http.Error(w, `[]`, http.StatusInternalServerError) // Boş dizi döndür
			return
		}
		defer rows.Close()

		var machines []map[string]interface{}
		for rows.Next() {
			var id, points, qCount, solveCount int
			var name, description, difficulty, creator string
			var isVIP bool
			var createdAt time.Time

			err := rows.Scan(&id, &name, &description, &difficulty, &points, &isVIP, &createdAt, &qCount, &solveCount, &creator)
			if err != nil {
				continue
			}

			machines = append(machines, map[string]interface{}{
				"ID":            id,
				"Name":          name,
				"Description":   description,
				"Difficulty":    difficulty,
				"PointsReward":  points,
				"IsVIPOnly":     isVIP,
				"CreatedAt":     createdAt,
				"QuestionCount": qCount,
				"SolveCount":    solveCount,
				"Creator":       "@" + creator,
				"Tags":          []string{}, // Boş tags dizisi ekle
			})
		}

		// Eğer hiç makine yoksa boş dizi döndür
		if machines == nil {
			machines = make([]map[string]interface{}, 0)
		}

		json.NewEncoder(w).Encode(machines)
	}
}

// API: /api/user/progress - Kullanıcı ilerlemesi
func APIUserProgress(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		session, _ := store.Get(r, "session")
		auth, ok := session.Values["authenticated"].(bool)
		if !ok || !auth {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID := session.Values["user_id"].(int)

		rows, err := db.Query(`
            SELECT machine_id, COUNT(*) as solved_count
            FROM user_solutions
            WHERE user_id = $1
            GROUP BY machine_id
        `, userID)

		if err != nil {
			http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		solvedMachines := make(map[int]int)
		for rows.Next() {
			var machineID, solvedCount int
			rows.Scan(&machineID, &solvedCount)
			solvedMachines[machineID] = solvedCount
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"solvedMachines": solvedMachines,
		})
	}
}

type MachinesPageData struct {
	IsAdmin         bool
	Title           string
	User            *models.User
	IsAuthenticated bool
	UserStats       UserStats
	TotalMachines   int
	CurrentYear     int
}

type UserStats struct {
	SolvedCount int
	TotalPoints int
	Rank        int
}

func MachinesPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		isAuth := false
		var user *models.User
		var userStats UserStats

		// Toplam makine sayısı
		var totalMachines int
		db.QueryRow(`SELECT COUNT(*) FROM machines WHERE is_active = true`).Scan(&totalMachines)

		var isAdmin bool
		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			userID := session.Values["user_id"].(int)

			user = &models.User{}
			db.QueryRow(`
                SELECT id, username, email, is_vip, points, avatar 
                FROM users WHERE id = $1
            `, userID).Scan(&user.ID, &user.Username, &user.Email, &user.IsVIP, &user.Points, &user.Avatar)

			// Kullanıcı istatistikleri
			db.QueryRow(`
                SELECT COUNT(DISTINCT machine_id)
                FROM user_solutions 
                WHERE user_id = $1
            `, userID).Scan(&userStats.SolvedCount)

			db.QueryRow(`
                SELECT COUNT(*) + 1 FROM users 
                WHERE points > (SELECT points FROM users WHERE id = $1)
            `, userID).Scan(&userStats.Rank)

			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, userID).Scan(&isAdmin)

		}

		data := MachinesPageData{
			Title:           "Makineler - CTF HACK PLATFORMU",
			User:            user,
			IsAdmin:         isAdmin,
			IsAuthenticated: isAuth,
			UserStats:       userStats,
			TotalMachines:   totalMachines,
			CurrentYear:     time.Now().Year(),
		}

		tmpl, err := template.ParseFiles("templates/machines.html")
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Template çalıştırılamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// AdminGetMachine - Tekil makine getir (GET /admin/api/machines/{id})
func AdminGetMachine(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		w.Header().Set("Content-Type", "application/json")

		var machine struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			Difficulty   string `json:"difficulty"`
			PointsReward int    `json:"points_reward"`
			IsVIPOnly    bool   `json:"is_vip_only"`
			IsActive     bool   `json:"is_active"`
			DockerImage  string `json:"docker_image"`
			ImageURL     string `json:"image_url"`
		}

		err := db.QueryRow(`
            SELECT id, name, description, difficulty, points_reward, 
                   is_vip_only, is_active, docker_image, image_url
            FROM machines 
            WHERE id = $1
        `, id).Scan(
			&machine.ID, &machine.Name, &machine.Description, &machine.Difficulty,
			&machine.PointsReward, &machine.IsVIPOnly, &machine.IsActive,
			&machine.DockerImage, &machine.ImageURL,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error": "Makine bulunamadı"}`, http.StatusNotFound)
			} else {
				http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
			}
			return
		}

		json.NewEncoder(w).Encode(machine)
	}
}
