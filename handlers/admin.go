// handlers/admin.go
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ctf-platform/models"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func calculatePercentage(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// ==================== ADMIN HTML SAYFALARI ====================

// AdminLoginPage - Admin giriş sayfası
func AdminLoginPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "Admin Giriş - CTF Platform",
	}

	tmpl := template.Must(template.ParseFiles("templates/admin/admin_login.html"))
	tmpl.Execute(w, data)
}

// AdminDashboard - Admin dashboard sayfası
func AdminDashboard(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		var stats models.AdminStats

		// Toplam kullanıcı (aktif olanlar)
		err := db.QueryRow("SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&stats.TotalUsers)
		if err != nil {
			log.Printf("Toplam kullanıcı sorgu hatası: %v", err)
		}

		// Bugün kaydolanlar
		err = db.QueryRow(`
            SELECT COUNT(*) FROM users 
            WHERE DATE(created_at) = CURRENT_DATE
        `).Scan(&stats.NewUsersToday)
		if err != nil {
			log.Printf("Yeni kullanıcı sorgu hatası: %v", err)
		}

		// Aktif kullanıcılar (son 24 saat)
		err = db.QueryRow(`
            SELECT COUNT(*) FROM users 
            WHERE last_login > NOW() - INTERVAL '24 hours'
        `).Scan(&stats.ActiveUsers)
		if err != nil {
			log.Printf("Aktif kullanıcı sorgu hatası: %v", err)
		}

		// Toplam makine (aktif olanlar)
		err = db.QueryRow("SELECT COUNT(*) FROM machines WHERE is_active = true").Scan(&stats.TotalMachines)
		if err != nil {
			log.Printf("Toplam makine sorgu hatası: %v", err)
		}

		// Toplam çözüm (tüm submission'lar)
		err = db.QueryRow("SELECT COUNT(*) FROM submissions").Scan(&stats.TotalSubmissions)
		if err != nil {
			log.Printf("Toplam submission sorgu hatası: %v", err)
		}

		// Bugünkü çözümler
		err = db.QueryRow(`
            SELECT COUNT(*) FROM submissions 
            WHERE DATE(created_at) = CURRENT_DATE
        `).Scan(&stats.SubmissionsToday)
		if err != nil {
			log.Printf("Bugünkü submission sorgu hatası: %v", err)
		}

		// Toplam VIP kullanıcı
		err = db.QueryRow("SELECT COUNT(*) FROM users WHERE is_vip = true").Scan(&stats.TotalVIPUsers)
		if err != nil {
			log.Printf("VIP kullanıcı sorgu hatası: %v", err)
		}

		// VIP geliri (bugün)
		err = db.QueryRow(`
            SELECT COALESCE(SUM(price), 0) FROM vip_purchases 
            WHERE DATE(purchased_at) = CURRENT_DATE
        `).Scan(&stats.VIPRevenue)
		if err != nil {
			log.Printf("VIP gelir sorgu hatası: %v", err)
		}

		// Ortalama puan
		err = db.QueryRow("SELECT COALESCE(AVG(points), 0) FROM users").Scan(&stats.AveragePoints)
		if err != nil {
			log.Printf("Ortalama puan sorgu hatası: %v", err)
		}

		// En yüksek puan
		err = db.QueryRow("SELECT COALESCE(MAX(points), 0) FROM users").Scan(&stats.TopUserPoints)
		if err != nil {
			log.Printf("En yüksek puan sorgu hatası: %v", err)
		}

		// Başarı oranı
		var total, success int
		err = db.QueryRow("SELECT COUNT(*) FROM submissions").Scan(&total)
		if err != nil {
			log.Printf("Toplam submission sayısı sorgu hatası: %v", err)
		}

		err = db.QueryRow("SELECT COUNT(*) FROM submissions WHERE status = 'accepted'").Scan(&success)
		if err != nil {
			log.Printf("Başarılı submission sorgu hatası: %v", err)
		}

		if total > 0 {
			stats.SuccessRate = float64(success) / float64(total) * 100
		}

		// Son kullanıcılar
		rows, err := db.Query(`
            SELECT id, username, email,COALESCE(NULLIF(avatar, ''), '/static/images/avatar.png'), points, is_vip, created_at 
            FROM users 
            WHERE is_active = true 
            ORDER BY created_at DESC 
            LIMIT 10
        `)
		if err != nil {
			log.Printf("Son kullanıcılar sorgu hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var recentUsers []models.User
		for rows.Next() {
			var u models.User
			var createdAt time.Time

			err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Avatar, &u.Points, &u.IsVIP, &createdAt)
			if err != nil {
				log.Printf("Kullanıcı satır okuma hatası: %v", err)
				continue
			}
			u.CreatedAt = createdAt
			recentUsers = append(recentUsers, u)
		}

		// --- recentSubmissions TANIMLANIYOR ---
		var recentSubmissions []models.Submission
		rows, err = db.Query(`
            SELECT s.id, u.username, m.name, mq.title, s.status, s.created_at
            FROM submissions s
            JOIN users u ON s.user_id = u.id
            JOIN machines m ON s.machine_id = m.id
            JOIN machine_questions mq ON s.question_id = mq.id
            ORDER BY s.created_at DESC
            LIMIT 10
        `)
		if err != nil {
			log.Printf("Son çözümler sorgu hatası: %v", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var s models.Submission
				var submittedAt time.Time

				err := rows.Scan(&s.ID, &s.Username, &s.MachineName, &s.QuestionTitle, &s.Status, &submittedAt)
				if err != nil {
					log.Printf("Submission satır okuma hatası: %v", err)
					continue
				}
				s.CreatedAt = submittedAt
				recentSubmissions = append(recentSubmissions, s)
			}
		}

		// --- popularMachines TANIMLANIYOR ---
		var popularMachines []models.PopularMachine
		rows, err = db.Query(`
            SELECT m.id, m.name, m.difficulty, 
                   COUNT(s.id) as submissions,
                   COALESCE(ROUND(AVG(CASE WHEN s.status = 'accepted' THEN 100 ELSE 0 END)), 0) as success_rate
            FROM machines m
            LEFT JOIN submissions s ON m.id = s.machine_id
            WHERE m.is_active = true
            GROUP BY m.id, m.name, m.difficulty
            ORDER BY submissions DESC
            LIMIT 5
        `)
		if err != nil {
			log.Printf("Popüler makineler sorgu hatası: %v", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var pm models.PopularMachine

				err := rows.Scan(&pm.ID, &pm.Name, &pm.Difficulty, &pm.Submissions, &pm.SuccessRate)
				if err != nil {
					log.Printf("Popüler makine satır okuma hatası: %v", err)
					continue
				}
				popularMachines = append(popularMachines, pm)
			}
		}

		// Sistem sağlığı (mock veri)
		systemHealth := models.SystemHealth{
			Status:              "healthy",
			CPUUsage:            23.5,
			MemoryUsage:         45.2,
			DiskUsage:           67.8,
			ActiveContainers:    12,
			DatabaseConnections: 5,
		}

		// Haftalık aktivite grafiği için veri
		chartData := models.ChartData{
			Labels: []string{"Pzt", "Sal", "Çar", "Per", "Cum", "Cmt", "Paz"},
			Datasets: []models.ChartDataset{
				{
					Label: "Çözümler",
					Data:  []int{0, 0, 0, 0, 0, 0, 0},
				},
			},
		}

		// Son 7 günlük submission verilerini çek
		rows, err = db.Query(`
            SELECT EXTRACT(DOW FROM created_at)::int as day, COUNT(*)::int as count
            FROM submissions
            WHERE created_at > NOW() - INTERVAL '7 days'
            GROUP BY EXTRACT(DOW FROM created_at)
        `)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var day int
				var count int
				err := rows.Scan(&day, &count)
				if err == nil {
					// PostgreSQL'de Pazar=0, Pazartesi=1, ...
					index := (day + 6) % 7 // Pazartesi=0, Pazar=6
					if index >= 0 && index < 7 {
						chartData.Datasets[0].Data[index] = count
					}
				}
			}
		} else {
			log.Printf("Grafik verisi sorgu hatası: %v", err)
		}

		// Admin bilgilerini al
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Admin avatarını veritabanından al
		var avatar sql.NullString
		err = db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		// --- TÜM DEĞİŞKENLER ARTIK TANIMLI ---
		data := models.AdminDashboardData{
			Title:             "Admin Dashboard - CTF Platform",
			Stats:             stats,
			Active:            "dashboard",
			RecentUsers:       recentUsers,
			RecentSubmissions: recentSubmissions, // Tanımlandı
			PopularMachines:   popularMachines,   // Tanımlandı
			SystemHealth:      systemHealth,
			ActivityChart:     chartData,
			CurrentDate:       time.Now().Format("02 January 2006 - Monday"),
			ActivePercentage:  calculatePercentage(stats.ActiveUsers, stats.TotalUsers),
			Admin:             admin,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"now": func() time.Time {
				return time.Now()
			},
			"percentage": func(part, total int) float64 {
				if total == 0 {
					return 0
				}
				return float64(part) / float64(total) * 100
			},
			"subtract": func(a, b int) int {
				return a - b
			},
			"add": func(a, b int) int {
				return a + b
			},
			"multiply": func(a, b int) int {
				return a * b
			},
			"divide": func(a, b int) float64 {
				if b == 0 {
					return 0
				}
				return float64(a) / float64(b)
			},
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006 15:04")
			},
			"formatShortDate": func(t time.Time) string {
				return t.Format("02.01.2006")
			},
		}

		// Template'i parse et
		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/dashboard.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Template yüklenemedi", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template execute hatası: %v", err)
			http.Error(w, "Template çalıştırılamadı", http.StatusInternalServerError)
		}
	}
}

// AdminUsersPage - Kullanıcı listesi sayfası
func AdminUsersPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Filtre parametrelerini al
		status := r.URL.Query().Get("status")
		vip := r.URL.Query().Get("vip")
		sort := r.URL.Query().Get("sort")
		search := r.URL.Query().Get("search")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))

		if page < 1 {
			page = 1
		}
		limit := 20 // Sayfa başına gösterilecek kullanıcı sayısı

		// İstatistikleri getir
		var stats struct {
			TotalUsers  int
			ActiveUsers int
			VIPUsers    int
			NewToday    int
		}

		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
		db.QueryRow("SELECT COUNT(*) FROM users WHERE last_login > NOW() - INTERVAL '24 hours'").Scan(&stats.ActiveUsers)
		db.QueryRow("SELECT COUNT(*) FROM users WHERE is_vip = true").Scan(&stats.VIPUsers)
		db.QueryRow("SELECT COUNT(*) FROM users WHERE DATE(created_at) = CURRENT_DATE").Scan(&stats.NewToday)

		// Kullanıcı sorgusu
		query := `
			SELECT id, username, email, fullname, avatar, points, is_vip, is_active, created_at, last_login
			FROM users
			WHERE 1=1
		`
		var args []interface{}
		argCount := 1

		// Filtreleri uygula
		if status == "active" {
			query += ` AND is_active = true`
		} else if status == "inactive" {
			query += ` AND is_active = false`
		}

		if vip == "vip" {
			query += ` AND is_vip = true`
		} else if vip == "normal" {
			query += ` AND is_vip = false`
		}

		if search != "" {
			query += ` AND (username ILIKE $` + strconv.Itoa(argCount) +
				` OR email ILIKE $` + strconv.Itoa(argCount) +
				` OR fullname ILIKE $` + strconv.Itoa(argCount) + `)`
			args = append(args, "%"+search+"%")
			argCount++
		}

		// Sıralama
		switch sort {
		case "date_asc":
			query += ` ORDER BY created_at ASC`
		case "points_desc":
			query += ` ORDER BY points DESC`
		case "points_asc":
			query += ` ORDER BY points ASC`
		case "name_asc":
			query += ` ORDER BY username ASC`
		case "name_desc":
			query += ` ORDER BY username DESC`
		default: // date_desc
			query += ` ORDER BY created_at DESC`
		}

		// Toplam kullanıcı sayısını hesapla (filtreler uygulanmış)
		var totalUsers int
		countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count"
		err := db.QueryRow(countQuery, args...).Scan(&totalUsers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Sayfalama ekle
		query += ` LIMIT $` + strconv.Itoa(argCount) + ` OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		// Kullanıcıları getir
		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var users []map[string]interface{}
		for rows.Next() {
			var u struct {
				ID        int
				Username  string
				Email     string
				FullName  sql.NullString
				Avatar    sql.NullString
				Points    int
				IsVIP     bool
				IsActive  bool
				CreatedAt time.Time
				LastLogin sql.NullTime
			}
			err := rows.Scan(
				&u.ID, &u.Username, &u.Email, &u.FullName, &u.Avatar,
				&u.Points, &u.IsVIP, &u.IsActive, &u.CreatedAt, &u.LastLogin,
			)
			if err != nil {
				continue
			}

			// Avatar yoksa varsayılan kullan
			avatar := "/static/images/avatar.png"
			if u.Avatar.Valid {
				avatar = u.Avatar.String
			}

			users = append(users, map[string]interface{}{
				"ID":        u.ID,
				"Username":  u.Username,
				"Email":     u.Email,
				"FullName":  u.FullName.String,
				"Avatar":    avatar,
				"Points":    u.Points,
				"IsVIP":     u.IsVIP,
				"IsActive":  u.IsActive,
				"CreatedAt": u.CreatedAt,
				"LastLogin": u.LastLogin.Time,
			})
		}

		// Sayfalama hesapla
		totalPages := (totalUsers + limit - 1) / limit

		// Sayfa numaralarını oluştur
		var pages []int
		for i := 1; i <= totalPages; i++ {
			if i == 1 || i == totalPages || (i >= page-2 && i <= page+2) {
				pages = append(pages, i)
			}
		}

		pagination := struct {
			CurrentPage int
			TotalPages  int
			TotalItems  int
			HasPrev     bool
			HasNext     bool
			PrevPage    int
			NextPage    int
			Start       int
			End         int
			Pages       []int
		}{
			CurrentPage: page,
			TotalPages:  totalPages,
			TotalItems:  totalUsers,
			HasPrev:     page > 1,
			HasNext:     page < totalPages,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			Start:       (page-1)*limit + 1,
			End:         min(page*limit, totalUsers),
			Pages:       pages,
		}

		// Filtre değerleri
		filters := struct {
			Status string
			VIP    string
			Sort   string
			Search string
		}{
			Status: status,
			VIP:    vip,
			Sort:   sort,
			Search: search,
		}

		// Admin bilgileri
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Template verisi
		data := struct {
			Title      string
			Active     string
			Stats      interface{}
			Filters    interface{}
			Users      []map[string]interface{}
			Pagination interface{}
			Admin      models.Admin
		}{
			Title:      "Kullanıcı Yönetimi - Admin Panel",
			Active:     "users",
			Stats:      stats,
			Filters:    filters,
			Users:      users,
			Pagination: pagination,
			Admin:      admin,
		}

		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/users.html",
		)
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

func AdminUserDetailHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		vars := mux.Vars(r)
		userID := vars["id"]

		// Admin bilgileri
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Admin avatarını al
		var avatar sql.NullString
		db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Kullanıcıyı models.User struct'ına çek
		var user models.User
		var fullName, referralCode sql.NullString

		err := db.QueryRow(`
            SELECT 
                id, username, email,  
                COALESCE(NULLIF(avatar, ''), '/static/images/avatar.png') as avatar,
                COALESCE(bio, '') as bio,
                COALESCE(country, '') as country,
                COALESCE(website, '') as website,
                is_vip, vip_expiry_date, points, rank, 
                two_factor_enabled, created_at, last_login, is_active,
                COALESCE(fullname, '') as fullname,
                COALESCE(referral_code, '') as referral_code,
                newsletter
            FROM users WHERE id = $1
        `, userID).Scan(
			&user.ID, &user.Username, &user.Email,
			&user.Avatar, &user.Bio, &user.Country, &user.Website,
			&user.IsVIP, &user.VIPExpiryDate, &user.Points, &user.Rank,
			&user.TwoFactorEnabled, &user.CreatedAt, &user.LastLogin, &user.IsActive,
			&fullName, &referralCode, &user.Newsletter,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Kullanıcı bulunamadı", http.StatusNotFound)
			} else {
				fmt.Println(err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}

		// NullString değerlerini ata
		if fullName.Valid {
			user.FullName = fullName.String
		}
		if referralCode.Valid {
			user.ReferralCode = referralCode.String
		}

		// Template için veri hazırla - direkt user struct'ını kullan
		data := struct {
			Title  string
			Active string
			Admin  models.Admin
			User   models.User
		}{
			Title:  "Kullanıcı Detayı - Admin Panel",
			Active: "users",
			Admin:  admin,
			User:   user, // Direkt models.User gönder
		}

		funcMap := template.FuncMap{
			"eq": func(a, b interface{}) bool { return a == b },
			"formatDateTime": func(t time.Time) string {
				return t.Format("02 Jan 2006 15:04")
			},
			"formatNullTime": func(nt sql.NullTime) string {
				if nt.Valid {
					return nt.Time.Format("02 Jan 2006 15:04")
				}
				return "Hiç giriş yapmamış"
			},
			"hasValue": func(s string) bool {
				return s != ""
			},
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/user_detail.html",
		)
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, data)
	}
}

// AdminAddUserForm - Yeni kullanıcı ekleme formu
func AdminAddUserForm(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// Admin bilgilerini veritabanından al (avatar için)
		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png"

		// Admin avatarını veritabanından al
		var avatar sql.NullString
		err := db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Boş kullanıcı oluştur (tüm alanlar varsayılan)
		emptyUser := models.User{
			IsActive:         true,
			IsVIP:            false,
			EmailVerified:    false,
			Points:           0,
			Rank:             0,
			TwoFactorEnabled: false,
			Newsletter:       false,
			// SolvedCount KALDIRILDI - veritabanında yok
		}

		// Ülke listesi (select box için)
		countries := []string{
			// Avrupa
			"Türkiye",
			"Almanya",
			"Amerika Birleşik Devletleri",
			"İngiltere",
			"Fransa",
			"Hollanda",
			"İtalya",
			"İspanya",
			"Portekiz",
			"Belçika",
			"İsviçre",
			"Avusturya",
			"İsveç",
			"Norveç",
			"Danimarka",
			"Finlandiya",
			"Polonya",
			"Çekya",
			"Macaristan",
			"Romanya",
			"Bulgaristan",
			"Yunanistan",
			"Hırvatistan",
			"Sırbistan",
			"Slovakya",
			"Slovenya",
			"Litvanya",
			"Letonya",
			"Estonya",
			"İzlanda",
			"İrlanda",

			// Asya
			"Japonya",
			"Çin",
			"Güney Kore",
			"Hindistan",
			"Endonezya",
			"Malezya",
			"Singapur",
			"Tayland",
			"Vietnam",
			"Filipinler",
			"Pakistan",
			"Bangladeş",
			"Suudi Arabistan",
			"Birleşik Arap Emirlikleri",
			"İsrail",
			"Katar",
			"Kuveyt",
			"Umman",
			"Ürdün",
			"Lübnan",
			"Kazakistan",
			"Özbekistan",
			"Azerbaycan",
			"Gürcistan",
			"Ermenistan",

			// Amerika
			"Kanada",
			"Meksika",
			"Brezilya",
			"Arjantin",
			"Şili",
			"Kolombiya",
			"Peru",
			"Venezuela",
			"Ekvador",
			"Bolivya",
			"Paraguay",
			"Uruguay",
			"Kosta Rika",
			"Panama",
			"Dominik Cumhuriyeti",
			"Küba",

			// Afrika
			"Mısır",
			"Güney Afrika",
			"Fas",
			"Cezayir",
			"Tunus",
			"Nijerya",
			"Kenya",
			"Etiyopya",
			"Gana",
			"Fildişi Sahili",
			"Senegal",
			"Uganda",
			"Tanzanya",
			"Angola",
			"Zimbabve",
			"Kamerun",

			// Okyanusya
			"Avustralya",
			"Yeni Zelanda",
			"Fiji",
			"Papua Yeni Gine",

			// Diğer
			"Diğer",
		}

		data := struct {
			Title      string
			Active     string
			User       models.User
			Admin      models.Admin
			Countries  []string
			IsEdit     bool
			FormAction string
		}{
			Title:      "Yeni Kullanıcı Ekle - Admin Panel",
			Active:     "users",
			User:       emptyUser,
			Admin:      admin,
			Countries:  countries,
			IsEdit:     false,
			FormAction: "/admin/api/users/create", // Yeni kullanıcı oluşturma endpoint'i
		}

		// Template fonksiyonlarını ekle
		funcMap := template.FuncMap{
			"eq": func(a, b interface{}) bool {
				return a == b
			},
			"ne": func(a, b interface{}) bool {
				return a != b
			},
			"contains": func(s, substr string) bool {
				return strings.Contains(s, substr)
			},
		}

		// Template'i parse et
		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/user_form.html",
		)
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Template'i execute et
		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Template çalıştırılamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// AdminEditUserForm - Kullanıcı düzenleme formu
func AdminEditUserForm(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// URL'den kullanıcı ID'sini al
		vars := mux.Vars(r)
		userID := vars["id"]

		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// Admin bilgilerini al
		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png"

		var avatar sql.NullString
		err := db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Kullanıcı bilgilerini veritabanından al
		var user models.User
		var vipExpiryDate sql.NullTime
		var lastLogin sql.NullTime
		var country sql.NullString
		var bio sql.NullString
		var website sql.NullString
		var userAvatar sql.NullString
		var fullName sql.NullString
		var referralCode sql.NullString

		err = db.QueryRow(`
            SELECT 
                id, username, email, 
                COALESCE(avatar, '') as avatar,
                COALESCE(bio, '') as bio,
                COALESCE(country, '') as country,
                COALESCE(website, '') as website,
                points, rank, is_vip, vip_expiry_date, 
                is_active, two_factor_enabled,
                created_at, last_login,
                COALESCE(fullname, '') as fullname,
                COALESCE(referral_code, '') as referral_code,
                newsletter
            FROM users
            WHERE id = $1
        `, userID).Scan(
			&user.ID, &user.Username, &user.Email,
			&userAvatar, &bio, &country, &website,
			&user.Points, &user.Rank, &user.IsVIP, &vipExpiryDate,
			&user.IsActive, &user.TwoFactorEnabled,
			&user.CreatedAt, &lastLogin,
			&fullName, &referralCode,
			&user.Newsletter,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Kullanıcı bulunamadı", http.StatusNotFound)
			} else {
				log.Printf("Kullanıcı sorgu hatası: %v", err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}

		// Null değerleri ata
		if userAvatar.Valid {
			user.Avatar = userAvatar.String
		}
		if bio.Valid {
			user.Bio = bio.String
		}
		if country.Valid {
			user.Country = country.String
		}
		if website.Valid {
			user.Website = website.String
		}
		if fullName.Valid {
			user.FullName = fullName.String
		}
		if referralCode.Valid {
			user.ReferralCode = referralCode.String
		}
		if lastLogin.Valid {
			user.LastLogin = lastLogin
		}
		if vipExpiryDate.Valid {
			user.VIPExpiryDate = vipExpiryDate
		}

		// Ülke listesi
		countries := []string{
			// Avrupa
			"Türkiye",
			"Almanya",
			"Amerika Birleşik Devletleri",
			"İngiltere",
			"Fransa",
			"Hollanda",
			"İtalya",
			"İspanya",
			"Portekiz",
			"Belçika",
			"İsviçre",
			"Avusturya",
			"İsveç",
			"Norveç",
			"Danimarka",
			"Finlandiya",
			"Polonya",
			"Çekya",
			"Macaristan",
			"Romanya",
			"Bulgaristan",
			"Yunanistan",
			"Hırvatistan",
			"Sırbistan",
			"Slovakya",
			"Slovenya",
			"Litvanya",
			"Letonya",
			"Estonya",
			"İzlanda",
			"İrlanda",

			// Asya
			"Japonya",
			"Çin",
			"Güney Kore",
			"Hindistan",
			"Endonezya",
			"Malezya",
			"Singapur",
			"Tayland",
			"Vietnam",
			"Filipinler",
			"Pakistan",
			"Bangladeş",
			"Suudi Arabistan",
			"Birleşik Arap Emirlikleri",
			"İsrail",
			"Katar",
			"Kuveyt",
			"Umman",
			"Ürdün",
			"Lübnan",
			"Kazakistan",
			"Özbekistan",
			"Azerbaycan",
			"Gürcistan",
			"Ermenistan",

			// Amerika
			"Kanada",
			"Meksika",
			"Brezilya",
			"Arjantin",
			"Şili",
			"Kolombiya",
			"Peru",
			"Venezuela",
			"Ekvador",
			"Bolivya",
			"Paraguay",
			"Uruguay",
			"Kosta Rika",
			"Panama",
			"Dominik Cumhuriyeti",
			"Küba",

			// Afrika
			"Mısır",
			"Güney Afrika",
			"Fas",
			"Cezayir",
			"Tunus",
			"Nijerya",
			"Kenya",
			"Etiyopya",
			"Gana",
			"Fildişi Sahili",
			"Senegal",
			"Uganda",
			"Tanzanya",
			"Angola",
			"Zimbabve",
			"Kamerun",

			// Okyanusya
			"Avustralya",
			"Yeni Zelanda",
			"Fiji",
			"Papua Yeni Gine",

			// Diğer
			"Diğer",
		}
		data := struct {
			Title      string
			Active     string
			User       models.User
			Admin      models.Admin
			Countries  []string
			IsEdit     bool
			FormAction string
			UserID     string
		}{
			Title:      "Kullanıcı Düzenle - Admin Panel",
			Active:     "users",
			User:       user,
			Admin:      admin,
			Countries:  countries,
			IsEdit:     true,
			FormAction: "/admin/api/users/" + userID,
			UserID:     userID,
		}

		// Template fonksiyonlarını ekle
		funcMap := template.FuncMap{
			"eq": func(a, b interface{}) bool {
				return a == b
			},
			"ne": func(a, b interface{}) bool {
				return a != b
			},
			"contains": func(s, substr string) bool {
				return strings.Contains(s, substr)
			},
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/user_form.html",
		)
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

// AdminMachinesPage - Makine listesi sayfası
func AdminMachinesPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Sayfalama parametreleri
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 20
		}

		// Filtre parametreleri
		difficulty := r.URL.Query().Get("difficulty")
		status := r.URL.Query().Get("status")
		vip := r.URL.Query().Get("vip")
		search := r.URL.Query().Get("search")

		// Makineleri getir (sayfalı)
		query := `
            SELECT m.id, m.name, m.description, m.difficulty, m.points_reward,
                   m.is_vip_only, m.is_active, m.created_at,
                   COALESCE(u.username, 'System') as creator,
                   COUNT(DISTINCT mq.id) as question_count,
                   COUNT(DISTINCT s.id) as submission_count
            FROM machines m
            LEFT JOIN users u ON m.creator_id = u.id
            LEFT JOIN machine_questions mq ON m.id = mq.machine_id
            LEFT JOIN submissions s ON m.id = s.machine_id
            WHERE 1=1
        `
		var args []interface{}
		argCount := 1

		if difficulty != "" && difficulty != "all" {
			query += ` AND m.difficulty = $` + strconv.Itoa(argCount)
			args = append(args, difficulty)
			argCount++
		}

		if status != "" && status != "all" {
			if status == "active" {
				query += ` AND m.is_active = true`
			} else if status == "inactive" {
				query += ` AND m.is_active = false`
			}
		}

		if vip != "" && vip != "all" {
			if vip == "vip" {
				query += ` AND m.is_vip_only = true`
			} else if vip == "free" {
				query += ` AND m.is_vip_only = false`
			}
		}

		if search != "" {
			query += ` AND m.name ILIKE $` + strconv.Itoa(argCount)
			args = append(args, "%"+search+"%")
			argCount++
		}

		query += ` GROUP BY m.id, u.username`

		// Toplam sayı
		var total int
		countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count"
		err := db.QueryRow(countQuery, args...).Scan(&total)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Sayfalama
		query += ` ORDER BY m.created_at DESC 
                   LIMIT $` + strconv.Itoa(argCount) + ` 
                   OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		// Machines slice'ını doldur
		var machines []map[string]interface{}
		for rows.Next() {
			var m struct {
				ID              int
				Name            string
				Description     string
				Difficulty      string
				PointsReward    int
				IsVIPOnly       bool
				IsActive        bool
				CreatedAt       time.Time
				Creator         string
				QuestionCount   int
				SubmissionCount int
			}
			err := rows.Scan(
				&m.ID, &m.Name, &m.Description, &m.Difficulty, &m.PointsReward,
				&m.IsVIPOnly, &m.IsActive, &m.CreatedAt,
				&m.Creator, &m.QuestionCount, &m.SubmissionCount,
			)
			if err != nil {
				continue
			}

			machines = append(machines, map[string]interface{}{
				"ID":             m.ID,
				"Name":           m.Name,
				"Description":    m.Description,
				"Difficulty":     m.Difficulty,
				"PointsReward":   m.PointsReward,
				"IsVIPOnly":      m.IsVIPOnly,
				"IsActive":       m.IsActive,
				"CreatedAt":      m.CreatedAt,
				"Creator":        m.Creator,
				"TotalQuestions": m.QuestionCount,
				"SolverCount":    m.SubmissionCount,
				"ImageURL":       "/static/images/machines/default.jpeg", // Varsayılan görsel
			})
		}

		// İstatistikleri getir
		var stats struct {
			TotalMachines  int
			ActiveMachines int
			EasyCount      int
			MediumCount    int
			HardCount      int
			ExpertCount    int
		}

		db.QueryRow("SELECT COUNT(*) FROM machines").Scan(&stats.TotalMachines)
		db.QueryRow("SELECT COUNT(*) FROM machines WHERE is_active = true").Scan(&stats.ActiveMachines)
		db.QueryRow("SELECT COUNT(*) FROM machines WHERE difficulty = 'easy'").Scan(&stats.EasyCount)
		db.QueryRow("SELECT COUNT(*) FROM machines WHERE difficulty = 'medium'").Scan(&stats.MediumCount)
		db.QueryRow("SELECT COUNT(*) FROM machines WHERE difficulty = 'hard'").Scan(&stats.HardCount)
		db.QueryRow("SELECT COUNT(*) FROM machines WHERE difficulty = 'expert'").Scan(&stats.ExpertCount)

		// Sayfalama hesapla
		totalPages := (total + limit - 1) / limit

		// Sayfa numaralarını oluştur
		var pages []int
		for i := 1; i <= totalPages; i++ {
			if i == 1 || i == totalPages || (i >= page-2 && i <= page+2) {
				pages = append(pages, i)
			}
		}

		pagination := struct {
			CurrentPage int
			TotalPages  int
			HasPrev     bool
			HasNext     bool
			PrevPage    int
			NextPage    int
			Pages       []int
		}{
			CurrentPage: page,
			TotalPages:  totalPages,
			HasPrev:     page > 1,
			HasNext:     page < totalPages,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			Pages:       pages,
		}

		// Admin bilgileri
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Template verisi
		data := struct {
			Title      string
			Active     string
			Stats      interface{}
			Machines   []map[string]interface{}
			Pagination interface{}
			Admin      models.Admin
		}{
			Title:      "Makine Yönetimi - Admin Panel",
			Active:     "machines",
			Stats:      stats,
			Machines:   machines,
			Pagination: pagination,
			Admin:      admin,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"title": strings.Title,
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/machines.html",
		)
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

// AdminAddMachineForm - Yeni makine ekleme formu
func AdminAddMachineForm(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		difficulties := []string{"easy", "medium", "hard", "expert"}

		// Session'dan admin bilgilerini al
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// Admin struct'ını oluştur
		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Template fonksiyonlarını tanımla
		funcMap := template.FuncMap{
			"add": func(a, b int) int {
				return a + b
			},
			"mul": func(a, b int) int {
				return a * b
			},
			"div": func(a, b int) float64 {
				if b == 0 {
					return 0
				}
				return float64(a) / float64(b)
			},
		}

		data := struct {
			Title        string
			Active       string
			Machine      models.Machine
			Difficulties []string
			Admin        models.Admin // Admin alanını ekle
		}{
			Title:  "Yeni Makine Ekle - Admin Panel",
			Active: "machines",
			Machine: models.Machine{
				IsActive:  true,
				IsVIPOnly: false,
			},
			Difficulties: difficulties,
			Admin:        admin, // Admin bilgisini ata
		}

		// Template'i parse et
		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/machine_form.html",
		)
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

// AdminEditMachineForm - Makine düzenleme formu
func AdminEditMachineForm(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		vars := mux.Vars(r)
		id := vars["id"]

		// Makine bilgilerini getir
		var machine models.Machine
		var creatorID sql.NullInt64
		var dockerImage sql.NullString
		var updatedAt sql.NullTime

		err := db.QueryRow(`
            SELECT 
                id, name, description, difficulty, points_reward, 
                is_vip_only, docker_image, creator_id, created_at, updated_at, is_active
            FROM machines 
            WHERE id = $1
        `, id).Scan(
			&machine.ID, &machine.Name, &machine.Description, &machine.Difficulty,
			&machine.PointsReward, &machine.IsVIPOnly, &dockerImage,
			&creatorID, &machine.CreatedAt, &updatedAt, &machine.IsActive,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Makine bulunamadı", http.StatusNotFound)
			} else {
				log.Printf("Makine sorgu hatası: %v", err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}

		// Null değerleri ata
		if dockerImage.Valid {
			machine.DockerImage = dockerImage.String
		}

		// CreatorID'yi ata (int olarak, 0 = sistem)
		if creatorID.Valid {
			machine.CreatorID = int(creatorID.Int64)
		} else {
			machine.CreatorID = 0
		}

		if updatedAt.Valid {
			machine.UpdatedAt = updatedAt
		}

		// Oluşturan kullanıcı adını getir
		if machine.CreatorID > 0 {
			db.QueryRow("SELECT username FROM users WHERE id = $1", machine.CreatorID).Scan(&machine.CreatorName)
		} else {
			machine.CreatorName = "System"
		}

		// Soruları getir
		rows, err := db.Query(`
            SELECT id, question_order, title, description, points_reward, 
                   hint, hint_cost, is_active
            FROM machine_questions
            WHERE machine_id = $1
            ORDER BY question_order
        `, id)

		if err != nil {
			log.Printf("Soru sorgu hatası: %v", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var q models.Question
				err := rows.Scan(
					&q.ID, &q.QuestionOrder, &q.Title, &q.Description,
					&q.PointsReward, &q.Hint, &q.HintCost, &q.IsActive,
				)
				if err != nil {
					log.Printf("Soru satır okuma hatası: %v", err)
					continue
				}
				machine.Questions = append(machine.Questions, q)
			}
		}

		// Toplam soru sayısını güncelle
		machine.TotalQuestions = len(machine.Questions)

		// Çözen kullanıcı sayısını getir
		err = db.QueryRow(`
            SELECT COUNT(DISTINCT user_id) 
            FROM user_solutions 
            WHERE machine_id = $1
        `, id).Scan(&machine.SolverCount)
		if err != nil {
			log.Printf("Solver count sorgu hatası: %v", err)
		}

		difficulties := []string{"easy", "medium", "hard", "expert"}

		// Admin bilgilerini al
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		var avatar sql.NullString
		err = db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		data := struct {
			Title        string
			Active       string
			Machine      models.Machine
			Difficulties []string
			Admin        models.Admin
		}{
			Title:        "Makine Düzenle - Admin Panel",
			Active:       "machines",
			Machine:      machine,
			Difficulties: difficulties,
			Admin:        admin,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"add": func(a, b int) int {
				return a + b
			},
			"sub": func(a, b int) int {
				return a - b
			},
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006 15:04")
			},
			"mul": func(a, b int) int {
				return a * b
			},
			"eq": func(a, b interface{}) bool {
				return a == b
			},
			"contains": func(s, substr string) bool {
				return strings.Contains(s, substr)
			},
			"div": func(a, b int) float64 { return float64(a) / float64(b) },
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/machine_form.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
			http.Error(w, "Template çalıştırılamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

func AdminMachineDetail(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		vars := mux.Vars(r)
		machineID := vars["id"]

		// Admin bilgilerini al
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Admin avatarını al
		var avatar sql.NullString
		db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Makine detaylarını sorgula
		var machine struct {
			ID           int
			Name         string
			Description  string
			Difficulty   string
			PointsReward int
			IsVIPOnly    bool
			IsActive     bool
			DockerImage  string
			ImageURL     string
		}

		err := db.QueryRow(`
            SELECT id, name, description, difficulty, points_reward, 
                   is_vip_only, is_active, docker_image, image_url
            FROM machines 
            WHERE id = $1
        `, machineID).Scan(
			&machine.ID, &machine.Name, &machine.Description, &machine.Difficulty,
			&machine.PointsReward, &machine.IsVIPOnly, &machine.IsActive,
			&machine.DockerImage, &machine.ImageURL,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Makine bulunamadı", http.StatusNotFound)
			} else {
				http.Error(w, "Veritabanı hatası: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}

		// Template verisi
		data := struct {
			Title   string
			Active  string
			Admin   models.Admin
			Machine interface{}
		}{
			Title:   "Makine Detayı - Admin Panel",
			Active:  "machines",
			Admin:   admin,
			Machine: machine,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"eq": func(a, b interface{}) bool {
				return a == b
			},
		}

		// Template'i parse et
		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/machine_detail.html",
		)
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Template'i çalıştır
		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Template çalıştırılamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// AdminQuestionsPage - Soru listesi sayfası
func AdminQuestionsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Makineleri getir (filtre için)
		rows, err := db.Query("SELECT id, name FROM machines ORDER BY name")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var machines []models.Machine
		for rows.Next() {
			var m models.Machine
			rows.Scan(&m.ID, &m.Name)
			machines = append(machines, m)
		}
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png" // Varsayılan avatar

		data := struct {
			Title    string
			Active   string
			Machines []models.Machine
			Admin    models.Admin
		}{
			Title:    "Soru Yönetimi - Admin Panel",
			Active:   "questions",
			Machines: machines,
			Admin:    admin,
		}

		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/questions.html",
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, data)
	}
}



// AdminStatsPage - İstatistik sayfası
// AdminStatsPage - Sistem İstatistikleri sayfası (statik)
func AdminStatsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png"

		var avatar sql.NullString
		err := db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid && avatar.String != "" {
			admin.Avatar = avatar.String
		}

		data := struct {
			Title  string
			Active string
			Admin  models.Admin
		}{
			Title:  "Sistem İstatistikleri - Admin Panel",
			Active: "stats",
			Admin:  admin,
		}

		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/stats.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Template yüklenemedi", http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, data)
	}
}

// AdminLogsPage - Log görüntüleme sayfası
func AdminLogsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png" // Varsayılan avatar
		data := struct {
			Title  string
			Active string
			Admin  models.Admin
		}{
			Title:  "Sistem Logları - Admin Panel",
			Active: "logs",
			Admin:  admin,
		}

		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/logs.html",
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, data)
	}
}

// AdminSettingsPage - Ayarlar sayfası

// AdminSubmissionsPage - Çözüm listesi sayfası
func AdminSubmissionsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// Admin bilgilerini al
		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png"

		var avatar sql.NullString
		err := db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Filtreleri al
		status := r.URL.Query().Get("status")
		machineID := r.URL.Query().Get("machine")
		search := r.URL.Query().Get("search")

		// İstatistikleri hesapla
		var stats struct {
			TotalSubmissions    int
			AcceptedSubmissions int
			FailedSubmissions   int
			UniqueSolvers       int
		}

		// Toplam çözüm
		db.QueryRow("SELECT COUNT(*) FROM submissions").Scan(&stats.TotalSubmissions)

		// Başarılı çözümler
		db.QueryRow("SELECT COUNT(*) FROM submissions WHERE status = 'accepted'").Scan(&stats.AcceptedSubmissions)

		// Başarısız çözümler
		db.QueryRow("SELECT COUNT(*) FROM submissions WHERE status = 'rejected'").Scan(&stats.FailedSubmissions)

		// Benzersiz çözenler
		db.QueryRow("SELECT COUNT(DISTINCT user_id) FROM submissions WHERE status = 'accepted'").Scan(&stats.UniqueSolvers)

		// Makine listesi (filtre için)
		machineRows, err := db.Query("SELECT id, name FROM machines WHERE is_active = true ORDER BY name")
		if err != nil {
			log.Printf("Makine listesi sorgu hatası: %v", err)
		}
		defer machineRows.Close()

		var machines []models.Machine
		for machineRows.Next() {
			var m models.Machine
			machineRows.Scan(&m.ID, &m.Name)
			machines = append(machines, m)
		}

		// Submission sorgusu - submitted_flag kullan
		query := `
			SELECT 
				s.id, 
				u.username, 
				COALESCE(u.avatar, '') as avatar,
				m.name as machine_name,
				mq.title as question_title,
				s.submitted_flag,
				s.status,
				s.ip_address,
				s.created_at
			FROM submissions s
			JOIN users u ON s.user_id = u.id
			JOIN machines m ON s.machine_id = m.id
			JOIN machine_questions mq ON s.question_id = mq.id
			WHERE 1=1
		`

		var args []interface{}
		argCount := 1

		if status != "" && status != "all" {
			query += fmt.Sprintf(" AND s.status = $%d", argCount)
			args = append(args, status)
			argCount++
		}

		if machineID != "" && machineID != "all" {
			var mid int
			fmt.Sscanf(machineID, "%d", &mid)
			query += fmt.Sprintf(" AND s.machine_id = $%d", argCount)
			args = append(args, mid)
			argCount++
		}

		if search != "" {
			query += fmt.Sprintf(" AND u.username ILIKE $%d", argCount)
			args = append(args, "%"+search+"%")
			argCount++
		}

		query += " ORDER BY s.created_at DESC LIMIT 100"

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("Submission sorgu hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type Submission struct {
			ID            int
			Username      string
			UserAvatar    string
			MachineName   string
			QuestionTitle string
			SubmittedFlag string
			Status        string
			IPAddress     string
			CreatedAt     time.Time
		}

		var submissions []Submission
		for rows.Next() {
			var s Submission
			var avatar sql.NullString
			err := rows.Scan(&s.ID, &s.Username, &avatar, &s.MachineName, &s.QuestionTitle, &s.SubmittedFlag, &s.Status, &s.IPAddress, &s.CreatedAt)
			if err != nil {
				log.Printf("Submission scan hatası: %v", err)
				continue
			}
			if avatar.Valid && avatar.String != "" {
				s.UserAvatar = avatar.String
			} else {
				s.UserAvatar = "/static/images/avatar.png"
			}
			submissions = append(submissions, s)
		}

		data := struct {
			Title         string
			Active        string
			Admin         models.Admin
			Stats         interface{}
			Machines      []models.Machine
			Submissions   []Submission
			StatusFilter  string
			MachineFilter string
			SearchFilter  string
		}{
			Title:         "Çözümler - Admin Panel",
			Active:        "submissions",
			Admin:         admin,
			Stats:         stats,
			Machines:      machines,
			Submissions:   submissions,
			StatusFilter:  status,
			MachineFilter: machineID,
			SearchFilter:  search,
		}

		tmpl, err := template.New("layout.html").Funcs(template.FuncMap{
			"eq": func(a, b interface{}) bool {
				if a == nil || b == nil {
					return false
				}
				return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
			},
		}).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/submissions.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Template yüklenemedi", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
			http.Error(w, "Template çalıştırılamadı", http.StatusInternalServerError)
		}
	}
}

// ==================== ADMIN API ENDPOINT'LERİ (JSON) ====================

// Admin Giriş API
func AdminLogin(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Geçersiz istek", http.StatusBadRequest)
			return
		}

		// Admin kullanıcısını kontrol et (önce username ile dene, olmazsa email ile)
		var admin models.User
		var role string
		err := db.QueryRow(`
            SELECT id, username, password_hash, role 
            FROM admins 
            WHERE (username = $1 OR email = $1) AND is_active = true
        `, req.Username).Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &role)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Kullanıcı adı veya şifre hatalı",
			})
			return
		}

		// Şifre kontrolü
		err = bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password))
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Kullanıcı adı veya şifre hatalı",
			})
			return
		}

		// Admin session oluştur
		session, _ := store.Get(r, "admin_session")
		session.Values["authenticated"] = true
		session.Values["admin_id"] = admin.ID
		session.Values["username"] = admin.Username
		session.Values["role"] = role
		session.Options.MaxAge = 3600 // 1 saat
		session.Save(r, w)

		// JWT token oluştur
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"admin_id": admin.ID,
			"username": admin.Username,
			"role":     role,
			"exp":      time.Now().Add(1 * time.Hour).Unix(),
		})

		tokenString, _ := token.SignedString([]byte("admin-secret-key"))

		// Son giriş zamanını güncelle
		db.Exec("UPDATE admins SET last_login = NOW() WHERE id = $1", admin.ID)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"access_token":  tokenString,
			"refresh_token": "",
			"user": map[string]interface{}{
				"id":       admin.ID,
				"username": admin.Username,
				"role":     role,
			},
		})
	}
}

// AdminUsers API - Kullanıcı listesi (JSON)
func AdminUsers(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session kontrolü
		session, _ := store.Get(r, "admin_session")
		if session.Values["username"] == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Query parametrelerini al
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		search := r.URL.Query().Get("search")
		status := r.URL.Query().Get("status")
		vip := r.URL.Query().Get("vip")
		sort := r.URL.Query().Get("sort")

		// Default değerler
		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 20
		}

		// Base query - DOĞRU SÜTUN İSİMLERİ
		query := `
            SELECT 
                u.id, 
                u.username, 
                u.email, 
                u.points, 
                u.is_vip, 
                u.is_active, 
                u.country,
                COALESCE(u.last_login, u.created_at) as last_activity, 
                u.created_at, 
                COALESCE(u.fullname, '') as fullname,
                COALESCE(u.avatar, '/static/images/avatar.png') as avatar,
                COUNT(DISTINCT us.question_id) as solved_count,
                COUNT(DISTINCT CASE WHEN us.used_hint THEN us.question_id END) as hint_used_count
            FROM users u
            LEFT JOIN user_solutions us ON u.id = us.user_id
            WHERE 1=1
        `

		// Group by ekle (çünkü COUNT kullandık)
		groupBy := ` GROUP BY u.id, u.username, u.email, u.points, u.is_vip, 
		                    u.is_active, u.country, u.last_login, u.created_at, 
		                    u.fullname, u.avatar`

		countQuery := `SELECT COUNT(*) FROM users WHERE 1=1`

		var args []interface{}
		var countArgs []interface{}
		argCount := 1

		// Arama filtresi
		if search != "" {
			query += ` AND (u.username ILIKE $` + strconv.Itoa(argCount) +
				` OR u.email ILIKE $` + strconv.Itoa(argCount) +
				` OR COALESCE(u.fullname, '') ILIKE $` + strconv.Itoa(argCount) +
				` OR COALESCE(u.country, '') ILIKE $` + strconv.Itoa(argCount) + `)`
			countQuery += ` AND (username ILIKE $` + strconv.Itoa(argCount) +
				` OR email ILIKE $` + strconv.Itoa(argCount) +
				` OR COALESCE(fullname, '') ILIKE $` + strconv.Itoa(argCount) +
				` OR COALESCE(country, '') ILIKE $` + strconv.Itoa(argCount) + `)`
			args = append(args, "%"+search+"%")
			countArgs = append(countArgs, "%"+search+"%")
			argCount++
		}

		// Durum filtresi (active/inactive)
		if status == "active" {
			query += ` AND u.is_active = true`
			countQuery += ` AND is_active = true`
		} else if status == "inactive" {
			query += ` AND u.is_active = false`
			countQuery += ` AND is_active = false`
		}

		// VIP filtresi
		if vip == "vip" {
			query += ` AND u.is_vip = true`
			countQuery += ` AND is_vip = true`
		} else if vip == "normal" {
			query += ` AND u.is_vip = false`
			countQuery += ` AND is_vip = false`
		}

		// Toplam sayıyı al
		var total int
		err := db.QueryRow(countQuery, countArgs...).Scan(&total)
		if err != nil {
			http.Error(w, "Toplam kullanıcı sayısı alınamadı: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Group by'ı ana query'e ekle
		query += groupBy

		// Sıralama
		switch sort {
		case "points_desc":
			query += ` ORDER BY u.points DESC`
		case "points_asc":
			query += ` ORDER BY u.points ASC`
		case "name_asc":
			query += ` ORDER BY u.username ASC`
		case "name_desc":
			query += ` ORDER BY u.username DESC`
		case "solved_desc":
			query += ` ORDER BY solved_count DESC`
		case "solved_asc":
			query += ` ORDER BY solved_count ASC`
		case "oldest":
			query += ` ORDER BY u.created_at ASC`
		default: // "date_desc" veya varsayılan
			query += ` ORDER BY u.created_at DESC`
		}

		// Sayfalama
		query += ` LIMIT $` + strconv.Itoa(argCount) +
			` OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		// Kullanıcıları getir
		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, "Kullanıcılar getirilemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type UserWithStats struct {
			models.User
			SolvedCount   int `json:"solvedCount"`
			HintUsedCount int `json:"hintUsedCount"`
		}

		var users []UserWithStats
		for rows.Next() {
			var u UserWithStats
			var lastActivity sql.NullTime
			var fullName sql.NullString
			var avatar sql.NullString
			var country sql.NullString

			err := rows.Scan(
				&u.ID,
				&u.Username,
				&u.Email,
				&u.Points,
				&u.IsVIP,
				&u.IsActive,
				&country,
				&lastActivity,
				&u.CreatedAt,
				&fullName,
				&avatar,
				&u.SolvedCount,
				&u.HintUsedCount,
			)
			if err != nil {
				log.Printf("Kullanıcı satırı okunamadı: %v", err)
				continue
			}

			// Null değerleri kontrol et
			if lastActivity.Valid {
				u.LastLogin = lastActivity
			}
			if fullName.Valid {
				u.FullName = fullName.String
			}
			if avatar.Valid {
				u.Avatar = avatar.String
			} else {
				u.Avatar = "/static/images/avatar.png"
			}
			if country.Valid {
				u.Country = country.String
			}

			users = append(users, u)
		}

		// İstatistikleri al
		var stats struct {
			TotalUsers       int `json:"totalUsers"`
			ActiveUsers      int `json:"activeUsers"`
			VIPUsers         int `json:"vipUsers"`
			NewToday         int `json:"newToday"`
			TotalSolved      int `json:"totalSolved"`
			TotalSubmissions int `json:"totalSubmissions"`
		}
		stats.TotalUsers = total

		// Aktif kullanıcı sayısı (son 24 saat)
		err = db.QueryRow(`
            SELECT COUNT(*) FROM users 
            WHERE last_login > NOW() - INTERVAL '24 hours'
        `).Scan(&stats.ActiveUsers)
		if err != nil {
			log.Printf("Aktif kullanıcı sayısı alınamadı: %v", err)
		}

		// VIP kullanıcı sayısı
		err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_vip = true`).Scan(&stats.VIPUsers)
		if err != nil {
			log.Printf("VIP kullanıcı sayısı alınamadı: %v", err)
		}

		// Bugün kaydolanlar
		err = db.QueryRow(`
            SELECT COUNT(*) FROM users 
            WHERE DATE(created_at) = CURRENT_DATE
        `).Scan(&stats.NewToday)
		if err != nil {
			log.Printf("Bugün kaydolanlar alınamadı: %v", err)
		}

		// Toplam çözülen sorular
		err = db.QueryRow(`SELECT COUNT(*) FROM user_solutions`).Scan(&stats.TotalSolved)
		if err != nil {
			log.Printf("Toplam çözülen sorular alınamadı: %v", err)
		}

		// Toplam submission
		err = db.QueryRow(`SELECT COUNT(*) FROM submissions`).Scan(&stats.TotalSubmissions)
		if err != nil {
			log.Printf("Toplam submission alınamadı: %v", err)
		}

		// Sayfalama hesaplamaları
		totalPages := (total + limit - 1) / limit
		if totalPages < 1 {
			totalPages = 1
		}

		// Sayfa numaralarını oluştur
		var pages []int
		startPage := max(1, page-2)
		endPage := min(totalPages, page+2)

		for i := startPage; i <= endPage; i++ {
			pages = append(pages, i)
		}

		// Admin bilgilerini al
		admin := models.Admin{
			Username: session.Values["username"].(string),
			Role:     session.Values["role"].(string),
			Avatar:   "/static/images/avatar.png",
		}

		// Admin avatarını veritabanından al
		var avatar sql.NullString
		err = db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, admin.Username).Scan(&avatar)
		if err == nil && avatar.Valid {
			admin.Avatar = avatar.String
		}

		// Template data
		data := struct {
			Title      string
			Active     string
			Admin      models.Admin
			Users      interface{} // UserWithStats kullanılıyor
			Stats      interface{}
			Pagination struct {
				CurrentPage int
				TotalPages  int
				TotalItems  int
				Start       int
				End         int
				HasPrev     bool
				HasNext     bool
				PrevPage    int
				NextPage    int
				Pages       []int
			}
			Filters struct {
				Search string
				Status string
				VIP    string
				Sort   string
			}
		}{
			Title:  "Kullanıcı Yönetimi - Admin Panel",
			Active: "users",
			Admin:  admin,
			Users:  users,
			Stats:  stats,
		}

		// Sayfalama bilgileri
		data.Pagination.CurrentPage = page
		data.Pagination.TotalPages = totalPages
		data.Pagination.TotalItems = total
		data.Pagination.Start = (page-1)*limit + 1
		data.Pagination.End = min(page*limit, total)
		data.Pagination.HasPrev = page > 1
		data.Pagination.HasNext = page < totalPages
		data.Pagination.PrevPage = page - 1
		data.Pagination.NextPage = page + 1
		data.Pagination.Pages = pages

		// Filtre bilgileri
		data.Filters.Search = search
		data.Filters.Status = status
		data.Filters.VIP = vip
		data.Filters.Sort = sort

		// JSON yanıt mı yoksa HTML mi kontrol et
		if r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(data)
			return
		}

		// HTML template
		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/users.html",
		)
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Template oluşturulamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// Yardımcı fonksiyonlar
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AdminUserDetail API - Kullanıcı detayı (JSON)
func AdminUserDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		// Kullanıcı bilgilerini al - location kaldırıldı, country eklendi
		var user models.User
		var vipExpiryDate sql.NullTime
		var lastLogin sql.NullTime
		var country sql.NullString
		var bio sql.NullString
		var website sql.NullString
		var avatar sql.NullString

		err := db.QueryRow(`
            SELECT 
                id, 
                username, 
                email, 
                COALESCE(avatar, '/static/images/avatar.png') as avatar,
                COALESCE(bio, '') as bio,
                COALESCE(country, '') as country,  -- location DEĞİL, country
                COALESCE(website, '') as website,
                points, 
                rank, 
                is_vip, 
                vip_expiry_date, 
                is_active,
                created_at, 
                last_login,
                COALESCE(fullname, '') as fullname,
                email_verified,
                newsletter
            FROM users
            WHERE id = $1
        `, userID).Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&avatar,
			&bio,
			&country,
			&website,
			&user.Points,
			&user.Rank,
			&user.IsVIP,
			&vipExpiryDate,
			&user.IsActive,
			&user.CreatedAt,
			&lastLogin,
			&user.FullName,
			&user.EmailVerified,
			&user.Newsletter,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Kullanıcı bulunamadı", http.StatusNotFound)
			} else {
				log.Printf("Kullanıcı sorgu hatası: %v", err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}

		// Null değerleri ata
		if avatar.Valid {
			user.Avatar = avatar.String
		}
		if bio.Valid {
			user.Bio = bio.String
		}
		if country.Valid {
			user.Country = country.String
		}
		if website.Valid {
			user.Website = website.String
		}
		if lastLogin.Valid {
			user.LastLogin = lastLogin
		}
		if vipExpiryDate.Valid {
			user.VIPExpiryDate = vipExpiryDate
		}

		// Kullanıcı istatistikleri
		var stats struct {
			TotalSubmissions    int     `json:"total_submissions"`
			AcceptedSubmissions int     `json:"accepted_submissions"`
			RejectedSubmissions int     `json:"rejected_submissions"`
			PendingSubmissions  int     `json:"pending_submissions"`
			TotalMachines       int     `json:"total_machines"`
			TotalQuestions      int     `json:"total_questions"`
			TotalPoints         int     `json:"total_points"`
			HintUsedCount       int     `json:"hint_used_count"`
			SuccessRate         float64 `json:"success_rate"`
			Rank                int     `json:"rank"`
			TotalUsers          int     `json:"total_users"`
		}

		// Submission istatistikleri
		err = db.QueryRow(`
            SELECT 
                COUNT(*) as total_submissions,
                COUNT(CASE WHEN status = 'accepted' THEN 1 END) as accepted,
                COUNT(CASE WHEN status = 'rejected' THEN 1 END) as rejected,
                COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending,
                COUNT(DISTINCT machine_id) as machines,
                COUNT(DISTINCT question_id) as questions,
                COALESCE(SUM(CASE WHEN status = 'accepted' THEN points_awarded ELSE 0 END), 0) as points
            FROM submissions
            WHERE user_id = $1
        `, userID).Scan(
			&stats.TotalSubmissions,
			&stats.AcceptedSubmissions,
			&stats.RejectedSubmissions,
			&stats.PendingSubmissions,
			&stats.TotalMachines,
			&stats.TotalQuestions,
			&stats.TotalPoints,
		)
		if err != nil {
			log.Printf("İstatistik sorgu hatası: %v", err)
		}

		// İpucu kullanım istatistiği
		err = db.QueryRow(`
            SELECT COUNT(DISTINCT question_id)
            FROM hint_usage
            WHERE user_id = $1
        `, userID).Scan(&stats.HintUsedCount)
		if err != nil {
			log.Printf("İpucu sorgu hatası: %v", err)
		}

		// Başarı oranı
		if stats.TotalSubmissions > 0 {
			stats.SuccessRate = float64(stats.AcceptedSubmissions) / float64(stats.TotalSubmissions) * 100
		}

		// Kullanıcının sıralaması ve toplam kullanıcı sayısı
		err = db.QueryRow(`
            WITH ranked_users AS (
                SELECT id, RANK() OVER (ORDER BY points DESC) as user_rank
                FROM users
                WHERE is_active = true
            )
            SELECT ru.user_rank, (SELECT COUNT(*) FROM users WHERE is_active = true)
            FROM ranked_users ru
            WHERE ru.id = $1
        `, userID).Scan(&stats.Rank, &stats.TotalUsers)
		if err != nil {
			log.Printf("Sıralama sorgu hatası: %v", err)
		}

		// Son aktiviteler (submissions)
		rows, err := db.Query(`
            SELECT 
                s.id,
                s.status,
                s.points_awarded,
                s.created_at,
                s.attempt_count,
                m.id as machine_id,
                m.name as machine_name,
                m.difficulty as machine_difficulty,
                mq.id as question_id,
                mq.title as question_title,
                mq.question_order as question_order
            FROM submissions s
            JOIN machines m ON s.machine_id = m.id
            JOIN machine_questions mq ON s.question_id = mq.id
            WHERE s.user_id = $1
            ORDER BY s.created_at DESC
            LIMIT 20
        `, userID)
		if err != nil {
			log.Printf("Submission sorgu hatası: %v", err)
		} else {
			defer rows.Close()

			type SubmissionDetail struct {
				ID                int       `json:"id"`
				Status            string    `json:"status"`
				PointsAwarded     int       `json:"points_awarded"`
				SubmittedAt       time.Time `json:"submitted_at"`
				AttemptCount      int       `json:"attempt_count"`
				MachineID         int       `json:"machine_id"`
				MachineName       string    `json:"machine_name"`
				MachineDifficulty string    `json:"machine_difficulty"`
				QuestionID        int       `json:"question_id"`
				QuestionTitle     string    `json:"question_title"`
				QuestionOrder     int       `json:"question_order"`
			}

			var submissions []SubmissionDetail
			for rows.Next() {
				var s SubmissionDetail
				err := rows.Scan(
					&s.ID,
					&s.Status,
					&s.PointsAwarded,
					&s.SubmittedAt,
					&s.AttemptCount,
					&s.MachineID,
					&s.MachineName,
					&s.MachineDifficulty,
					&s.QuestionID,
					&s.QuestionTitle,
					&s.QuestionOrder,
				)
				if err != nil {
					log.Printf("Submission satır okuma hatası: %v", err)
					continue
				}
				submissions = append(submissions, s)
			}

			// Çözülen makineler (benzersiz)
			machineRows, err := db.Query(`
                SELECT DISTINCT 
                    m.id,
                    m.name,
                    m.difficulty,
                    m.points_reward,
                    COUNT(DISTINCT us.question_id) as solved_questions,
                    (SELECT COUNT(*) FROM machine_questions WHERE machine_id = m.id) as total_questions,
                    MIN(us.solved_at) as first_solved_at,
                    MAX(us.solved_at) as last_solved_at
                FROM user_solutions us
                JOIN machines m ON us.machine_id = m.id
                WHERE us.user_id = $1
                GROUP BY m.id, m.name, m.difficulty, m.points_reward
                ORDER BY last_solved_at DESC
                LIMIT 10
            `, userID)

			var solvedMachines []map[string]interface{}
			if err == nil {
				defer machineRows.Close()
				for machineRows.Next() {
					var id int
					var name, difficulty string
					var points, solvedQ, totalQ int
					var firstSolved, lastSolved time.Time

					err := machineRows.Scan(&id, &name, &difficulty, &points, &solvedQ, &totalQ, &firstSolved, &lastSolved)
					if err == nil {
						solvedMachines = append(solvedMachines, map[string]interface{}{
							"id":               id,
							"name":             name,
							"difficulty":       difficulty,
							"points_reward":    points,
							"solved_questions": solvedQ,
							"total_questions":  totalQ,
							"completion_rate":  float64(solvedQ) / float64(totalQ) * 100,
							"first_solved_at":  firstSolved,
							"last_solved_at":   lastSolved,
						})
					}
				}
			}

			// Kazanılan başarımlar
			achievementRows, err := db.Query(`
                SELECT 
                    a.id,
                    a.name,
                    a.description,
                    a.icon,
                    a.points_reward,
                    ua.earned_at
                FROM user_achievements ua
                JOIN achievements a ON ua.achievement_id = a.id
                WHERE ua.user_id = $1
                ORDER BY ua.earned_at DESC
            `, userID)

			var achievements []map[string]interface{}
			if err == nil {
				defer achievementRows.Close()
				for achievementRows.Next() {
					var id, points int
					var name, description, icon string
					var earnedAt time.Time

					err := achievementRows.Scan(&id, &name, &description, &icon, &points, &earnedAt)
					if err == nil {
						achievements = append(achievements, map[string]interface{}{
							"id":          id,
							"name":        name,
							"description": description,
							"icon":        icon,
							"points":      points,
							"earned_at":   earnedAt,
						})
					}
				}
			}

			// JSON yanıt
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"user":            user,
				"stats":           stats,
				"recent_activity": submissions,
				"solved_machines": solvedMachines,
				"achievements":    achievements,
				"success":         true,
			})
		}
	}
}

// AdminUpdateUser API - Kullanıcı güncelle (JSON)
func AdminUpdateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Geçersiz istek", http.StatusBadRequest)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Kullanıcıyı güncelle
		query := "UPDATE users SET "
		var args []interface{}
		argCount := 1

		allowedFields := []string{"is_vip", "is_active", "points", "rank"}
		for _, field := range allowedFields {
			if val, ok := updates[field]; ok {
				if argCount > 1 {
					query += ", "
				}
				query += field + " = $" + strconv.Itoa(argCount)
				args = append(args, val)
				argCount++
			}
		}

		if argCount > 1 {
			query += " WHERE id = $" + strconv.Itoa(argCount)
			args = append(args, userID)

			_, err = tx.Exec(query, args...)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Şifre değiştirme
		if newPassword, ok := updates["new_password"]; ok && newPassword != "" {
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword.(string)), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			_, err = tx.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", hashedPassword, userID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		tx.Commit()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Kullanıcı güncellendi",
		})
	}
}

// AdminToggleUserStatus API - Kullanıcı durumunu değiştir
func AdminToggleUserStatus(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		_, err := db.Exec("UPDATE users SET is_active = NOT is_active WHERE id = $1", userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Kullanıcı durumu güncellendi",
		})
	}
}

// AdminToggleUserVIP API - Kullanıcı VIP durumunu değiştir
func AdminToggleUserVIP(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		_, err := db.Exec("UPDATE users SET is_vip = NOT is_vip WHERE id = $1", userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Kullanıcı VIP durumu güncellendi",
		})
	}
}

// AdminResetPassword API - Şifre sıfırla
func AdminResetPassword(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		// Rastgele şifre oluştur
		newPassword := generateRandomPassword(10)
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)

		_, err := db.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", hashedPassword, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"password": newPassword,
			"message":  "Şifre sıfırlandı",
		})
	}
}

// AdminDeleteUser API - Kullanıcı sil
func AdminDeleteUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		_, err := db.Exec("DELETE FROM users WHERE id = $1", userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Kullanıcı silindi",
		})
	}
}

// AdminCreateUser API - Yeni kullanıcı oluştur
// AdminCreateUser - Yeni kullanıcı oluştur API
func AdminCreateUser(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Multipart form verisini parse et (10 MB limit)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			log.Printf("Multipart form parse hatası: %v", err)
			http.Error(w, "Form verisi çok büyük veya geçersiz", http.StatusBadRequest)
			return
		}

		// Form verilerini al
		username := r.FormValue("username")
		email := r.FormValue("email")
		password := r.FormValue("password")
		fullname := r.FormValue("fullname")
		country := r.FormValue("country")
		countryCode := getCountryCode(country)
		website := r.FormValue("website")
		bio := r.FormValue("bio")

		// Checkbox değerleri
		isActive := r.FormValue("is_active") == "true" || r.FormValue("is_active") == "on"
		isVIP := r.FormValue("is_vip") == "true" || r.FormValue("is_vip") == "on"
		newsletter := r.FormValue("newsletter") == "true" || r.FormValue("newsletter") == "on"

		// Zorunlu alanları kontrol et
		if username == "" || email == "" || password == "" {
			log.Printf("Eksik alanlar: username=%v, email=%v, password=%v", username != "", email != "", password != "")
			http.Error(w, "Kullanıcı adı, email ve şifre zorunludur", http.StatusBadRequest)
			return
		}

		// Şifreyi hashle
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("Şifre hashleme hatası: %v", err)
			http.Error(w, "Şifre hashlenemedi", http.StatusInternalServerError)
			return
		}

		// Profil resmini işle (varsa)
		var avatarPath = "/static/images/default-avatar.png"
		file, header, err := r.FormFile("avatar")
		if err == nil {
			defer file.Close()

			// Uploads dizininin var olduğundan emin ol
			uploadDir := "uploads/avatars"
			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				log.Printf("Uploads dizini oluşturma hatası: %v", err)
				http.Error(w, "Dosya yükleme dizini oluşturulamadı", http.StatusInternalServerError)
				return
			}

			// Dosya tipini kontrol et
			contentType := header.Header.Get("Content-Type")
			allowedTypes := map[string]bool{
				"image/jpeg": true,
				"image/png":  true,
				"image/gif":  true,
				"image/webp": true,
			}

			if !allowedTypes[contentType] {
				log.Printf("Geçersiz dosya tipi: %s", contentType)
				http.Error(w, "Sadece JPEG, PNG, GIF ve WEBP dosyaları yüklenebilir", http.StatusBadRequest)
				return
			}

			// Benzersiz dosya adı oluştur
			ext := filepath.Ext(header.Filename)
			fileName := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), username, ext)
			filePath := filepath.Join(uploadDir, fileName)

			// Dosyayı kaydet
			dst, err := os.Create(filePath)
			if err != nil {
				log.Printf("Dosya oluşturma hatası: %v", err)
				http.Error(w, "Dosya kaydedilemedi: "+err.Error(), http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				log.Printf("Dosya kopyalama hatası: %v", err)
				http.Error(w, "Dosya kopyalanamadı", http.StatusInternalServerError)
				return
			}

			// Dosya boyutunu kontrol et (5 MB)
			fileInfo, err := dst.Stat()
			if err == nil && fileInfo.Size() > 5*1024*1024 {
				os.Remove(filePath) // Dosyayı sil
				http.Error(w, "Dosya boyutu 5 MB'dan büyük olamaz", http.StatusBadRequest)
				return
			}

			avatarPath = "/uploads/avatars/" + fileName
			log.Printf("Avatar yüklendi: %s", avatarPath)
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction başlatma hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Kullanıcıyı veritabanına ekle
		var userID int
		err = tx.QueryRow(`
            INSERT INTO users (
                username, email, password_hash, fullname, is_active, 
                avatar, country, website, bio, is_vip, newsletter,
                points, rank, created_at
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0, 0, NOW())
            RETURNING id
        `,
			username,
			email,
			string(hashedPassword),
			fullname,
			isActive,
			avatarPath,
			countryCode,
			website,
			bio,
			isVIP,
			newsletter,
		).Scan(&userID)

		if err != nil {
			log.Printf("Kullanıcı ekleme hatası: %v", err)
			http.Error(w, "Kullanıcı oluşturulamadı: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Varsayılan ayarları ekle
		_, err = tx.Exec(`
            INSERT INTO user_settings (
                user_id, email_notifications, browser_notifications, 
                sound_enabled, profile_public, show_activity, show_online_status,
                theme, font_size, language, updated_at
            ) VALUES ($1, true, true, false, true, true, true, 'dark', 'medium', 'tr', NOW())
        `, userID)

		if err != nil {
			log.Printf("Varsayılan ayar ekleme hatası: %v", err)
			// Hata önemli değil, devam et
		}

		// Transaction'ı commit et
		err = tx.Commit()
		if err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			http.Error(w, "İşlem tamamlanamadı", http.StatusInternalServerError)
			return
		}

		log.Printf("Yeni kullanıcı oluşturuldu: ID=%d, Username=%s", userID, username)

		// Başarılı - kullanıcı listesine yönlendir
		http.Redirect(w, r, "/admin/users?success=created", http.StatusSeeOther)
	}
}

func getCountryCode(countryName string) string {
	countryMap := map[string]string{
		// Avrupa
		"Türkiye":                     "TR",
		"Almanya":                     "DE",
		"Amerika":                     "US",
		"Amerika Birleşik Devletleri": "US",
		"İngiltere":                   "GB",
		"Birleşik Krallık":            "GB",
		"Fransa":                      "FR",
		"Hollanda":                    "NL",
		"İtalya":                      "IT",
		"İspanya":                     "ES",
		"Portekiz":                    "PT",
		"Belçika":                     "BE",
		"İsviçre":                     "CH",
		"Avusturya":                   "AT",
		"İsveç":                       "SE",
		"Norveç":                      "NO",
		"Danimarka":                   "DK",
		"Finlandiya":                  "FI",
		"Polonya":                     "PL",
		"Çekya":                       "CZ",
		"Çek Cumhuriyeti":             "CZ",
		"Macaristan":                  "HU",
		"Romanya":                     "RO",
		"Bulgaristan":                 "BG",
		"Yunanistan":                  "GR",
		"Hırvatistan":                 "HR",
		"Sırbistan":                   "RS",
		"Slovakya":                    "SK",
		"Slovenya":                    "SI",
		"Litvanya":                    "LT",
		"Letonya":                     "LV",
		"Estonya":                     "EE",
		"İzlanda":                     "IS",
		"İrlanda":                     "IE",
		"Rusya":                       "RU",
		"Rusya Federasyonu":           "RU",

		// Asya
		"Japonya":                   "JP",
		"Çin":                       "CN",
		"Çin Halk Cumhuriyeti":      "CN",
		"Güney Kore":                "KR",
		"Hindistan":                 "IN",
		"Endonezya":                 "ID",
		"Malezya":                   "MY",
		"Singapur":                  "SG",
		"Tayland":                   "TH",
		"Vietnam":                   "VN",
		"Filipinler":                "PH",
		"Pakistan":                  "PK",
		"Bangladeş":                 "BD",
		"Suudi Arabistan":           "SA",
		"Birleşik Arap Emirlikleri": "AE",
		"İsrail":                    "IL",
		"Katar":                     "QA",
		"Kuveyt":                    "KW",
		"Umman":                     "OM",
		"Ürdün":                     "JO",
		"Lübnan":                    "LB",
		"Kazakistan":                "KZ",
		"Özbekistan":                "UZ",
		"Azerbaycan":                "AZ",
		"Gürcistan":                 "GE",
		"Ermenistan":                "AM",

		// Amerika
		"Kanada":              "CA",
		"Meksika":             "MX",
		"Brezilya":            "BR",
		"Arjantin":            "AR",
		"Şili":                "CL",
		"Kolombiya":           "CO",
		"Peru":                "PE",
		"Venezuela":           "VE",
		"Ekvador":             "EC",
		"Bolivya":             "BO",
		"Paraguay":            "PY",
		"Uruguay":             "UY",
		"Kosta Rika":          "CR",
		"Panama":              "PA",
		"Dominik Cumhuriyeti": "DO",
		"Küba":                "CU",

		// Afrika
		"Mısır":          "EG",
		"Güney Afrika":   "ZA",
		"Fas":            "MA",
		"Cezayir":        "DZ",
		"Tunus":          "TN",
		"Nijerya":        "NG",
		"Kenya":          "KE",
		"Etiyopya":       "ET",
		"Gana":           "GH",
		"Fildişi Sahili": "CI",
		"Senegal":        "SN",
		"Uganda":         "UG",
		"Tanzanya":       "TZ",
		"Angola":         "AO",
		"Zimbabve":       "ZW",
		"Kamerun":        "CM",

		// Okyanusya
		"Avustralya":      "AU",
		"Yeni Zelanda":    "NZ",
		"Fiji":            "FJ",
		"Papua Yeni Gine": "PG",

		// Diğer
		"Diğer": "OT",
	}

	// Büyük/küçük harf duyarlılığını kaldır
	normalizedName := strings.TrimSpace(countryName)

	if code, ok := countryMap[normalizedName]; ok {
		return code
	}

	// Alternatif: Büyük harfe çevirerek de dene
	normalizedName = strings.Title(strings.ToLower(normalizedName))
	if code, ok := countryMap[normalizedName]; ok {
		return code
	}

	return countryName // Bulunamazsa orijinal değeri döndür
}

// AdminMachines API - Makine listesi (JSON)
func AdminMachines(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		difficulty := r.URL.Query().Get("difficulty")

		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 20
		}

		query := `
            SELECT m.id, m.name, m.description, m.difficulty, m.points_reward,
                   m.is_vip_only, m.is_active, m.created_at,
                   COALESCE(u.username, 'System') as creator,
                   COUNT(DISTINCT mq.id) as question_count,
                   COUNT(DISTINCT s.id) as submission_count
            FROM machines m
            LEFT JOIN users u ON m.creator_id = u.id
            LEFT JOIN machine_questions mq ON m.id = mq.machine_id
            LEFT JOIN submissions s ON m.id = s.machine_id
            WHERE 1=1
        `
		var args []interface{}
		argCount := 1

		if difficulty != "" && difficulty != "all" {
			query += ` AND m.difficulty = $` + strconv.Itoa(argCount)
			args = append(args, difficulty)
			argCount++
		}

		query += ` GROUP BY m.id, u.username`

		// Toplam sayı
		var total int
		countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count"
		db.QueryRow(countQuery, args...).Scan(&total)

		// Sayfalama
		query += ` ORDER BY m.created_at DESC 
                   LIMIT $` + strconv.Itoa(argCount) + ` 
                   OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var machines []map[string]interface{}
		for rows.Next() {
			var m struct {
				ID              int
				Name            string
				Description     string
				Difficulty      string
				PointsReward    int
				IsVIPOnly       bool
				IsActive        bool
				CreatedAt       time.Time
				Creator         string
				QuestionCount   int
				SubmissionCount int
			}
			rows.Scan(
				&m.ID, &m.Name, &m.Description, &m.Difficulty, &m.PointsReward,
				&m.IsVIPOnly, &m.IsActive, &m.CreatedAt,
				&m.Creator, &m.QuestionCount, &m.SubmissionCount,
			)
			machines = append(machines, map[string]interface{}{
				"id":               m.ID,
				"name":             m.Name,
				"description":      m.Description,
				"difficulty":       m.Difficulty,
				"points_reward":    m.PointsReward,
				"is_vip_only":      m.IsVIPOnly,
				"is_active":        m.IsActive,
				"created_at":       m.CreatedAt,
				"creator":          m.Creator,
				"question_count":   m.QuestionCount,
				"submission_count": m.SubmissionCount,
			})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"machines": machines,
			"pagination": map[string]interface{}{
				"current_page": page,
				"total_pages":  (total + limit - 1) / limit,
				"total":        total,
				"limit":        limit,
			},
		})
	}
}

// AdminCreateMachine - Yeni makine oluştur API (Multipart form)
func AdminCreateMachine(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Multipart form verisini parse et (10 MB limit)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			log.Printf("Multipart form parse hatası: %v", err)
			http.Error(w, "Form verisi çok büyük", http.StatusBadRequest)
			return
		}

		// Session'dan admin ID'yi al
		session, _ := store.Get(r, "admin_session")
		adminID, ok := session.Values["admin_id"].(int)
		if !ok {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		// Form verilerini al
		name := r.FormValue("name")
		description := r.FormValue("description")
		difficulty := r.FormValue("difficulty")
		pointsReward, _ := strconv.Atoi(r.FormValue("points_reward"))

		// Checkbox değerleri
		isVIPOnly := r.FormValue("is_vip_only") == "true" || r.FormValue("is_vip_only") == "on"
		isActive := r.FormValue("is_active") == "true" || r.FormValue("is_active") == "on"
		dockerImage := r.FormValue("docker_image")

		// Zorunlu alanları kontrol et
		if name == "" || difficulty == "" {
			http.Error(w, "Makine adı ve zorluk seviyesi zorunludur", http.StatusBadRequest)
			return
		}

		// Görsel dosyasını işle (varsa)
		var imagePath string
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// Uploads dizininin var olduğundan emin ol
			uploadDir := "uploads/machines"
			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				log.Printf("Uploads dizini oluşturma hatası: %v", err)
				http.Error(w, "Dosya yükleme dizini oluşturulamadı", http.StatusInternalServerError)
				return
			}

			// Dosya tipini kontrol et
			contentType := header.Header.Get("Content-Type")
			allowedTypes := map[string]bool{
				"image/jpeg": true,
				"image/png":  true,
				"image/gif":  true,
				"image/webp": true,
			}

			if !allowedTypes[contentType] {
				log.Printf("Geçersiz dosya tipi: %s", contentType)
				http.Error(w, "Sadece JPEG, PNG, GIF ve WEBP dosyaları yüklenebilir", http.StatusBadRequest)
				return
			}

			// Dosya boyutunu kontrol et (2MB)
			if header.Size > 2*1024*1024 {
				http.Error(w, "Dosya boyutu 2MB'dan büyük olamaz", http.StatusBadRequest)
				return
			}

			// Benzersiz dosya adı oluştur
			ext := filepath.Ext(header.Filename)
			fileName := fmt.Sprintf("machine_%d_%s%s", time.Now().UnixNano(), name, ext)
			filePath := filepath.Join(uploadDir, fileName)

			// Dosyayı kaydet
			dst, err := os.Create(filePath)
			if err != nil {
				log.Printf("Dosya oluşturma hatası: %v", err)
				http.Error(w, "Dosya kaydedilemedi", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				log.Printf("Dosya kopyalama hatası: %v", err)
				http.Error(w, "Dosya kopyalanamadı", http.StatusInternalServerError)
				return
			}

			imagePath = "/uploads/machines/" + fileName
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction başlatma hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Makineyi ekle
		var machineID int
		err = tx.QueryRow(`
            INSERT INTO machines (
                name, description, difficulty, points_reward, 
                is_vip_only, docker_image, creator_id, image_url, is_active, created_at
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
            RETURNING id
        `, name, description, difficulty, pointsReward,
			isVIPOnly, dockerImage, adminID, imagePath, isActive).Scan(&machineID)

		if err != nil {
			log.Printf("Makine ekleme hatası: %v", err)
			http.Error(w, "Makine oluşturulamadı: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Soruları ekle
		questionTitles := r.Form["question_title[]"]
		questionDescs := r.Form["question_description[]"]
		questionPoints := r.Form["question_points[]"]
		questionFlags := r.Form["question_flag[]"]
		questionHints := r.Form["question_hint[]"]
		questionHintCosts := r.Form["question_hint_cost[]"]
		questionActives := r.Form["question_active[]"]

		for i := 0; i < len(questionTitles); i++ {
			if questionTitles[i] == "" {
				continue
			}

			// Flag'i hashle
			flagHash := ""
			if i < len(questionFlags) && questionFlags[i] != "" {
				hashed, err := bcrypt.GenerateFromPassword([]byte(questionFlags[i]), bcrypt.DefaultCost)
				if err != nil {
					log.Printf("Flag hashleme hatası: %v", err)
					continue
				}
				flagHash = string(hashed)
			}

			points := 100
			if i < len(questionPoints) && questionPoints[i] != "" {
				points, _ = strconv.Atoi(questionPoints[i])
			}

			hintCost := 10
			if i < len(questionHintCosts) && questionHintCosts[i] != "" {
				hintCost, _ = strconv.Atoi(questionHintCosts[i])
			}

			isActive := true
			if i < len(questionActives) {
				isActive = questionActives[i] == "true" || questionActives[i] == "on"
			}

			_, err = tx.Exec(`
                INSERT INTO machine_questions (
                    machine_id, question_order, title, description, flag_hash, 
                    points_reward, hint, hint_cost, is_active, created_at
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
            `, machineID, i+1, questionTitles[i], questionDescs[i], flagHash,
				points, questionHints[i], hintCost, isActive)

			if err != nil {
				log.Printf("Soru ekleme hatası: %v", err)
				http.Error(w, "Sorular eklenirken hata oluştu", http.StatusInternalServerError)
				return
			}
		}

		err = tx.Commit()
		if err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			http.Error(w, "İşlem tamamlanamadı", http.StatusInternalServerError)
			return
		}

		log.Printf("Yeni makine oluşturuldu: ID=%d, Name=%s", machineID, name)

		// Başarılı - makine listesine yönlendir
		http.Redirect(w, r, "/admin/machines?success=created", http.StatusSeeOther)
	}
}

// AdminUpdateMachine - Makine güncelle API (Multipart form)
func AdminUpdateMachine(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineID := vars["id"]

		// Multipart form verisini parse et
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			log.Printf("Multipart form parse hatası: %v", err)
			http.Error(w, "Form verisi çok büyük", http.StatusBadRequest)
			return
		}

		// Form verilerini al
		name := r.FormValue("name")
		description := r.FormValue("description")
		difficulty := r.FormValue("difficulty")
		pointsReward, _ := strconv.Atoi(r.FormValue("points_reward"))

		// Checkbox değerleri
		isVIPOnly := r.FormValue("is_vip_only") == "true" || r.FormValue("is_vip_only") == "on"
		isActive := r.FormValue("is_active") == "true" || r.FormValue("is_active") == "on"
		dockerImage := r.FormValue("docker_image")

		// Zorunlu alanları kontrol et
		if name == "" || difficulty == "" {
			http.Error(w, "Makine adı ve zorluk seviyesi zorunludur", http.StatusBadRequest)
			return
		}

		// Mevcut image_url'yi al
		var currentImageURL string
		err := db.QueryRow("SELECT COALESCE(image_url, '') FROM machines WHERE id = $1", machineID).Scan(&currentImageURL)
		if err != nil {
			log.Printf("Mevcut görsel URL alınamadı: %v", err)
			http.Error(w, "Makine bulunamadı", http.StatusNotFound)
			return
		}

		// Yeni görsel var mı kontrol et
		imageURL := currentImageURL
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// Uploads dizininin var olduğundan emin ol
			uploadDir := "uploads/machines"
			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				log.Printf("Uploads dizini oluşturma hatası: %v", err)
				http.Error(w, "Dosya yükleme dizini oluşturulamadı", http.StatusInternalServerError)
				return
			}

			// Dosya tipini kontrol et
			buf := make([]byte, 512)
			if _, err := file.Read(buf); err != nil {
				http.Error(w, "Dosya okunamadı", http.StatusInternalServerError)
				return
			}
			file.Seek(0, io.SeekStart)

			contentType := http.DetectContentType(buf)
			allowedTypes := map[string]bool{
				"image/jpeg": true,
				"image/png":  true,
				"image/gif":  true,
				"image/webp": true,
			}
			if !allowedTypes[contentType] {
				http.Error(w, "Sadece JPEG, PNG, GIF ve WEBP dosyaları yüklenebilir", http.StatusBadRequest)
				return
			}

			// Dosya boyutunu kontrol et (2MB)
			if header.Size > 2*1024*1024 {
				http.Error(w, "Dosya boyutu 2MB'dan büyük olamaz", http.StatusBadRequest)
				return
			}

			// Benzersiz dosya adı oluştur
			ext := filepath.Ext(header.Filename)
			fileName := fmt.Sprintf("machine_%d%s", time.Now().UnixNano(), ext)
			filePath := filepath.Join(uploadDir, fileName)

			// Dosyayı kaydet
			dst, err := os.Create(filePath)
			if err != nil {
				log.Printf("Dosya oluşturma hatası: %v", err)
				http.Error(w, "Dosya kaydedilemedi", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				log.Printf("Dosya kopyalama hatası: %v", err)
				http.Error(w, "Dosya kopyalanamadı", http.StatusInternalServerError)
				return
			}

			// Eski görseli sil (varsa)
			if currentImageURL != "" {
				oldPath := strings.TrimPrefix(currentImageURL, "/")
				if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
					log.Printf("Eski görsel silinemedi: %v", err)
				}
			}

			imageURL = "/uploads/machines/" + fileName
		} else {
			log.Printf("Görsel yüklenmedi, mevcut görsel korunacak: %s", currentImageURL)
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction başlatma hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Makineyi güncelle
		_, err = tx.Exec(`
            UPDATE machines SET 
                name = $1, description = $2, difficulty = $3, 
                points_reward = $4, is_vip_only = $5, docker_image = $6,
                is_active = $7, image_url = $8, updated_at = NOW()
            WHERE id = $9
        `, name, description, difficulty, pointsReward, isVIPOnly, dockerImage, isActive, imageURL, machineID)

		if err != nil {
			log.Printf("Makine güncelleme hatası: %v", err)
			http.Error(w, "Makine güncellenemedi", http.StatusInternalServerError)
			return
		}

		// Mevcut soruları güncelle ve yenilerini ekle
		questionIDs := r.Form["question_id[]"]
		questionTitles := r.Form["question_title[]"]
		questionDescs := r.Form["question_description[]"]
		questionPoints := r.Form["question_points[]"]
		questionFlags := r.Form["question_flag[]"]
		questionHints := r.Form["question_hint[]"]
		questionHintCosts := r.Form["question_hint_cost[]"]

		// Her soru için işlem yap
		for i := 0; i < len(questionTitles); i++ {
			if questionTitles[i] == "" {
				continue
			}

			points := 100
			if i < len(questionPoints) && questionPoints[i] != "" {
				points, _ = strconv.Atoi(questionPoints[i])
			}

			hintCost := 10
			if i < len(questionHintCosts) && questionHintCosts[i] != "" {
				hintCost, _ = strconv.Atoi(questionHintCosts[i])
			}

			// Soru ID'sine göre güncelle veya ekle
			if i < len(questionIDs) && questionIDs[i] != "" && questionIDs[i] != "new" {
				// Mevcut soruyu güncelle
				qID, _ := strconv.Atoi(questionIDs[i])

				// Flag değişti mi kontrol et
				if i < len(questionFlags) && questionFlags[i] != "" {
					hashed, err := bcrypt.GenerateFromPassword([]byte(questionFlags[i]), bcrypt.DefaultCost)
					if err == nil {
						_, err = tx.Exec(`
                            UPDATE machine_questions SET 
                                title = $1, description = $2, points_reward = $3,
                                hint = $4, hint_cost = $5, flag_hash = $6
                            WHERE id = $7
                        `, questionTitles[i], questionDescs[i], points,
							questionHints[i], hintCost, string(hashed), qID)
					}
				} else {
					// Flag değişmedi
					_, err = tx.Exec(`
                        UPDATE machine_questions SET 
                            title = $1, description = $2, points_reward = $3,
                            hint = $4, hint_cost = $5
                        WHERE id = $6
                    `, questionTitles[i], questionDescs[i], points,
						questionHints[i], hintCost, qID)
				}
			} else {
				// Yeni soru ekle
				if i < len(questionFlags) && questionFlags[i] != "" {
					hashed, err := bcrypt.GenerateFromPassword([]byte(questionFlags[i]), bcrypt.DefaultCost)
					if err == nil {
						_, err = tx.Exec(`
                            INSERT INTO machine_questions (
                                machine_id, question_order, title, description, flag_hash,
                                points_reward, hint, hint_cost, is_active, created_at
                            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, NOW())
                        `, machineID, i+1, questionTitles[i], questionDescs[i], string(hashed),
							points, questionHints[i], hintCost)
					}
				}
			}

			if err != nil {
				log.Printf("Soru işleme hatası: %v", err)
			}
		}

		err = tx.Commit()
		if err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			http.Error(w, "İşlem tamamlanamadı", http.StatusInternalServerError)
			return
		}

		log.Printf("Makine güncellendi: ID=%s", machineID)

		// Başarılı - makine listesine yönlendir
		http.Redirect(w, r, "/admin/machines?success=updated", http.StatusSeeOther)
	}
}

// AdminToggleMachine API - Makine durumunu değiştir
func AdminToggleMachine(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineID := vars["id"]

		_, err := db.Exec("UPDATE machines SET is_active = NOT is_active WHERE id = $1", machineID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Makine durumu güncellendi",
		})
	}
}

// AdminDeleteMachine API - Makine sil
func AdminDeleteMachine(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineID := vars["id"]

		_, err := db.Exec("DELETE FROM machines WHERE id = $1", machineID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Makine silindi",
		})
	}
}

// AdminQuestions API - Soru listesi (JSON)
func AdminQuestions(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Önce Content-Type header'ını ayarla
		w.Header().Set("Content-Type", "application/json")

		machineID := r.URL.Query().Get("machine_id")

		query := `
            SELECT q.id, q.machine_id, q.question_order, q.title, 
                   q.description, q.points_reward, q.hint, q.hint_cost, q.is_active,
                   m.name as machine_name,
                   COUNT(s.id) as submission_count,
                   COUNT(CASE WHEN s.status = 'accepted' THEN 1 END) as accepted_count
            FROM machine_questions q
            JOIN machines m ON q.machine_id = m.id
            LEFT JOIN submissions s ON q.id = s.question_id
            WHERE 1=1
        `
		var args []interface{}
		argCount := 1

		if machineID != "" && machineID != "all" {
			query += ` AND q.machine_id = $` + strconv.Itoa(argCount)
			args = append(args, machineID)
			argCount++
		}

		query += ` GROUP BY q.id, m.id ORDER BY q.machine_id, q.question_order`

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("AdminQuestions sorgu hatası: %v", err)
			// Hata durumunda geçerli JSON döndür
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var questions []map[string]interface{}
		for rows.Next() {
			var q struct {
				ID              int
				MachineID       int
				QuestionOrder   int
				Title           string
				Description     string
				PointsReward    int
				Hint            string
				HintCost        int
				IsActive        bool
				MachineName     string
				SubmissionCount int
				AcceptedCount   int
			}

			err := rows.Scan(
				&q.ID, &q.MachineID, &q.QuestionOrder, &q.Title,
				&q.Description, &q.PointsReward, &q.Hint, &q.HintCost, &q.IsActive,
				&q.MachineName, &q.SubmissionCount, &q.AcceptedCount,
			)

			if err != nil {
				log.Printf("AdminQuestions satır okuma hatası: %v", err)
				continue
			}

			// Başarı oranını hesapla (sıfıra bölme hatasını önle)
			successRate := 0.0
			if q.SubmissionCount > 0 {
				successRate = float64(q.AcceptedCount) / float64(q.SubmissionCount) * 100
			}

			questions = append(questions, map[string]interface{}{
				"id":               q.ID,
				"machine_id":       q.MachineID,
				"machine_name":     q.MachineName,
				"question_order":   q.QuestionOrder,
				"title":            q.Title,
				"description":      q.Description,
				"points_reward":    q.PointsReward,
				"hint":             q.Hint,
				"hint_cost":        q.HintCost,
				"is_active":        q.IsActive,
				"submission_count": q.SubmissionCount,
				"accepted_count":   q.AcceptedCount,
				"success_rate":     successRate,
			})
		}

		// rows.Err() kontrolü
		if err = rows.Err(); err != nil {
			log.Printf("AdminQuestions rows hatası: %v", err)
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}

		// Boş dizi gönder (nil yerine)
		if questions == nil {
			questions = make([]map[string]interface{}, 0)
		}

		// JSON olarak gönder
		if err := json.NewEncoder(w).Encode(questions); err != nil {
			log.Printf("AdminQuestions JSON encode hatası: %v", err)
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}

		log.Printf("AdminQuestions: %d soru gönderildi", len(questions))
	}
}

// AdminCreateQuestion API - Yeni soru oluştur
func AdminCreateQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var requestData struct {
			MachineID     int    `json:"machine_id"`
			Title         string `json:"title"`
			Description   string `json:"description"`
			Flag          string `json:"flag"`
			FlagHash      string `json:"flag_hash"`
			PointsReward  int    `json:"points_reward"`
			Hint          string `json:"hint"`
			HintCost      int    `json:"hint_cost"`
			IsActive      bool   `json:"is_active"`
			QuestionOrder int    `json:"question_order"`
		}

		if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
			http.Error(w, "Geçersiz istek", http.StatusBadRequest)
			return
		}

		// Flag Hashleme
		flagHash := requestData.FlagHash
		if requestData.Flag != "" {
			hashed, err := bcrypt.GenerateFromPassword([]byte(requestData.Flag), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "Flag hashlenemedi", http.StatusInternalServerError)
				return
			}
			flagHash = string(hashed)
		}

		var questionID int
		var finalOrder int

		// TEK BİR SORGU: Hem sıra numarasını hesapla hem de kaydı yap
		err := db.QueryRow(`
            INSERT INTO machine_questions (
                machine_id, question_order, title, description, flag_hash, 
                points_reward, hint, hint_cost, is_active 
            ) VALUES (
                $1, 
                CASE 
                    WHEN $2 > 0 THEN $2 
                    ELSE (SELECT COALESCE(MAX(question_order), 0) + 1 FROM machine_questions WHERE machine_id = $1)
                END, 
                $3, $4, $5, $6, $7, $8, $9
            )
            RETURNING id, question_order
        `,
			requestData.MachineID,
			requestData.QuestionOrder,
			requestData.Title,
			requestData.Description,
			flagHash,
			requestData.PointsReward,
			requestData.Hint,
			requestData.HintCost,
			requestData.IsActive,
		).Scan(&questionID, &finalOrder)

		if err != nil {
			log.Printf("Veritabanı kayıt hatası: %v", err)
			// Eğer hala unique kısıtlaması hatası gelirse kullanıcıya bildir
			http.Error(w, "Soru oluşturulamadı: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Başarılı yanıt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"question_id": questionID,
			"order":       finalOrder,
			"message":     "Soru başarıyla oluşturuldu",
		})
	}
}

// AdminUpdateQuestion API - Soru güncelle (JSON)
func AdminUpdateQuestion(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		questionIDStr := vars["id"] // Bu string
		fmt.Println("hello")
		// String'i int'e çevir
		questionID, err := strconv.Atoi(questionIDStr)
		if err != nil {
			http.Error(w, "Geçersiz soru ID", http.StatusBadRequest)
			return
		}

		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Geçersiz istek", http.StatusBadRequest)
			return
		}

		// İzin verilen alanlar
		allowedFields := []string{
			"title", "description", "flag_hash", "points_reward",
			"hint", "hint_cost", "is_active", "question_order",
		}

		// Güncelleme sorgusunu oluştur
		query := "UPDATE machine_questions SET "
		var args []interface{}
		argCount := 1

		for _, field := range allowedFields {
			if val, ok := updates[field]; ok {
				if argCount > 1 {
					query += ", "
				}
				query += field + " = $" + strconv.Itoa(argCount)
				args = append(args, val)
				argCount++
			}
		}

		// Eğer flag güncellenmişse hash'le
		if flag, ok := updates["flag"]; ok && flag != "" {
			if argCount > 1 {
				query += ", "
			}
			hashedFlag, err := bcrypt.GenerateFromPassword([]byte(flag.(string)), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "Flag hashlenemedi", http.StatusInternalServerError)
				return
			}
			query += "flag_hash = $" + strconv.Itoa(argCount)
			args = append(args, string(hashedFlag))
			argCount++
		}

		// Güncellenecek alan yoksa hata döndür
		if argCount == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Güncellenecek alan bulunamadı",
			})
			return
		}

		// WHERE koşulunu ekle
		query += " WHERE id = $" + strconv.Itoa(argCount)
		args = append(args, questionID) // int olarak kullan

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Sorguyu çalıştır
		result, err := tx.Exec(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Etkilenen satır sayısını kontrol et
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Soru bulunamadı",
			})
			return
		}

		// Log kaydı ekle
		//session, _ := store.Get(r, "admin_session")
		//adminID := session.Values["admin_id"].(int)
		//adminUsername := session.Values["username"].(string)

		// _, err = tx.Exec(`
		//     INSERT INTO admin_logs (admin_id, action_type, target_id, details, created_at)
		//     VALUES ($1, $2, $3, $4, NOW())
		// `, adminID, "UPDATE_QUESTION", questionID, // questionID int
		// 	adminUsername+" soru #"+strconv.Itoa(questionID)+" güncelledi") // Burada int kullan

		// if err != nil {
		// 	// Log hatası önemli değil, devam et
		// 	log.Println("Log kaydı eklenemedi:", err)
		// }

		// Transaction'ı commit et
		err = tx.Commit()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Başarılı yanıt
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Soru başarıyla güncellendi",
		})
	}
}

// AdminToggleQuestion API - Soru durumunu değiştir
func AdminToggleQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		questionID := vars["id"]

		_, err := db.Exec("UPDATE machine_questions SET is_active = NOT is_active WHERE id = $1", questionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Soru durumu güncellendi",
		})
	}
}

// AdminDeleteQuestion API - Soru sil
func AdminDeleteQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		questionID := vars["id"]

		_, err := db.Exec("DELETE FROM machine_questions WHERE id = $1", questionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Soru silindi",
		})
	}
}

// AdminGetQuestion - Tekil soru getir (GET /admin/api/questions/{id})
func AdminGetQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		w.Header().Set("Content-Type", "application/json")

		var question struct {
			ID            int    `json:"id"`
			MachineID     int    `json:"machine_id"`
			QuestionOrder int    `json:"question_order"`
			Title         string `json:"title"`
			Description   string `json:"description"`
			PointsReward  int    `json:"points_reward"`
			Hint          string `json:"hint"`
			HintCost      int    `json:"hint_cost"`
			IsActive      bool   `json:"is_active"`
			FlagHash      string `json:"flag_hash"`
		}

		err := db.QueryRow(`
            SELECT id, machine_id, question_order, title, description, 
                   points_reward, hint, hint_cost, is_active, flag_hash
            FROM machine_questions 
            WHERE id = $1
        `, id).Scan(
			&question.ID, &question.MachineID, &question.QuestionOrder,
			&question.Title, &question.Description, &question.PointsReward,
			&question.Hint, &question.HintCost, &question.IsActive,
			&question.FlagHash,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error": "Soru bulunamadı"}`, http.StatusNotFound)
			} else {
				http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
			}
			return
		}

		json.NewEncoder(w).Encode(question)
	}
}

// AdminGetMachineQuestions - Belirli bir makineye ait soruları getirir
func AdminGetMachineQuestions(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineID := vars["id"]

		w.Header().Set("Content-Type", "application/json")

		// Makineye ait soruları sorgula
		rows, err := db.Query(`
            SELECT 
                id, machine_id, question_order, title, description, 
                flag_hash, points_reward, hint, hint_cost, is_active
            FROM machine_questions 
            WHERE machine_id = $1
            ORDER BY question_order ASC
        `, machineID)

		if err != nil {
			http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var questions []models.Question

		for rows.Next() {
			var q models.Question
			var hint sql.NullString

			err := rows.Scan(
				&q.ID, &q.MachineID, &q.QuestionOrder, &q.Title, &q.Description,
				&q.FlagHash, &q.PointsReward, &hint, &q.HintCost, &q.IsActive,
			)
			if err != nil {
				continue
			}

			// NullString kontrolü
			if hint.Valid {
				q.Hint = hint.String
			}

			// Kullanıcıya özel alanları boş gönder (admin paneli için gerekli değil)
			q.Solved = false
			q.HintUsed = false
			q.SolveCount = 0

			questions = append(questions, q)
		}

		// Eğer soru yoksa boş array dön
		if questions == nil {
			questions = make([]models.Question, 0)
		}

		json.NewEncoder(w).Encode(questions)
	}
}

// AdminVIPPlans API - VIP planları listesi

// DailySolve - Günlük çözüm istatistiği

// AdminSystemStats - Ana sistem istatistikleri
func AdminSystemStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var stats struct {
			TotalUsers       int     `json:"total_users"`
			NewUsersToday    int     `json:"new_users_today"`
			ActiveUsers      int     `json:"active_users"`
			TotalMachines    int     `json:"total_machines"`
			TotalSubmissions int     `json:"total_submissions"`
			SubmissionsToday int     `json:"submissions_today"`
			TotalVIPUsers    int     `json:"total_vip_users"`
			VIPRevenue       float64 `json:"vip_revenue"`
			AveragePoints    float64 `json:"average_points"`
			TopUserPoints    int     `json:"top_user_points"`
			SuccessRate      float64 `json:"success_rate"`
		}

		// Toplam kullanıcı
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_active = true`).Scan(&stats.TotalUsers)

		// Bugün kaydolanlar
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE DATE(created_at) = CURRENT_DATE`).Scan(&stats.NewUsersToday)

		// Aktif kullanıcılar (son 24 saat)
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE last_login > NOW() - INTERVAL '24 hours'`).Scan(&stats.ActiveUsers)

		// Toplam makine
		db.QueryRow(`SELECT COUNT(*) FROM machines WHERE is_active = true`).Scan(&stats.TotalMachines)

		// Toplam çözüm
		db.QueryRow(`SELECT COUNT(*) FROM submissions`).Scan(&stats.TotalSubmissions)

		// Bugünkü çözümler
		db.QueryRow(`SELECT COUNT(*) FROM submissions WHERE DATE(created_at) = CURRENT_DATE`).Scan(&stats.SubmissionsToday)

		// Toplam VIP kullanıcı
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_vip = true`).Scan(&stats.TotalVIPUsers)

		// VIP geliri
		db.QueryRow(`SELECT COALESCE(SUM(price), 0) FROM vip_purchases`).Scan(&stats.VIPRevenue)

		// Ortalama puan
		db.QueryRow(`SELECT COALESCE(AVG(points), 0) FROM users`).Scan(&stats.AveragePoints)

		// En yüksek puan
		db.QueryRow(`SELECT COALESCE(MAX(points), 0) FROM users`).Scan(&stats.TopUserPoints)

		// Başarı oranı
		var accepted, total int
		db.QueryRow(`SELECT COUNT(*) FROM submissions WHERE status = 'accepted'`).Scan(&accepted)
		db.QueryRow(`SELECT COUNT(*) FROM submissions`).Scan(&total)
		if total > 0 {
			stats.SuccessRate = float64(accepted) / float64(total) * 100
		}

		json.NewEncoder(w).Encode(stats)
	}
}

// AdminMachineStats - Belirli bir makine için detaylı istatistikler
func AdminMachineStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineID := vars["id"]

		w.Header().Set("Content-Type", "application/json")

		var stats struct {
			TotalSolves    int     `json:"total_solves"`
			TotalAttempts  int     `json:"total_attempts"`
			SuccessRate    float64 `json:"success_rate"`
			AvgSolveTime   float64 `json:"avg_solve_time"`
			FirstBlood     string  `json:"first_blood"`
			FirstBloodTime string  `json:"first_blood_time"`
			LastSolve      string  `json:"last_solve"`
		}

		// Toplam çözen kişi
		db.QueryRow(`
			SELECT COUNT(DISTINCT user_id) 
			FROM submissions 
			WHERE machine_id = $1 AND status = 'accepted'
		`, machineID).Scan(&stats.TotalSolves)

		// Toplam deneme
		db.QueryRow(`
			SELECT COUNT(*) 
			FROM submissions 
			WHERE machine_id = $1
		`, machineID).Scan(&stats.TotalAttempts)

		// Başarı oranı
		if stats.TotalAttempts > 0 {
			stats.SuccessRate = float64(stats.TotalSolves) / float64(stats.TotalAttempts) * 100
		}

		// Ortalama çözüm süresi (dakika)
		db.QueryRow(`
			SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (solved_at - created_at)) / 60), 0)
			FROM user_solutions 
			WHERE machine_id = $1
		`, machineID).Scan(&stats.AvgSolveTime)

		// First blood
		var firstBloodUser sql.NullString
		var firstBloodTime sql.NullTime
		err := db.QueryRow(`
			SELECT u.username, s.created_at
			FROM submissions s
			JOIN users u ON s.user_id = u.id
			WHERE s.machine_id = $1 AND s.status = 'accepted'
			ORDER BY s.created_at ASC
			LIMIT 1
		`, machineID).Scan(&firstBloodUser, &firstBloodTime)

		if err == nil {
			stats.FirstBlood = firstBloodUser.String
			if firstBloodTime.Valid {
				stats.FirstBloodTime = firstBloodTime.Time.Format("2006-01-02 15:04:05")
			}
		}

		// Son çözüm
		var lastSolveTime sql.NullTime
		err = db.QueryRow(`
			SELECT created_at
			FROM submissions 
			WHERE machine_id = $1 AND status = 'accepted'
			ORDER BY created_at DESC
			LIMIT 1
		`, machineID).Scan(&lastSolveTime)

		if err == nil && lastSolveTime.Valid {
			stats.LastSolve = lastSolveTime.Time.Format("2006-01-02 15:04:05")
		}

		json.NewEncoder(w).Encode(stats)
	}
}

// AdminDashboardStats - Dashboard için kullanıcı istatistikleri
func AdminDashboardStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var stats struct {
			TotalPoints        int     `json:"total_points"`
			TotalSolved        int     `json:"total_solved"`
			TotalMachines      int     `json:"total_machines"`
			TotalMachinesCount int     `json:"total_machines_count"`
			Rank               float64 `json:"rank"`
			DailyGoal          int     `json:"daily_goal"`
			DailyProgress      int     `json:"daily_progress"`
			Streak             int     `json:"streak"`
			VIPCount           int     `json:"vip_count"`
		}

		// Toplam puan
		db.QueryRow(`SELECT COALESCE(SUM(points), 0) FROM users`).Scan(&stats.TotalPoints)

		// Toplam çözülen soru
		db.QueryRow(`SELECT COUNT(*) FROM user_solutions`).Scan(&stats.TotalSolved)

		// Çözülen benzersiz makine
		db.QueryRow(`SELECT COUNT(DISTINCT machine_id) FROM user_solutions`).Scan(&stats.TotalMachines)

		// Toplam makine
		db.QueryRow(`SELECT COUNT(*) FROM machines WHERE is_active = true`).Scan(&stats.TotalMachinesCount)

		// Ortalama rank
		db.QueryRow(`SELECT COALESCE(AVG(rank), 0) FROM users WHERE rank > 0`).Scan(&stats.Rank)

		// Günlük hedef
		stats.DailyGoal = 10
		db.QueryRow(`
			SELECT COUNT(*) FROM user_solutions 
			WHERE DATE(solved_at) = CURRENT_DATE
		`).Scan(&stats.DailyProgress)

		// Streak - son 7 günde çözüm yapanlar
		db.QueryRow(`
			SELECT COUNT(DISTINCT user_id) 
			FROM user_solutions 
			WHERE solved_at > NOW() - INTERVAL '7 days'
		`).Scan(&stats.Streak)

		// VIP sayısı
		db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_vip = true`).Scan(&stats.VIPCount)

		json.NewEncoder(w).Encode(stats)
	}
}

// AdminProfileStats - Kullanıcı profili istatistikleri
func AdminProfileStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		userID := vars["id"]

		w.Header().Set("Content-Type", "application/json")

		var stats struct {
			TotalMachines   int     `json:"total_machines"`
			TotalQuestions  int     `json:"total_questions"`
			TotalPoints     int     `json:"total_points"`
			Rank            int     `json:"rank"`
			Accuracy        float64 `json:"accuracy"`
			FirstBloods     int     `json:"first_bloods"`
			VIPCount        int     `json:"vip_count"`
			HintUsedCount   int     `json:"hint_used_count"`
			SubmissionCount int     `json:"submission_count"`
			SuccessRate     float64 `json:"success_rate"`
			MemberDays      int     `json:"member_days"`
		}

		// Çözülen makine sayısı
		db.QueryRow(`
			SELECT COUNT(DISTINCT machine_id) 
			FROM user_solutions 
			WHERE user_id = $1
		`, userID).Scan(&stats.TotalMachines)

		// Çözülen soru sayısı
		db.QueryRow(`
			SELECT COUNT(*) 
			FROM user_solutions 
			WHERE user_id = $1
		`, userID).Scan(&stats.TotalQuestions)

		// Toplam puan
		db.QueryRow(`SELECT points FROM users WHERE id = $1`, userID).Scan(&stats.TotalPoints)

		// Rank
		db.QueryRow(`SELECT rank FROM users WHERE id = $1`, userID).Scan(&stats.Rank)

		// Doğruluk oranı
		var accepted, total int
		db.QueryRow(`
			SELECT COUNT(*) FROM submissions 
			WHERE user_id = $1 AND status = 'accepted'
		`, userID).Scan(&accepted)
		db.QueryRow(`
			SELECT COUNT(*) FROM submissions 
			WHERE user_id = $1
		`, userID).Scan(&total)

		if total > 0 {
			stats.Accuracy = float64(accepted) / float64(total) * 100
		}

		// First blood sayısı
		db.QueryRow(`
			SELECT COUNT(*) FROM (
				SELECT DISTINCT ON (machine_id) machine_id
				FROM submissions s
				WHERE user_id = $1 AND status = 'accepted'
				AND NOT EXISTS (
					SELECT 1 FROM submissions s2
					WHERE s2.machine_id = s.machine_id
					AND s2.status = 'accepted'
					AND s2.created_at < s.created_at
				)
			) as first_bloods
		`, userID).Scan(&stats.FirstBloods)

		// İpucu kullanma sayısı
		db.QueryRow(`
			SELECT COUNT(*) FROM hint_usage 
			WHERE user_id = $1
		`, userID).Scan(&stats.HintUsedCount)

		// Toplam submission
		db.QueryRow(`
			SELECT COUNT(*) FROM submissions 
			WHERE user_id = $1
		`, userID).Scan(&stats.SubmissionCount)

		// Başarı oranı
		if stats.SubmissionCount > 0 {
			stats.SuccessRate = float64(accepted) / float64(stats.SubmissionCount) * 100
		}

		// Üyelik günü
		var createdAt time.Time
		db.QueryRow(`SELECT created_at FROM users WHERE id = $1`, userID).Scan(&createdAt)
		stats.MemberDays = int(time.Since(createdAt).Hours() / 24)

		json.NewEncoder(w).Encode(stats)
	}
}

// AdminUsersWithStats - İstatistiklerle birlikte kullanıcı listesi
func AdminUsersWithStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query(`
			SELECT 
				u.id, u.username, u.email, COALESCE(u.avatar, '') as avatar,
				COALESCE(u.country, '') as country, u.points, u.is_vip,
				u.created_at, u.last_login, u.is_active,
				COALESCE(solved.solved_count, 0) as solved_count,
				COALESCE(submissions.submission_count, 0) as submission_count,
				COALESCE(accepted.accepted_count, 0) as accepted_count
			FROM users u
			LEFT JOIN (
				SELECT user_id, COUNT(*) as solved_count
				FROM user_solutions
				GROUP BY user_id
			) solved ON u.id = solved.user_id
			LEFT JOIN (
				SELECT user_id, COUNT(*) as submission_count
				FROM submissions
				GROUP BY user_id
			) submissions ON u.id = submissions.user_id
			LEFT JOIN (
				SELECT user_id, COUNT(*) as accepted_count
				FROM submissions
				WHERE status = 'accepted'
				GROUP BY user_id
			) accepted ON u.id = accepted.user_id
			ORDER BY u.points DESC
		`)

		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var users []map[string]interface{}
		for rows.Next() {
			var id, points, solvedCount, submissionCount, acceptedCount int
			var username, email, avatar, country string
			var isVip, isActive bool
			var createdAt, lastLogin sql.NullTime

			err := rows.Scan(&id, &username, &email, &avatar, &country, &points, &isVip,
				&createdAt, &lastLogin, &isActive, &solvedCount, &submissionCount, &acceptedCount)
			if err != nil {
				continue
			}

			successRate := 0.0
			if submissionCount > 0 {
				successRate = float64(acceptedCount) / float64(submissionCount) * 100
			}

			user := map[string]interface{}{
				"id":               id,
				"username":         username,
				"email":            email,
				"avatar":           avatar,
				"country":          country,
				"points":           points,
				"is_vip":           isVip,
				"is_active":        isActive,
				"solved_count":     solvedCount,
				"submission_count": submissionCount,
				"success_rate":     successRate,
				"member_since":     "",
			}

			if createdAt.Valid {
				user["member_since"] = createdAt.Time.Format("2006-01-02")
			}
			if lastLogin.Valid {
				user["last_login"] = lastLogin.Time.Format("2006-01-02 15:04:05")
			}

			users = append(users, user)
		}

		json.NewEncoder(w).Encode(users)
	}
}

// API: Makine soruları

// Yardımcı fonksiyonlar
func getActivityData(db *sql.DB, days int) ([]string, []int, []int) {
	rows, err := db.Query(`
		SELECT 
			TO_CHAR(date, 'DD/MM') as date,
			COALESCE(users.new_count, 0) as new_users,
			COALESCE(subs.sub_count, 0) as submissions
		FROM generate_series(
			CURRENT_DATE - INTERVAL '1 day' * ($1 - 1), 
			CURRENT_DATE, 
			'1 day'
		) AS date
		LEFT JOIN (
			SELECT DATE(created_at) as day, COUNT(*) as new_count
			FROM users
			GROUP BY day
		) users ON date = users.day
		LEFT JOIN (
			SELECT DATE(created_at) as day, COUNT(*) as sub_count
			FROM submissions
			GROUP BY day
		) subs ON date = subs.day
		ORDER BY date
	`, days)

	if err != nil {
		return []string{}, []int{}, []int{}
	}
	defer rows.Close()

	var labels []string
	var users []int
	var submissions []int

	for rows.Next() {
		var label string
		var user, sub int
		rows.Scan(&label, &user, &sub)
		labels = append(labels, label)
		users = append(users, user)
		submissions = append(submissions, sub)
	}

	return labels, users, submissions
}

func getDifficultyDistribution(db *sql.DB) ([]string, []int) {
	rows, err := db.Query(`
		SELECT 
			CASE 
				WHEN difficulty = 'easy' THEN 'Kolay'
				WHEN difficulty = 'medium' THEN 'Orta'
				WHEN difficulty = 'hard' THEN 'Zor'
				WHEN difficulty = 'expert' THEN 'Uzman'
				ELSE 'Diğer'
			END as difficulty,
			COUNT(*) as count
		FROM machines 
		WHERE is_active = true 
		GROUP BY difficulty
	`)

	if err != nil {
		return []string{}, []int{}
	}
	defer rows.Close()

	var labels []string
	var data []int
	for rows.Next() {
		var label string
		var count int
		rows.Scan(&label, &count)
		labels = append(labels, label)
		data = append(data, count)
	}

	return labels, data
}

func getPopularMachines(db *sql.DB) ([]string, []int) {
	rows, err := db.Query(`
		SELECT m.name, COUNT(s.id) as solve_count
		FROM machines m
		LEFT JOIN submissions s ON m.id = s.machine_id AND s.status = 'accepted'
		WHERE m.is_active = true
		GROUP BY m.id, m.name
		ORDER BY solve_count DESC
		LIMIT 5
	`)

	if err != nil {
		return []string{}, []int{}
	}
	defer rows.Close()

	var labels []string
	var data []int
	for rows.Next() {
		var name string
		var count int
		rows.Scan(&name, &count)
		labels = append(labels, name)
		data = append(data, count)
	}

	return labels, data
}

func getUserDistribution(db *sql.DB) ([]string, []int) {
	var activeUsers, vipUsers, newUsers, totalUsers int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE last_login > NOW() - INTERVAL '7 days'`).Scan(&activeUsers)
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_vip = true`).Scan(&vipUsers)
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE DATE(created_at) >= CURRENT_DATE - INTERVAL '7 days'`).Scan(&newUsers)
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)

	return []string{"Aktif", "VIP", "Yeni", "Diğer"},
		[]int{activeUsers, vipUsers, newUsers, totalUsers - activeUsers}
}

func getVIPSalesData(db *sql.DB) ([]string, []int) {
	rows, err := db.Query(`
		SELECT 
			TO_CHAR(DATE_TRUNC('month', purchased_at), 'MM/YY') as month,
			COALESCE(SUM(price), 0) as total
		FROM vip_purchases
		WHERE purchased_at > NOW() - INTERVAL '12 months'
		GROUP BY DATE_TRUNC('month', purchased_at)
		ORDER BY month ASC
		LIMIT 12
	`)

	if err != nil {
		return []string{}, []int{}
	}
	defer rows.Close()

	var labels []string
	var data []int
	for rows.Next() {
		var month string
		var total int
		rows.Scan(&month, &total)
		labels = append(labels, month)
		data = append(data, total)
	}

	return labels, data
}

// AdminLogs API - Log listesi (JSON)
func AdminLogs(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		logType := r.URL.Query().Get("type")

		if page < 1 {
			page = 1
		}
		if limit < 1 {
			limit = 50
		}

		query := `
            SELECT id, action_type, user_id, username, ip_address, details, created_at
            FROM admin_logs
            WHERE 1=1
        `
		var args []interface{}
		argCount := 1

		if logType != "" && logType != "all" {
			query += ` AND action_type = $` + strconv.Itoa(argCount)
			args = append(args, logType)
			argCount++
		}

		// Toplam sayı
		var total int
		countQuery := "SELECT COUNT(*) FROM (" + query + ") AS count"
		db.QueryRow(countQuery, args...).Scan(&total)

		// Sayfalama
		query += ` ORDER BY created_at DESC 
                   LIMIT $` + strconv.Itoa(argCount) + ` 
                   OFFSET $` + strconv.Itoa(argCount+1)
		args = append(args, limit, (page-1)*limit)

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var logs []map[string]interface{}
		for rows.Next() {
			var log struct {
				ID         int
				ActionType string
				UserID     *int
				Username   *string
				IPAddress  string
				Details    string
				CreatedAt  time.Time
			}
			rows.Scan(&log.ID, &log.ActionType, &log.UserID, &log.Username,
				&log.IPAddress, &log.Details, &log.CreatedAt)

			logs = append(logs, map[string]interface{}{
				"id":          log.ID,
				"action_type": log.ActionType,
				"user_id":     log.UserID,
				"username":    log.Username,
				"ip_address":  log.IPAddress,
				"details":     log.Details,
				"created_at":  log.CreatedAt,
			})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": logs,
			"pagination": map[string]interface{}{
				"current_page": page,
				"total_pages":  (total + limit - 1) / limit,
				"total":        total,
				"limit":        limit,
			},
		})
	}
}

// AdminExportLogs API - Logları dışa aktar
func AdminExportLogs(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
            SELECT created_at, action_type, username, ip_address, details
            FROM admin_logs
            ORDER BY created_at DESC
            LIMIT 1000
        `)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment;filename=logs.csv")

		// CSV başlıkları
		w.Write([]byte("Tarih,İşlem Tipi,Kullanıcı,IP Adresi,Detay\n"))

		for rows.Next() {
			var createdAt time.Time
			var actionType, username, ipAddress, details string
			rows.Scan(&createdAt, &actionType, &username, &ipAddress, &details)
			w.Write([]byte(createdAt.Format("2006-01-02 15:04:05") + "," +
				actionType + "," + username + "," + ipAddress + "," + details + "\n"))
		}
	}
}

// AdminSettings API - Ayarlar (JSON)
func AdminSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			// Ayarları getir
			var settings map[string]interface{}
			rows, err := db.Query("SELECT key, value FROM system_settings")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			settings = make(map[string]interface{})
			for rows.Next() {
				var key, value string
				rows.Scan(&key, &value)
				settings[key] = value
			}

			json.NewEncoder(w).Encode(settings)
		} else if r.Method == "POST" {
			// Ayarları güncelle
			var updates map[string]string
			if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
				http.Error(w, "Geçersiz istek", http.StatusBadRequest)
				return
			}

			tx, err := db.Begin()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer tx.Rollback()

			for key, value := range updates {
				_, err = tx.Exec(`
                    INSERT INTO system_settings (key, value, updated_at)
                    VALUES ($1, $2, NOW())
                    ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
                `, key, value)

				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}

			tx.Commit()

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Ayarlar güncellendi",
			})
		}
	}
}

// AdminLogout API - Çıkış (JSON)
func AdminLogout(store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		session.Values["authenticated"] = false
		session.Options.MaxAge = -1
		session.Save(r, w)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Çıkış yapıldı",
		})
	}
}

// Yardımcı fonksiyonlar
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	password := make([]byte, length)
	for i := range password {
		password[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(1)
	}
	return string(password)
}

// AdminCreateVIPPlan API - Yeni VIP planı oluştur

// /----------------------------------------------------------------------------------------
// DENEYSEL
// Template fonksiyonlarını tanımla
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"toLower":   strings.ToLower,
		"toUpper":   strings.ToUpper,
		"replace":   strings.ReplaceAll,
		"split":     strings.Split,
		"join":      strings.Join,
		"trim":      strings.TrimSpace,
		"now":       time.Now,
		"formatDate": func(t time.Time) string {
			return t.Format("02.01.2006 15:04")
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int { return a / b },
		"mod": func(a, b int) int { return a % b },
	}
}

// Güncellenmiş AdminSettingsPage fonksiyonu
func AdminSettingsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// Ayarları getir - struct olarak tanımla
		type Settings struct {
			SiteName             string
			SiteURL              string
			SiteDescription      string
			SiteKeywords         string
			LogoURL              string
			MaintenanceMode      bool
			RegistrationOpen     bool
			DebugMode            bool
			ThemeColor           string
			BackgroundPattern    string
			DefaultLanguage      string
			Timezone             string
			DateFormat           string
			SessionTimeout       int
			MaxSessions          int
			MaxLoginAttempts     int
			LockoutTime          int
			TwoFactorAuth        bool
			RecaptchaEnabled     bool
			LogIPAddress         bool
			ForceHTTPS           bool
			JWTSecret            string
			JWTExpiry            int
			RefreshTokenExpiry   int
			RateLimit            int
			BlockedIPs           string
			BlockedCountries     string
			SMTPHost             string
			SMTPPort             int
			SMTPUsername         string
			SMTPPassword         string
			SMTPFromEmail        string
			SMTPFromName         string
			SMTPSSL              bool
			SMTPEnabled          bool
			DockerHost           string
			DockerAPIVersion     string
			DockerNetwork        string
			MaxContainers        int
			ContainerTimeout     int
			ContainerCPU         float64
			ContainerMemory      int
			ContainerDisk        int
			AutoStartContainers  bool
			ContainerLogging     bool
			MaxUsers             int
			MaxMachines          int
			DailySubmissionLimit int
			MaxFlagAttempts      int
			MaxUploadSize        int
			AllowedFileTypes     string
			MinPoints            int
			MaxPoints            int
			VIPPrice             float64
			VIPDuration          int
			MailSubject          string
			MailBody             string
			BackupCron           string
			MaxBackups           int
			BackupPath           string
			AutoBackup           bool
			BackupDatabase       bool
			BackupFiles          bool
		}

		settings := Settings{
			// Varsayılan değerler
			SiteName:             "CTF Platform",
			SiteURL:              "http://localhost:8181",
			LogoURL:              "/static/images/logo.png",
			ThemeColor:           "#00ff9d",
			DefaultLanguage:      "tr",
			Timezone:             "Europe/Istanbul",
			DateFormat:           "DD.MM.YYYY",
			SessionTimeout:       60,
			MaxSessions:          5,
			MaxLoginAttempts:     5,
			LockoutTime:          15,
			JWTExpiry:            24,
			RefreshTokenExpiry:   7,
			RateLimit:            100,
			SMTPPort:             587,
			MaxContainers:        10,
			ContainerTimeout:     60,
			ContainerCPU:         1.0,
			ContainerMemory:      512,
			ContainerDisk:        10,
			MaxUsers:             10000,
			MaxMachines:          100,
			DailySubmissionLimit: 100,
			MaxFlagAttempts:      5,
			MaxUploadSize:        10,
			AllowedFileTypes:     ".jpg,.png,.pdf,.txt",
			MinPoints:            0,
			MaxPoints:            10000,
			VIPPrice:             99.90,
			VIPDuration:          30,
			MaxBackups:           10,
			BackupPath:           "/backups",
		}

		// Veritabanından ayarları çek
		rows, err := db.Query("SELECT key, value FROM system_settings")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var key, value string
				rows.Scan(&key, &value)

				// String değerler
				switch key {
				case "site_name":
					settings.SiteName = value
				case "site_url":
					settings.SiteURL = value
				case "site_description":
					settings.SiteDescription = value
				case "site_keywords":
					settings.SiteKeywords = value
				case "logo_url":
					settings.LogoURL = value
				case "theme_color":
					settings.ThemeColor = value
				case "background_pattern":
					settings.BackgroundPattern = value
				case "default_language":
					settings.DefaultLanguage = value
				case "timezone":
					settings.Timezone = value
				case "date_format":
					settings.DateFormat = value
				case "jwt_secret":
					settings.JWTSecret = value
				case "blocked_ips":
					settings.BlockedIPs = value
				case "blocked_countries":
					settings.BlockedCountries = value
				case "smtp_host":
					settings.SMTPHost = value
				case "smtp_username":
					settings.SMTPUsername = value
				case "smtp_password":
					settings.SMTPPassword = value
				case "smtp_from_email":
					settings.SMTPFromEmail = value
				case "smtp_from_name":
					settings.SMTPFromName = value
				case "docker_host":
					settings.DockerHost = value
				case "docker_api_version":
					settings.DockerAPIVersion = value
				case "docker_network":
					settings.DockerNetwork = value
				case "allowed_file_types":
					settings.AllowedFileTypes = value
				case "mail_subject":
					settings.MailSubject = value
				case "mail_body":
					settings.MailBody = value
				case "backup_cron":
					settings.BackupCron = value
				case "backup_path":
					settings.BackupPath = value
				}

				// Boolean değerler
				switch key {
				case "maintenance_mode":
					settings.MaintenanceMode = value == "true"
				case "registration_open":
					settings.RegistrationOpen = value == "true"
				case "debug_mode":
					settings.DebugMode = value == "true"
				case "two_factor_auth":
					settings.TwoFactorAuth = value == "true"
				case "recaptcha_enabled":
					settings.RecaptchaEnabled = value == "true"
				case "log_ip_address":
					settings.LogIPAddress = value == "true"
				case "force_https":
					settings.ForceHTTPS = value == "true"
				case "smtp_ssl":
					settings.SMTPSSL = value == "true"
				case "smtp_enabled":
					settings.SMTPEnabled = value == "true"
				case "auto_start_containers":
					settings.AutoStartContainers = value == "true"
				case "container_logging":
					settings.ContainerLogging = value == "true"
				case "auto_backup":
					settings.AutoBackup = value == "true"
				case "backup_database":
					settings.BackupDatabase = value == "true"
				case "backup_files":
					settings.BackupFiles = value == "true"
				}

				// Integer değerler
				intFields := map[string]*int{
					"session_timeout":        &settings.SessionTimeout,
					"max_sessions":           &settings.MaxSessions,
					"max_login_attempts":     &settings.MaxLoginAttempts,
					"lockout_time":           &settings.LockoutTime,
					"jwt_expiry":             &settings.JWTExpiry,
					"refresh_token_expiry":   &settings.RefreshTokenExpiry,
					"rate_limit":             &settings.RateLimit,
					"smtp_port":              &settings.SMTPPort,
					"max_containers":         &settings.MaxContainers,
					"container_timeout":      &settings.ContainerTimeout,
					"container_memory":       &settings.ContainerMemory,
					"container_disk":         &settings.ContainerDisk,
					"max_users":              &settings.MaxUsers,
					"max_machines":           &settings.MaxMachines,
					"daily_submission_limit": &settings.DailySubmissionLimit,
					"max_flag_attempts":      &settings.MaxFlagAttempts,
					"max_upload_size":        &settings.MaxUploadSize,
					"min_points":             &settings.MinPoints,
					"max_points":             &settings.MaxPoints,
					"vip_duration":           &settings.VIPDuration,
					"max_backups":            &settings.MaxBackups,
				}

				for field, ptr := range intFields {
					if key == field {
						if intVal, err := strconv.Atoi(value); err == nil {
							*ptr = intVal
						}
					}
				}

				// Float değerler
				if key == "container_cpu" {
					if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
						settings.ContainerCPU = floatVal
					}
				}
				if key == "vip_price" {
					if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
						settings.VIPPrice = floatVal
					}
				}
			}
		}

		admin := models.Admin{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		data := struct {
			Title    string
			Active   string
			Admin    models.Admin
			Settings Settings // interface{} yerine Settings tipi
		}{
			Title:    "Sistem Ayarları - Admin Panel",
			Active:   "settings",
			Admin:    admin,
			Settings: settings,
		}

		// Template fonksiyonlarını tanımla
		funcMap := template.FuncMap{
			"contains": func(s, substr string) bool {
				return strings.Contains(s, substr)
			},
			"eq": func(a, b interface{}) bool {
				return a == b
			},
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/settings.html",
		)
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

// Sistem ayarları struct'ı

// Helper fonksiyonlar
func getSystemSettings(db *sql.DB) (models.SystemSettings, error) {
	var settings models.SystemSettings

	// Veritabanından ayarları çek (örnek)
	rows, err := db.Query("SELECT key, value FROM system_settings")
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	settingsMap := make(map[string]string)
	for rows.Next() {
		var key, value string
		rows.Scan(&key, &value)
		settingsMap[key] = value
	}

	// Map'ten struct'a dönüştür
	settings.SiteName = settingsMap["site_name"]
	settings.SiteDescription = settingsMap["site_description"]
	settings.MaintenanceMode = settingsMap["maintenance_mode"] == "true"
	// ... diğer alanlar

	return settings, nil
}

func getDefaultSettings() models.SystemSettings {
	return models.SystemSettings{
		SiteName:          "HACKLAB CTF Platform",
		SiteDescription:   "Güvenlik uzmanları yetiştiren CTF platformu",
		SiteKeywords:      "ctf, cybersecurity, hacking, pentest",
		MaintenanceMode:   false,
		RegistrationOpen:  true,
		DefaultUserPoints: 100,
		SessionTimeout:    120,
		MaxUploadSize:     10,
		AllowedFileTypes:  []string{".jpg", ".png", ".txt", ".pdf"},
		SecuritySettings: models.SecuritySettings{
			TwoFactorAuth:   false,
			PasswordMinLen:  8,
			PasswordComplex: true,
			LoginAttempts:   5,
			BlockDuration:   30,
		},
		CTFSettings: models.CTFSettings{
			EnableCTF:        true,
			MaxTeamSize:      4,
			EnableScoreboard: true,
			FlagFormat:       "flag{...}",
		},
	}
}

// AdminBackupPage - Yedekleme sayfası (HTML)
func AdminBackupPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		var admin models.Admin
		admin.Username = username
		admin.Role = role
		admin.Avatar = "/static/images/avatar.png"

		var avatar sql.NullString
		err := db.QueryRow(`SELECT avatar FROM admins WHERE username = $1`, username).Scan(&avatar)
		if err == nil && avatar.Valid && avatar.String != "" {
			admin.Avatar = avatar.String
		}

		// Son yedekleme bilgilerini al
		var lastBackup struct {
			ID        int
			Filename  string
			Size      int64
			CreatedAt time.Time
		}
		err = db.QueryRow(`
			SELECT id, filename, size, created_at 
			FROM backups 
			ORDER BY created_at DESC 
			LIMIT 1
		`).Scan(&lastBackup.ID, &lastBackup.Filename, &lastBackup.Size, &lastBackup.CreatedAt)

		hasLastBackup := err == nil

		// Yedekleme istatistikleri
		var backupCount int
		db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&backupCount)

		var totalSize int64
		db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM backups`).Scan(&totalSize)

		data := struct {
			Title         string
			Active        string
			Admin         interface{}
			LastBackup    interface{}
			HasLastBackup bool
			BackupCount   int
			TotalSize     int64
		}{
			Title:         "Yedekleme - Admin Panel",
			Active:        "backup",
			Admin:         admin,
			LastBackup:    lastBackup,
			HasLastBackup: hasLastBackup,
			BackupCount:   backupCount,
			TotalSize:     totalSize,
		}

		tmpl, err := template.New("layout.html").Funcs(template.FuncMap{
			"formatSize": func(size int64) string {
				const unit = 1024
				if size < unit {
					return fmt.Sprintf("%d B", size)
				}
				div, exp := int64(unit), 0
				for n := size / unit; n >= unit; n /= unit {
					div *= unit
					exp++
				}
				return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
			},
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006 15:04:05")
			},
		}).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/backup.html",
		)
		if err != nil {
			http.Error(w, "Template yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl.Execute(w, data)
	}
}

// AdminBackupList - Yedekleme listesi API
func AdminBackupList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query(`
			SELECT id, filename, size, created_at 
			FROM backups 
			ORDER BY created_at DESC
			LIMIT 50
		`)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var backups []map[string]interface{}
		for rows.Next() {
			var id int
			var filename string
			var size int64
			var createdAt time.Time
			rows.Scan(&id, &filename, &size, &createdAt)

			backups = append(backups, map[string]interface{}{
				"id":         id,
				"filename":   filename,
				"size":       size,
				"created_at": createdAt,
			})
		}

		json.NewEncoder(w).Encode(backups)
	}
}

// AdminCreateBackup - Yeni yedekleme oluştur
func AdminCreateBackup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Backup dizini oluştur
		backupDir := "./backups"
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Backup dizini oluşturulamadı: " + err.Error(),
			})
			return
		}

		// Backup dosya adı
		timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("backup_%s.sql", timestamp)
		filepath := fmt.Sprintf("%s/%s", backupDir, filename)

		// PostgreSQL dump komutu
		// NOT: Burada veritabanı bağlantı bilgilerinizi kullanın
		psqlPath := "C:\\Program Files\\PostgreSQL\\18\\bin\\pg_dump.exe"

		cmd := exec.Command(psqlPath,
			"-h", "localhost",
			"-U", "postgres",
			"-d", "ctf_platform",
			"-f", filepath,
		)

		// Şifre için environment variable kullanın
		cmd.Env = append(os.Environ(), "PGPASSWORD=muhammed")
		fmt.Println("Yedekleme komutu çalıştırılıyor:", cmd.String())
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println("Yedekleme komutu başarısız:", err)
			fmt.Println("Komut çıktısı:", string(output))
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Yedekleme başarısız: " + err.Error(),
				"output":  string(output),
			})
			return
		}
		fmt.Println("Yedekleme komutu başarıyla çalıştı:", string(output))
		// Dosya boyutunu al
		fileInfo, err := os.Stat(filepath)
		size := int64(0)
		if err == nil {
			fmt.Println(err)
			size = fileInfo.Size()
		}

		// Veritabanına kaydet
		var backupID int
		err = db.QueryRow(`
			INSERT INTO backups (filename, size, created_at)
			VALUES ($1, $2, NOW())
			RETURNING id
		`, filename, size).Scan(&backupID)

		if err != nil {
			fmt.Println(err)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Yedekleme kaydedilemedi: " + err.Error(),
			})
			return
		}

		// Log ekle
		db.Exec(`
			INSERT INTO admin_logs (action_type, username, ip_address, details, created_at)
			VALUES ('backup_create', $1, $2, $3, NOW())
		`, r.Header.Get("X-Admin-Username"), r.RemoteAddr, filename)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"message":   "Yedekleme başarıyla oluşturuldu",
			"backup_id": backupID,
			"filename":  filename,
			"size":      size,
		})
	}
}

// AdminDownloadBackup - Yedekleme dosyasını indir
func AdminDownloadBackup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		backupID := vars["id"]

		var filename string
		err := db.QueryRow(`SELECT filename FROM backups WHERE id = $1`, backupID).Scan(&filename)
		if err != nil {
			http.Error(w, "Yedek bulunamadı", http.StatusNotFound)
			return
		}

		filepath := fmt.Sprintf("./backups/%s", filename)

		// Dosya var mı kontrol et
		if _, err := os.Stat(filepath); os.IsNotExist(err) {
			http.Error(w, "Dosya bulunamadı", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		w.Header().Set("Content-Type", "application/sql")
		http.ServeFile(w, r, filepath)
	}
}

// AdminDeleteBackup - Yedekleme sil
func AdminDeleteBackup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		backupID := vars["id"]

		var filename string
		err := db.QueryRow(`SELECT filename FROM backups WHERE id = $1`, backupID).Scan(&filename)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Yedek bulunamadı",
			})
			return
		}

		// Dosyayı sil
		filepath := fmt.Sprintf("./backups/%s", filename)
		os.Remove(filepath)

		// Veritabanından sil
		_, err = db.Exec(`DELETE FROM backups WHERE id = $1`, backupID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Silme başarısız: " + err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Yedek silindi",
		})
	}
}

// AdminRestoreBackup - Yedekten geri yükleme
func AdminRestoreBackup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		backupID := vars["id"]

		var filename string
		err := db.QueryRow(`SELECT filename FROM backups WHERE id = $1`, backupID).Scan(&filename)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Yedek bulunamadı",
			})
			return
		}

		filepath := fmt.Sprintf("./backups/%s", filename)

		// PostgreSQL restore komutu
		psqlPath := "C:\\Program Files\\PostgreSQL\\18\\bin\\psql.exe"

		cmd := exec.Command(psqlPath,
			"-h", "localhost",
			"-U", "postgres",
			"-d", "ctf_platform",
			"-f", filepath,
		)

		cmd.Env = append(os.Environ(), "PGPASSWORD=muhammed")

		output, err := cmd.CombinedOutput()
		if err != nil {

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Geri yükleme başarısız: " + err.Error(),
				"output":  string(output),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Veritabanı başarıyla geri yüklendi",
		})
	}
}

// AdminUploadBackup - Dosya yükleme ile yedekleme
func AdminUploadBackup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 10 MB limit
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Dosya çok büyük",
			})
			return
		}

		file, header, err := r.FormFile("backup_file")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Dosya alınamadı",
			})
			return
		}
		defer file.Close()

		// Dosya tipini kontrol et
		if !strings.HasSuffix(header.Filename, ".sql") && !strings.HasSuffix(header.Filename, ".sql.gz") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Sadece .sql veya .sql.gz dosyaları yüklenebilir",
			})
			return
		}

		// Backup dizini oluştur
		backupDir := "./backups"
		os.MkdirAll(backupDir, 0755)

		// Dosyayı kaydet
		timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("uploaded_%s_%s", timestamp, header.Filename)
		filepath := fmt.Sprintf("%s/%s", backupDir, filename)

		dst, err := os.Create(filepath)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Dosya kaydedilemedi",
			})
			return
		}
		defer dst.Close()

		_, err = io.Copy(dst, file)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Dosya kopyalanamadı",
			})
			return
		}

		// Dosya boyutunu al
		fileInfo, _ := dst.Stat()
		size := fileInfo.Size()

		// Veritabanına kaydet
		_, err = db.Exec(`INSERT INTO backups (filename, size, created_at) VALUES ($1, $2, NOW())`, filename, size)

		if err != nil {
			fmt.Println("Hata", err)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Yedek kaydedilemedi",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"message":  "Yedek başarıyla yüklendi",
			"filename": filename,
			"size":     size,
		})
	}
}
