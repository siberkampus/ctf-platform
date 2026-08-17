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

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// UserSessionResponse - Oturum bilgileri için response yapısı
type UserSessionResponse struct {
	ID         int       `json:"id"`
	Device     string    `json:"device"`
	IP         string    `json:"ip"`
	Location   string    `json:"location"`
	LastActive time.Time `json:"last_active"`
	CreatedAt  time.Time `json:"created_at"`
	IsCurrent  bool      `json:"is_current"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// SettingsData - Ayarlar sayfası verileri
type SettingsData struct {
	Title           string
	User            *models.User
	IsAuthenticated bool
	Settings        models.UserSettings
	Sessions        []UserSessionResponse
	Security        SecurityData
	CurrentUser     *models.User
	IsAdmin         bool
}

// SecurityData - Güvenlik ayarları verileri
type SecurityData struct {
	TwoFactorEnabled bool `json:"two_factor_enabled"`
	EmailVerified    bool `json:"email_verified"`
}

// API Response yardımcıları
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeSuccessResponse(w http.ResponseWriter, data interface{}) {
	writeJSONResponse(w, http.StatusOK, APIResponse{Success: true, Data: data})
}

func writeErrorResponse(w http.ResponseWriter, status int, message string) {
	writeJSONResponse(w, status, APIResponse{Success: false, Message: message})
}

// SettingsPage - Ayarlar sayfası (HTML)
func SettingsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		isAuth := false
		var user *models.User
		var userSettings models.UserSettings
		var sessions []UserSessionResponse
		security := SecurityData{TwoFactorEnabled: false, EmailVerified: false}

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			userID, ok := session.Values["user_id"].(int)
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user = getUserByID(db, userID)
			if user == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			security.TwoFactorEnabled = user.TwoFactorEnabled

			// Kullanıcı ayarlarını getir veya oluştur
			err := db.QueryRow(`
				SELECT COALESCE(email_notifications, true), COALESCE(browser_notifications, true), 
				       COALESCE(sound_enabled, false), COALESCE(profile_public, true), 
				       COALESCE(show_activity, true), COALESCE(show_online_status, true),
				       COALESCE(theme, 'dark'), COALESCE(font_size, 'medium'), 
				       COALESCE(language, 'tr'), COALESCE(updated_at, NOW())
				FROM user_settings
				WHERE user_id = $1
			`, userID).Scan(
				&userSettings.EmailNotifications, &userSettings.BrowserNotifications, &userSettings.SoundEnabled,
				&userSettings.ProfilePublic, &userSettings.ShowActivity, &userSettings.ShowOnlineStatus,
				&userSettings.Theme, &userSettings.FontSize, &userSettings.Language,
				&userSettings.UpdatedAt,
			)

			if err != nil {
				if err == sql.ErrNoRows {
					userSettings = models.UserSettings{
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

					_, err = db.Exec(`
						INSERT INTO user_settings 
							(user_id, email_notifications, browser_notifications, sound_enabled,
							 profile_public, show_activity, show_online_status,
							 theme, font_size, language, updated_at)
						VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
					`, userSettings.UserID, userSettings.EmailNotifications, userSettings.BrowserNotifications,
						userSettings.SoundEnabled, userSettings.ProfilePublic, userSettings.ShowActivity,
						userSettings.ShowOnlineStatus, userSettings.Theme, userSettings.FontSize,
						userSettings.Language, userSettings.UpdatedAt)

					if err != nil {
						log.Printf("Varsayılan ayar ekleme hatası: %v", err)
					}
				} else {
					log.Printf("Ayar sorgu hatası: %v", err)
				}
			}

			// Aktif oturumları getir
			rows, err := db.Query(`
				SELECT id, COALESCE(device, 'Bilinmeyen Cihaz'), COALESCE(ip_address, '0.0.0.0'), 
				       COALESCE(location, 'Bilinmiyor'), last_activity, created_at, expires_at
				FROM user_sessions
				WHERE user_id = $1 AND is_active = true
				ORDER BY last_activity DESC
			`, userID)

			if err == nil {
				defer rows.Close()
				currentSessionID := session.Values["session_id"]
				for rows.Next() {
					var s UserSessionResponse
					var device, ip, location string

					err := rows.Scan(
						&s.ID, &device, &ip, &location, &s.LastActive, &s.CreatedAt, &s.ExpiresAt,
					)
					if err == nil {
						s.Device = device
						s.IP = ip
						s.Location = location

						if currentSessionID != nil {
							s.IsCurrent = strconv.Itoa(s.ID) == currentSessionID.(string)
						}
						sessions = append(sessions, s)
					}
				}
			}
		} else {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Admin kontrolü
		isAdmin := false
		if user != nil {
			var role string
			err := db.QueryRow(`SELECT COALESCE(role, 'user') FROM users WHERE id = $1`, user.ID).Scan(&role)
			if err == nil && role == "admin" {
				isAdmin = true
			}
		}

		data := SettingsData{
			Title:           "Ayarlar - CTF HACK PLATFORMU",
			CurrentUser:     user,
			User:            user,
			IsAuthenticated: isAuth,
			IsAdmin:         isAdmin,
			Settings:        userSettings,
			Sessions:        sessions,
			Security:        security,
		}

		funcMap := template.FuncMap{
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006 15:04")
			},
			"formatDuration": func(t time.Time) string {
				duration := time.Until(t)
				if duration.Hours() > 24 {
					days := int(duration.Hours() / 24)
					return strconv.Itoa(days) + " gün"
				} else if duration.Hours() > 1 {
					hours := int(duration.Hours())
					return strconv.Itoa(hours) + " saat"
				} else if duration.Minutes() > 1 {
					minutes := int(duration.Minutes())
					return strconv.Itoa(minutes) + " dakika"
				}
				return "süresi doldu"
			},
			"eq": func(a, b interface{}) bool {
				return a == b
			},
			"first": func(arr []UserSessionResponse, limit int) []UserSessionResponse {
				if len(arr) > limit {
					return arr[:limit]
				}
				return arr
			},
			"gt": func(a, b int) bool {
				return a > b
			},
		}

		tmpl, err := template.New("settings.html").Funcs(funcMap).ParseFiles("templates/settings.html")
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Sayfa yüklenemedi: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
			http.Error(w, "Sayfa oluşturulamadı: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// UpdateProfileHandler - Profil bilgilerini güncelle (API)
func UpdateProfileHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var req struct {
			Username string `json:"username"`
			Email    string `json:"email"`
			Bio      string `json:"bio"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		// Username kontrolü
		var exists bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND id != $2)`, req.Username, userID).Scan(&exists)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "Kullanıcı adı kontrol edilemedi")
			return
		}
		if exists {
			writeErrorResponse(w, http.StatusBadRequest, "Bu kullanıcı adı zaten kullanılıyor")
			return
		}

		// Email kontrolü
		err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1 AND id != $2)`, req.Email, userID).Scan(&exists)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "E-posta kontrol edilemedi")
			return
		}
		if exists {
			writeErrorResponse(w, http.StatusBadRequest, "Bu e-posta adresi zaten kullanılıyor")
			return
		}

		_, err = db.Exec(`
			UPDATE users SET username = $1, email = $2, bio = $3
			WHERE id = $4
		`, req.Username, req.Email, req.Bio, userID)

		if err != nil {
			log.Printf("Profil güncelleme hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "Profil güncellenemedi")
			return
		}

		// Session'daki username'i güncelle
		session.Values["username"] = req.Username
		session.Save(r, w)

		writeSuccessResponse(w, map[string]interface{}{
			"message": "Profil başarıyla güncellendi",
		})
	}
}

// UpdateSettingsHandler - Tüm ayarları güncelle (tek endpoint)
func UpdateSettingsHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var req struct {
			// Bildirim ayarları
			EmailNotifications   *bool `json:"email_notifications"`
			BrowserNotifications *bool `json:"browser_notifications"`
			SoundEnabled         *bool `json:"sound_enabled"`
			// Gizlilik ayarları
			ProfilePublic    *bool `json:"profile_public"`
			ShowActivity     *bool `json:"show_activity"`
			ShowOnlineStatus *bool `json:"show_online_status"`
			// Görünüm ayarları
			Theme    string `json:"theme"`
			FontSize string `json:"font_size"`
			Language string `json:"language"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		// Mevcut ayarları al
		var current models.UserSettings
		err := db.QueryRow(`
			SELECT COALESCE(email_notifications, true), COALESCE(browser_notifications, true), 
			       COALESCE(sound_enabled, false), COALESCE(profile_public, true), 
			       COALESCE(show_activity, true), COALESCE(show_online_status, true),
			       COALESCE(theme, 'dark'), COALESCE(font_size, 'medium'), 
			       COALESCE(language, 'tr')
			FROM user_settings WHERE user_id = $1
		`, userID).Scan(
			&current.EmailNotifications, &current.BrowserNotifications, &current.SoundEnabled,
			&current.ProfilePublic, &current.ShowActivity, &current.ShowOnlineStatus,
			&current.Theme, &current.FontSize, &current.Language,
		)

		if err != nil && err != sql.ErrNoRows {
			writeErrorResponse(w, http.StatusInternalServerError, "Ayarlar okunamadı")
			return
		}

		// Güncelleme yapılacak değerleri belirle
		emailNotif := current.EmailNotifications
		if req.EmailNotifications != nil {
			emailNotif = *req.EmailNotifications
		}
		browserNotif := current.BrowserNotifications
		if req.BrowserNotifications != nil {
			browserNotif = *req.BrowserNotifications
		}
		sound := current.SoundEnabled
		if req.SoundEnabled != nil {
			sound = *req.SoundEnabled
		}
		profilePublic := current.ProfilePublic
		if req.ProfilePublic != nil {
			profilePublic = *req.ProfilePublic
		}
		showActivity := current.ShowActivity
		if req.ShowActivity != nil {
			showActivity = *req.ShowActivity
		}
		showOnlineStatus := current.ShowOnlineStatus
		if req.ShowOnlineStatus != nil {
			showOnlineStatus = *req.ShowOnlineStatus
		}
		theme := current.Theme
		if req.Theme != "" {
			theme = req.Theme
		}
		fontSize := current.FontSize
		if req.FontSize != "" {
			fontSize = req.FontSize
		}
		language := current.Language
		if req.Language != "" {
			language = req.Language
		}

		_, err = db.Exec(`
			INSERT INTO user_settings (user_id, email_notifications, browser_notifications, sound_enabled,
				profile_public, show_activity, show_online_status, theme, font_size, language, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
			ON CONFLICT (user_id) DO UPDATE SET
				email_notifications = EXCLUDED.email_notifications,
				browser_notifications = EXCLUDED.browser_notifications,
				sound_enabled = EXCLUDED.sound_enabled,
				profile_public = EXCLUDED.profile_public,
				show_activity = EXCLUDED.show_activity,
				show_online_status = EXCLUDED.show_online_status,
				theme = EXCLUDED.theme,
				font_size = EXCLUDED.font_size,
				language = EXCLUDED.language,
				updated_at = EXCLUDED.updated_at
		`, userID, emailNotif, browserNotif, sound, profilePublic, showActivity, showOnlineStatus, theme, fontSize, language)

		if err != nil {
			log.Printf("Ayarlar güncelleme hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "Ayarlar güncellenemedi")
			return
		}

		writeSuccessResponse(w, map[string]interface{}{
			"message": "Ayarlar başarıyla güncellendi",
		})
	}
}

// UpdateSecurityHandler - Güvenlik ayarlarını güncelle (Şifre + 2FA)
func UpdateSecurityHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var req struct {
			CurrentPassword  string `json:"current_password"`
			NewPassword      string `json:"new_password"`
			TwoFactorEnabled *bool  `json:"two_factor_enabled"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Transaction hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "İşlem başlatılamadı")
			return
		}
		defer tx.Rollback()

		// Şifre değişikliği varsa
		if req.NewPassword != "" {
			if len(req.NewPassword) < 8 {
				writeErrorResponse(w, http.StatusBadRequest, "Yeni şifre en az 8 karakter olmalıdır")
				return
			}

			// Mevcut şifreyi kontrol et
			var currentHash string
			err = tx.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&currentHash)
			if err != nil {
				writeErrorResponse(w, http.StatusInternalServerError, "Şifre kontrol edilemedi")
				return
			}

			// bcrypt ile mevcut şifreyi kontrol et
			err = bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword))
			if err != nil {
				writeErrorResponse(w, http.StatusBadRequest, "Mevcut şifre yanlış")
				return
			}

			// Yeni şifreyi bcrypt ile hash'le
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
			if err != nil {
				log.Printf("Şifre hash'leme hatası: %v", err)
				writeErrorResponse(w, http.StatusInternalServerError, "Şifre güncellenemedi")
				return
			}

			_, err = tx.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, string(hashedPassword), userID)
			if err != nil {
				log.Printf("Şifre güncelleme hatası: %v", err)
				writeErrorResponse(w, http.StatusInternalServerError, "Şifre güncellenemedi")
				return
			}
		}

		// 2FA değişikliği varsa
		if req.TwoFactorEnabled != nil {
			_, err = tx.Exec(`UPDATE users SET two_factor_enabled = $1 WHERE id = $2`, *req.TwoFactorEnabled, userID)
			if err != nil {
				log.Printf("2FA güncelleme hatası: %v", err)
				writeErrorResponse(w, http.StatusInternalServerError, "2FA ayarı güncellenemedi")
				return
			}
		}

		// Activity log'a ekle
		_, err = tx.Exec(`
			INSERT INTO activity_logs (user_id, action_type, ip_address, created_at)
			VALUES ($1, 'security_update', $2, NOW())
		`, userID, r.RemoteAddr)
		if err != nil {
			log.Printf("Activity log hatası: %v", err)
		}

		if err = tx.Commit(); err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			writeErrorResponse(w, http.StatusInternalServerError, "İşlem tamamlanamadı")
			return
		}

		writeSuccessResponse(w, map[string]interface{}{
			"message": "Güvenlik ayarları başarıyla güncellendi",
		})
	}
}

// GetSessionsHandler - Kullanıcının aktif oturumlarını getir (API)
func GetSessionsHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		currentSessionID := session.Values["session_id"]

		rows, err := db.Query(`
			SELECT id, COALESCE(device, 'Bilinmeyen Cihaz'), COALESCE(ip_address, '0.0.0.0'), 
			       COALESCE(location, 'Bilinmiyor'), last_activity, created_at, expires_at
			FROM user_sessions
			WHERE user_id = $1 AND is_active = true
			ORDER BY last_activity DESC
		`, userID)

		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "Oturumlar getirilemedi")
			return
		}
		defer rows.Close()

		var sessions []UserSessionResponse
		for rows.Next() {
			var s UserSessionResponse
			var device, ip, location string

			err := rows.Scan(&s.ID, &device, &ip, &location, &s.LastActive, &s.CreatedAt, &s.ExpiresAt)
			if err != nil {
				continue
			}

			s.Device = device
			s.IP = ip
			s.Location = location
			if currentSessionID != nil {
				s.IsCurrent = strconv.Itoa(s.ID) == currentSessionID.(string)
			}
			sessions = append(sessions, s)
		}

		writeSuccessResponse(w, sessions)
	}
}

// TerminateSessionHandler - Belirtilen oturumu sonlandır (API)
func TerminateSessionHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		vars := mux.Vars(r)
		sessionIDStr := vars["id"]
		sessionID, err := strconv.Atoi(sessionIDStr)
		if err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Geçersiz oturum ID")
			return
		}

		// Mevcut oturumu kontrol et
		currentSessionID := session.Values["session_id"]

		if strconv.Itoa(sessionID) == currentSessionID {
			writeErrorResponse(w, http.StatusBadRequest, "Mevcut oturum sonlandırılamaz")
			return
		}

		result, err := db.Exec(`
			UPDATE user_sessions 
			SET is_active = false, terminated_at = NOW() 
			WHERE id = $1 AND user_id = $2 AND is_active = true
		`, sessionID, userID)

		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "Oturum sonlandırılamadı")
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			writeErrorResponse(w, http.StatusNotFound, "Oturum bulunamadı")
			return
		}

		writeSuccessResponse(w, map[string]interface{}{
			"message": "Oturum başarıyla sonlandırıldı",
		})
	}
}

// TerminateAllSessionsHandler - Diğer tüm oturumları sonlandır (API)
func TerminateAllSessionsHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		currentSessionID := session.Values["session_id"].(string)

		result, err := db.Exec(`
			UPDATE user_sessions 
			SET is_active = false, terminated_at = NOW() 
			WHERE user_id = $1 AND id::text != $2 AND is_active = true
		`, userID, currentSessionID)

		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "Oturumlar sonlandırılamadı")
			return
		}

		rowsAffected, _ := result.RowsAffected()

		writeSuccessResponse(w, map[string]interface{}{
			"message":    "Diğer tüm oturumlar sonlandırıldı",
			"terminated": rowsAffected,
		})
	}
}

// DeleteAccountHandler - Hesabı kalıcı olarak sil
func DeleteAccountHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "İşlem başlatılamadı")
			return
		}
		defer tx.Rollback()

		// Kullanıcıya ait tüm verileri sil
		queries := []string{
			"DELETE FROM user_sessions WHERE user_id = $1",
			"DELETE FROM user_settings WHERE user_id = $1",
			"DELETE FROM user_section_answers WHERE user_id = $1",
			"DELETE FROM user_practical_solutions WHERE user_id = $1",
			"DELETE FROM user_solutions WHERE user_id = $1",
			"DELETE FROM submissions WHERE user_id = $1",
			"DELETE FROM activity_logs WHERE user_id = $1",
			"DELETE FROM academy_progress WHERE user_id = $1",
			"DELETE FROM hint_usage WHERE user_id = $1",
			"DELETE FROM users WHERE id = $1",
		}

		for _, query := range queries {
			if _, err := tx.Exec(query, userID); err != nil {
				tx.Rollback()
				writeErrorResponse(w, http.StatusInternalServerError, "Hesap silinemedi: "+err.Error())
				return
			}
		}

		if err := tx.Commit(); err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "Hesap silinemedi")
			return
		}

		// Session'ı temizle
		session.Options.MaxAge = -1
		session.Values = make(map[interface{}]interface{})
		session.Save(r, w)

		writeSuccessResponse(w, map[string]interface{}{
			"message": "Hesabınız başarıyla silindi",
		})
	}
}

// UploadAvatarHandler - Profil fotoğrafı yükleme
func UploadAvatarHandler(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			writeErrorResponse(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		// Kullanıcı adını al (dosya adı için)
		var username string
		err := db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
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

		// Benzersiz dosya adı oluştur
		ext := filepath.Ext(header.Filename)
		if ext == "" {
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
		if err == nil && oldAvatar != "" && oldAvatar != "/static/images/avatar.png" {
			oldPath := strings.TrimPrefix(oldAvatar, "/")
			if err := os.Remove(oldPath); err != nil {
				log.Printf("Eski avatar silinemedi: %v", err)
			}
		}

		// Yeni avatar URL'i
		avatarURL := "/uploads/avatars/" + fileName

		// Veritabanında avatar yolunu güncelle
		_, err = db.Exec(`UPDATE users SET avatar = $1 WHERE id = $2`, avatarURL, userID)
		if err != nil {
			log.Printf("Avatar güncelleme hatası: %v", err)
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
