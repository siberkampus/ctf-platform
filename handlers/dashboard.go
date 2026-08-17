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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ctf-platform/models"

	"github.com/gorilla/sessions"
)

// GetMyProfile - Kullanıcının kendi profilini getir
func GetMyProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Geçersiz kullanıcı ID", http.StatusBadRequest)
			return
		}

		var user models.User
		var avatar, bio, country, website sql.NullString
		var vipExpiryDate sql.NullTime
		var lastLogin sql.NullTime
		var fullName, referralCode sql.NullString

		err = db.QueryRow(`
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
            WHERE id = $1 AND is_active = true
        `, userID).Scan(
			&user.ID, &user.Username, &user.Email,
			&avatar, &bio, &country, &website,
			&user.IsVIP, &vipExpiryDate, &user.Points, &user.Rank,
			&user.TwoFactorEnabled, &user.CreatedAt, &lastLogin, &user.IsActive,
			&fullName, &referralCode, &user.Newsletter, &user.EmailVerified,
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

		// Null değerleri ata
		user.Avatar = avatar.String
		user.Bio = bio.String
		user.Country = country.String
		user.Website = website.String
		user.FullName = fullName.String
		user.ReferralCode = referralCode.String

		if vipExpiryDate.Valid {
			user.VIPExpiryDate = vipExpiryDate
		}
		if lastLogin.Valid {
			user.LastLogin = lastLogin
		}

		// İstatistikleri getir
		var solvedCount, hintUsedCount, submissionCount int

		db.QueryRow(`
            SELECT COUNT(DISTINCT question_id) 
            FROM user_solutions 
            WHERE user_id = $1
        `, userID).Scan(&solvedCount)

		db.QueryRow(`
            SELECT COUNT(DISTINCT question_id) 
            FROM hint_usage 
            WHERE user_id = $1
        `, userID).Scan(&hintUsedCount)

		db.QueryRow(`
            SELECT COUNT(*) 
            FROM submissions 
            WHERE user_id = $1
        `, userID).Scan(&submissionCount)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user": user,
			"stats": map[string]interface{}{
				"solved_count":     solvedCount,
				"hint_used_count":  hintUsedCount,
				"submission_count": submissionCount,
			},
		})
	}
}

// UpdateProfile - Kullanıcı profilini güncelle
func UpdateProfile(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Geçersiz kullanıcı ID", http.StatusBadRequest)
			return
		}

		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Geçersiz istek formatı", http.StatusBadRequest)
			return
		}

		// Güncellenebilir alanlar (location kaldırıldı, country eklendi)
		allowedFields := []string{"bio", "country", "website", "avatar", "fullname", "newsletter"}

		query := "UPDATE users SET "
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

		// Güncelleme yapılacak alan yoksa
		if argCount == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Güncellenecek alan bulunamadı",
			})
			return
		}

		query += " WHERE id = $" + strconv.Itoa(argCount)
		args = append(args, userID)

		_, err = db.Exec(query, args...)
		if err != nil {
			log.Printf("Profil güncelleme hatası: %v", err)
			http.Error(w, "Profil güncellenemedi", http.StatusInternalServerError)
			return
		}

		// Activity log'a ekle
		_, err = db.Exec(`
            INSERT INTO activity_logs (user_id, action_type, ip_address, created_at)
            VALUES ($1, 'profile_update', $2, NOW())
        `, userID, r.RemoteAddr)
		if err != nil {
			log.Printf("Activity log hatası: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Profil başarıyla güncellendi",
		})
	}
}

// GetSettings - Kullanıcı ayarlarını getir
func GetSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Geçersiz kullanıcı ID", http.StatusBadRequest)
			return
		}

		var settings models.UserSettings

		err = db.QueryRow(`
            SELECT 
                user_id, email_notifications, browser_notifications, sound_enabled,
                profile_public, show_activity, show_online_status,
                theme, font_size, language, updated_at
            FROM user_settings
            WHERE user_id = $1
        `, userID).Scan(
			&settings.UserID, &settings.EmailNotifications, &settings.BrowserNotifications,
			&settings.SoundEnabled, &settings.ProfilePublic, &settings.ShowActivity,
			&settings.ShowOnlineStatus, &settings.Theme, &settings.FontSize,
			&settings.Language, &settings.UpdatedAt,
		)

		if err == sql.ErrNoRows {
			// Varsayılan ayarları oluştur
			settings = models.UserSettings{
				UserID:               userID,
				EmailNotifications:   true,
				BrowserNotifications: true,
				SoundEnabled:         false,
				ProfilePublic:        true,
				ShowActivity:         true,
				ShowOnlineStatus:     true,
				Theme:                "dark",
				FontSize:             "medium",
				Language:             "tr",
				UpdatedAt:            time.Now(),
			}

			// Varsayılan ayarları veritabanına ekle
			_, err = db.Exec(`
                INSERT INTO user_settings 
                    (user_id, email_notifications, browser_notifications, sound_enabled,
                     profile_public, show_activity, show_online_status,
                     theme, font_size, language, updated_at)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
            `, settings.UserID, settings.EmailNotifications, settings.BrowserNotifications,
				settings.SoundEnabled, settings.ProfilePublic, settings.ShowActivity,
				settings.ShowOnlineStatus, settings.Theme, settings.FontSize,
				settings.Language, settings.UpdatedAt)

			if err != nil {
				log.Printf("Varsayılan ayar ekleme hatası: %v", err)
			}
		} else if err != nil {
			log.Printf("Ayar sorgu hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	}
}

// UpdateSettings - Kullanıcı ayarlarını güncelle
func UpdateSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Geçersiz kullanıcı ID", http.StatusBadRequest)
			return
		}

		var settings models.UserSettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "Geçersiz istek formatı", http.StatusBadRequest)
			return
		}

		settings.UserID = userID
		settings.UpdatedAt = time.Now()

		// Ayarları güncelle
		_, err = db.Exec(`
            UPDATE user_settings 
            SET email_notifications = $1, browser_notifications = $2,
                sound_enabled = $3, profile_public = $4,
                show_activity = $5, show_online_status = $6,
                theme = $7, font_size = $8, language = $9,
                updated_at = $10
            WHERE user_id = $11
        `,
			settings.EmailNotifications, settings.BrowserNotifications,
			settings.SoundEnabled, settings.ProfilePublic,
			settings.ShowActivity, settings.ShowOnlineStatus,
			settings.Theme, settings.FontSize, settings.Language,
			settings.UpdatedAt, userID,
		)

		if err != nil {
			log.Printf("Ayar güncelleme hatası: %v", err)
			http.Error(w, "Ayarlar güncellenemedi", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Ayarlar başarıyla güncellendi",
		})
	}
}

// UpdateSecurity - Güvenlik ayarlarını güncelle
func UpdateSecurity(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			http.Error(w, "Geçersiz kullanıcı ID", http.StatusBadRequest)
			return
		}

		var security struct {
			TwoFactorEnabled bool   `json:"two_factor_enabled"`
			CurrentPassword  string `json:"current_password"`
			NewPassword      string `json:"new_password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&security); err != nil {
			http.Error(w, "Geçersiz istek formatı", http.StatusBadRequest)
			return
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Şifre değişikliği varsa
		if security.NewPassword != "" {
			// Mevcut şifreyi kontrol et (gerçek uygulamada hash karşılaştırması yapılır)
			var currentHash string
			err = tx.QueryRow(`
                SELECT password_hash FROM users WHERE id = $1
            `, userID).Scan(&currentHash)

			if err != nil {
				log.Printf("Şifre sorgu hatası: %v", err)
				http.Error(w, "Kullanıcı bulunamadı", http.StatusNotFound)
				return
			}

			// TODO: Şifre hash'ini kontrol et
			// if !checkPasswordHash(security.CurrentPassword, currentHash) {
			//     http.Error(w, "Mevcut şifre yanlış", http.StatusBadRequest)
			//     return
			// }

			// Yeni şifreyi hash'le ve güncelle
			// newHash := hashPassword(security.NewPassword)
			_, err = tx.Exec(`
                UPDATE users SET password_hash = $1 WHERE id = $2
            `, security.NewPassword, userID) // TODO: Hash'lenmiş şifre kullan

			if err != nil {
				log.Printf("Şifre güncelleme hatası: %v", err)
				http.Error(w, "Şifre güncellenemedi", http.StatusInternalServerError)
				return
			}
		}

		// 2FA durumunu güncelle
		_, err = tx.Exec(`
            UPDATE users SET two_factor_enabled = $1 WHERE id = $2
        `, security.TwoFactorEnabled, userID)

		if err != nil {
			log.Printf("2FA güncelleme hatası: %v", err)
			http.Error(w, "Güvenlik ayarları güncellenemedi", http.StatusInternalServerError)
			return
		}

		// Activity log'a ekle
		_, err = tx.Exec(`
            INSERT INTO activity_logs (user_id, action_type, ip_address, created_at)
            VALUES ($1, 'security_update', $2, NOW())
        `, userID, r.RemoteAddr)

		if err != nil {
			log.Printf("Activity log hatası: %v", err)
		}

		err = tx.Commit()
		if err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			http.Error(w, "İşlem tamamlanamadı", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Güvenlik ayarları güncellendi",
		})
	}
}

// UploadAvatar - Profil fotoğrafı yükle
func UploadAvatar(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Session'dan user_id al (güvenli!)
		session, err := store.Get(r, "session")
		if err != nil {
			writeErrorResponse(w, http.StatusUnauthorized, "Oturum bulunamadı")
			return
		}

		userIDValue, ok := session.Values["user_id"]
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		userID, ok := userIDValue.(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Geçersiz kullanıcı bilgisi")
			return
		}

		// Kullanıcı adını al (dosya adı için)
		var username string
		err = db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
		if err != nil {
			writeErrorResponse(w, http.StatusNotFound, "Kullanıcı bulunamadı")
			return
		}

		// Multipart form parse et (10 MB max)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Dosya çok büyük (max 10MB)")
			return
		}

		file, header, err := r.FormFile("avatar")
		if err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Dosya yüklenemedi")
			return
		}
		defer file.Close()

		// Dosya tipini kontrol et
		contentType := header.Header.Get("Content-Type")
		allowedTypes := map[string]bool{
			"image/jpeg": true,
			"image/png":  true,
			"image/gif":  true,
			"image/webp": true,
		}

		if !allowedTypes[contentType] {
			writeErrorResponse(w, http.StatusBadRequest, "Sadece JPEG, PNG, GIF ve WEBP dosyaları yüklenebilir")
			return
		}

		// Uploads dizininin var olduğundan emin ol
		uploadDir := "uploads/avatars"
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			log.Printf("Uploads dizini oluşturma hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "Dosya yükleme dizini oluşturulamadı")
			return
		}

		// Benzersiz dosya adı oluştur (timestamp + username)
		ext := filepath.Ext(header.Filename)
		if ext == "" {
			// Content-Type'a göre uzantı belirle
			switch contentType {
			case "image/jpeg":
				ext = ".jpg"
			case "image/png":
				ext = ".png"
			case "image/gif":
				ext = ".gif"
			case "image/webp":
				ext = ".webp"
			default:
				ext = ".jpg"
			}
		}

		// Benzersiz dosya adı: timestamp_username.extension
		fileName := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), username, ext)
		filePath := filepath.Join(uploadDir, fileName)

		// Dosya boyutunu kontrol et (5 MB)
		if header.Size > 5*1024*1024 {
			writeErrorResponse(w, http.StatusBadRequest, "Dosya boyutu 5 MB'dan büyük olamaz")
			return
		}

		// Dosyayı kaydet
		dst, err := os.Create(filePath)
		if err != nil {
			log.Printf("Dosya oluşturma hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "Dosya kaydedilemedi")
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			log.Printf("Dosya kopyalama hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "Dosya kopyalanamadı")
			return
		}

		// Eski avatarı sil (varsayılan değilse)
		var oldAvatar string
		err = db.QueryRow(`SELECT avatar FROM users WHERE id = $1`, userID).Scan(&oldAvatar)
		if err == nil && oldAvatar != "" && oldAvatar != "/static/images/default-avatar.png" {
			// Eski dosya yolunu bul ve sil
			oldPath := strings.TrimPrefix(oldAvatar, "/")
			if err := os.Remove(oldPath); err != nil {
				log.Printf("Eski avatar silinemedi: %v", err)
			}
		}

		// Yeni avatar URL'i
		avatarURL := "/uploads/avatars/" + fileName

		// Veritabanında avatar yolunu güncelle
		_, err = db.Exec(`
			UPDATE users SET avatar = $1 WHERE id = $2
		`, avatarURL, userID)

		if err != nil {
			log.Printf("Avatar güncelleme hatası: %v", err)
			// Dosyayı sil (hata durumunda)
			os.Remove(filePath)
			writeErrorResponse(w, http.StatusInternalServerError, "Avatar güncellenemedi")
			return
		}

		writeSuccessResponse(w, map[string]interface{}{
			"avatar_url": avatarURL,
			"message":    "Profil fotoğrafı başarıyla güncellendi",
		})
	}
}

// DashboardPage - Kullanıcı dashboard sayfası
func DashboardPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		isAuth := false
		var user *models.User
		var stats models.DashboardStats
		var recentActivity []models.Activity
		var inProgress []models.InProgressMachine
		var achievements []models.UserAchievement

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true

			userID, ok := session.Values["user_id"].(int)
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user = &models.User{}

			// Sadece ihtiyacımız olan alanları tanımla
			var avatar, bio, country, website sql.NullString

			err := db.QueryRow(`
                SELECT 
                    id, username, email, 
                    COALESCE(avatar, '/static/images/avatar.png') as avatar,
                    COALESCE(bio, '') as bio,
                    COALESCE(country, '') as country,
                    COALESCE(website, '') as website,
                    is_vip, points, rank, created_at
                FROM users 
                WHERE id = $1
            `, userID).Scan(
				&user.ID, &user.Username, &user.Email,
				&avatar, &bio, &country, &website,
				&user.IsVIP, &user.Points, &user.Rank,
				&user.CreatedAt,
			)

			if err != nil {
				log.Printf("Kullanıcı sorgu hatası: %v", err)
				// Kullanıcı bulunamazsa dashboard yerine login'e yönlendir
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Null değerleri ata
			user.Avatar = avatar.String
			user.Bio = bio.String
			user.Country = country.String
			user.Website = website.String

			// İstatistikler - Tek sorguda birleştir
			var solvedQuestions, solvedMachines, totalPoints int

			// Kullanıcının toplam puanını users tablosundan al (en doğrusu)
			err = db.QueryRow(`
    SELECT COALESCE(points, 0) FROM users WHERE id = $1
`, userID).Scan(&totalPoints)

			// Çözülen benzersiz soru sayısı
			err = db.QueryRow(`
    SELECT COUNT(DISTINCT question_id) 
    FROM user_solutions 
    WHERE user_id = $1
`, userID).Scan(&solvedQuestions)

			// Çözülen benzersiz makine sayısı
			err = db.QueryRow(`
    SELECT COUNT(DISTINCT machine_id) 
    FROM user_solutions 
    WHERE user_id = $1
`, userID).Scan(&solvedMachines)

			// Toplam kazanılan puan (eğer users tablosuna güvenmiyorsanız)
			var earnedPoints int
			err = db.QueryRow(`
    SELECT COALESCE(SUM(mq.points_reward), 0)
    FROM user_solutions us
    JOIN machine_questions mq ON us.question_id = mq.id
    WHERE us.user_id = $1
`, userID).Scan(&earnedPoints)

			stats.TotalSolved = solvedQuestions
			stats.TotalMachines = solvedMachines
			stats.TotalPoints = totalPoints

			// Toplam makine sayısı
			err = db.QueryRow(`
                SELECT COUNT(*) FROM machines WHERE is_active = true
            `).Scan(&stats.TotalMachinesCount)
			if err != nil {
				log.Printf("Toplam makine sayısı hatası: %v", err)
			}

			// Sıralama
			err = db.QueryRow(`
                SELECT COUNT(*) + 1 FROM users 
                WHERE points > (SELECT points FROM users WHERE id = $1)
            `, userID).Scan(&stats.Rank)
			if err != nil {
				log.Printf("Sıralama sorgu hatası: %v", err)
				stats.Rank = 999 // Varsayılan değer
			}

			// Günlük ilerleme
			err = db.QueryRow(`
                SELECT COUNT(*) FROM user_solutions
                WHERE user_id = $1 AND DATE(solved_at) = CURRENT_DATE
            `, userID).Scan(&stats.DailyProgress)
			if err != nil {
				log.Printf("Günlük ilerleme hatası: %v", err)
			}
			stats.DailyGoal = 5 // Sabit hedef

			// Seri (streak) hesapla - Basitleştirilmiş versiyon
			var streak int
			err = db.QueryRow(`
                WITH daily_solves AS (
                    SELECT DISTINCT DATE(solved_at) as solve_day
                    FROM user_solutions
                    WHERE user_id = $1
                    ORDER BY solve_day DESC
                )
                SELECT COUNT(*)
                FROM daily_solves
                WHERE solve_day > CURRENT_DATE - INTERVAL '7 days'
            `, userID).Scan(&streak)
			if err != nil {
				log.Printf("Streak sorgu hatası: %v", err)
			}
			stats.Streak = streak

			// VIP makine sayısı
			err = db.QueryRow(`
                SELECT COUNT(DISTINCT m.id)
                FROM user_solutions us
                JOIN machines m ON us.machine_id = m.id
                WHERE us.user_id = $1 AND m.is_vip_only = true
            `, userID).Scan(&stats.VIPCount)
			if err != nil {
				log.Printf("VIP makine sayısı hatası: %v", err)
			}

			// Son aktiviteler
			rows, err := db.Query(`
                SELECT 
                    al.id, al.user_id, al.action_type, 
                    al.machine_id, al.question_id, al.created_at,
                    COALESCE(m.name, '') as machine_name,
                    COALESCE(mq.title, '') as question_title
                FROM activity_logs al
                LEFT JOIN machines m ON al.machine_id = m.id
                LEFT JOIN machine_questions mq ON al.question_id = mq.id
                WHERE al.user_id = $1
                ORDER BY al.created_at DESC
                LIMIT 10
            `, userID)

			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var a models.Activity
					var machineName, questionTitle string

					err := rows.Scan(
						&a.ID, &a.UserID, &a.ActionType,
						&a.MachineID, &a.QuestionID, &a.CreatedAt,
						&machineName, &questionTitle,
					)
					if err == nil {
						a.MachineName = machineName
						a.QuestionTitle = questionTitle
						recentActivity = append(recentActivity, a)
					}
				}
			} else {
				log.Printf("Aktivite sorgu hatası: %v", err)
			}

			// Devam eden makineler
			rows, err = db.Query(`
                SELECT m.id, m.name, m.difficulty,
                       COUNT(DISTINCT us.question_id) as solved,
                       COUNT(DISTINCT mq.id) as total
                FROM user_solutions us
                JOIN machines m ON us.machine_id = m.id
                JOIN machine_questions mq ON m.id = mq.machine_id AND mq.is_active = true
                WHERE us.user_id = $1
                GROUP BY m.id, m.name, m.difficulty
                HAVING COUNT(DISTINCT us.question_id) < COUNT(DISTINCT mq.id)
                ORDER BY MAX(us.solved_at) DESC
                LIMIT 5
            `, userID)

			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var ip models.InProgressMachine
					err := rows.Scan(&ip.ID, &ip.Name, &ip.Difficulty, &ip.Solved, &ip.Total)
					if err == nil {
						inProgress = append(inProgress, ip)
					}
				}
			} else {
				log.Printf("Devam eden makineler sorgu hatası: %v", err)
			}

			// Son kazanılan başarımlar
			rows, err = db.Query(`
                SELECT 
                    a.id, a.name, a.description, a.icon, a.points_reward,
                    ua.earned_at
                FROM user_achievements ua
                JOIN achievements a ON ua.achievement_id = a.id
                WHERE ua.user_id = $1
                ORDER BY ua.earned_at DESC
                LIMIT 5
            `, userID)

			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var ua models.UserAchievement
					var a models.Achievement

					err := rows.Scan(
						&a.ID, &a.Name, &a.Description, &a.Icon, &a.PointsReward,
						&ua.EarnedAt,
					)
					if err == nil {
						ua.Achievement = &a
						ua.UserID = userID
						ua.AchievementID = a.ID
						achievements = append(achievements, ua)
					}
				}
			} else {
				log.Printf("Başarım sorgu hatası: %v", err)
			}
		} else {
			// Giriş yapmamış kullanıcıyı login sayfasına yönlendir
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Haftalık aktivite grafiği için veri
		chartData := models.ChartData{
			Labels: []string{"Pzt", "Sal", "Çar", "Per", "Cum", "Cmt", "Paz"},
			Datasets: []models.ChartDataset{
				{
					Label: "Çözülen Sorular",
					Data:  []int{0, 0, 0, 0, 0, 0, 0},
				},
			},
		}

		if isAuth && user != nil {
			// Haftalık aktivite verilerini doldur
			rows, err := db.Query(`
                SELECT EXTRACT(DOW FROM solved_at)::int as day, COUNT(*)::int as count
                FROM user_solutions
                WHERE user_id = $1 
                  AND solved_at > NOW() - INTERVAL '7 days'
                GROUP BY EXTRACT(DOW FROM solved_at)
            `, user.ID)

			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var day int
					var count int
					err := rows.Scan(&day, &count)
					if err == nil {
						// PostgreSQL'de Pazar=0, Pazartesi=1, ...
						// Pazartesi'den başlatmak için index'i ayarla
						index := (day + 6) % 7 // Pazartesi=0, Pazar=6
						if index >= 0 && index < 7 {
							chartData.Datasets[0].Data[index] = count
						}
					}
				}
			} else {
				log.Printf("Grafik verisi sorgu hatası: %v", err)
			}
		}
		var isAdmin bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, user.ID).Scan(&isAdmin)
		data := models.DashboardData{
			Title:           "Dashboard - CTF HACK PLATFORMU",
			User:            user,
			IsAuthenticated: isAuth,
			IsAdmin:         isAdmin,
			Stats:           stats,
			RecentActivity:  recentActivity,
			InProgress:      inProgress,
			Achievements:    achievements,
			ChartData:       chartData,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"add": func(a, b int) int {
				return a + b
			},
			"sub": func(a, b int) int {
				return a - b
			},
			"mul": func(a, b int) int {
				return a * b
			},
			"div": func(a, b int) int {
				if b == 0 {
					return 0
				}
				return a / b
			},
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
			"percentage": func(solved, total int) int {
				if total == 0 {
					return 0
				}
				return solved * 100 / total
			},
		}

		tmpl, err := template.New("dashboard.html").Funcs(funcMap).ParseFiles(
			"templates/dashboard.html",
			"templates/partials/activity_item.html",
			"templates/partials/machine_progress.html",
			"templates/partials/achievement_item.html",
		)

		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Sayfa yüklenemedi", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
			http.Error(w, "Sayfa oluşturulamadı", http.StatusInternalServerError)
		}
	}
}
