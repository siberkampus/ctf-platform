// handlers/home.go
package handlers

import (
	"ctf-platform/models"
)

type HomePageData struct {
	Title            string
	User             *models.User
	IsAuthenticated  bool
	Stats            HomeStats
	FeaturedMachines []models.Machine
}

type HomeStats struct {
	TotalMachines  int
	TotalSolutions int
	TotalUsers     int
	ActiveUsers    int
}

// func HomePage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
//     return func(w http.ResponseWriter, r *http.Request) {
//         // Session kontrolü
//         session, _ := store.Get(r, "session")
//         isAuth := false
//         var user *models.User

//         if auth, ok := session.Values["authenticated"].(bool); ok && auth {
//             isAuth = true
//             userID := session.Values["user_id"].(int)

//             // Kullanıcı bilgilerini getir
//             user = &models.User{}
//             db.QueryRow(`
//                 SELECT id, username, email, is_vip, points
//                 FROM users WHERE id = $1
//             `, userID).Scan(&user.ID, &user.Username, &user.Email, &user.IsVIP, &user.Points)
//         }

//         // İstatistikleri getir
//         var stats HomeStats
//         db.QueryRow("SELECT COUNT(*) FROM machines WHERE is_active = true").Scan(&stats.TotalMachines)
//         db.QueryRow("SELECT COUNT(*) FROM user_solutions").Scan(&stats.TotalSolutions)
//         db.QueryRow("SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&stats.TotalUsers)
//         db.QueryRow("SELECT COUNT(*) FROM users WHERE last_login > NOW() - INTERVAL '24 hours'").Scan(&stats.ActiveUsers)

//         // Öne çıkan makineleri getir
//         rows, err := db.Query(`
//             SELECT m.id, m.name, m.description, m.difficulty, m.points_reward,
//                    COUNT(DISTINCT us.user_id) as solver_count
//             FROM machines m
//             LEFT JOIN user_solutions us ON m.id = us.machine_id
//             WHERE m.is_active = true
//             GROUP BY m.id
//             ORDER BY solver_count DESC
//             LIMIT 6
//         `)
//         if err != nil {
//             http.Error(w, "Veri getirilemedi", http.StatusInternalServerError)
//             return
//         }
//         defer rows.Close()

//         var featuredMachines []models.Machine
//         for rows.Next() {
//             var m models.Machine
//             rows.Scan(&m.ID, &m.Name, &m.Description, &m.Difficulty, &m.PointsReward, &m.SolverCount)
//             featuredMachines = append(featuredMachines, m)
//         }

//         data := HomePageData{
//             Title:           "CTF HACK PLATFORMU",
//             User:            user,
//             IsAuthenticated: isAuth,
//             Stats:           stats,
//             FeaturedMachines: featuredMachines,
//         }

//         tmpl := template.Must(template.ParseFiles("templates/index.html"))
//         tmpl.Execute(w, data)
//     }
// }
