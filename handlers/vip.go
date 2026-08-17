package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"ctf-platform/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// PurchaseRequest - VIP satın alma isteği
type PurchaseRequest struct {
	Package       string `json:"package"`        // monthly, yearly, lifetime
	PaymentMethod string `json:"payment_method"` // credit_card, bank_transfer, etc.
	CampaignCode  string `json:"campaign_code,omitempty"`
}

// VIPStats - VIP istatistikleri
type VIPStats struct {
	TotalVIPUsers    int     `json:"total_vip_users"`
	TotalVIPMachines int     `json:"total_vip_machines"`
	AverageRating    float64 `json:"average_rating"`
	MonthlyVIPPrice  float64 `json:"monthly_price"`
	YearlyVIPPrice   float64 `json:"yearly_price"`
	LifetimePrice    float64 `json:"lifetime_price"`
}

// VIPBenefit - VIP avantajı
type VIPBenefit struct {
	Icon        string `json:"icon"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Note        string `json:"note"`
}

// Testimonial - Kullanıcı yorumu
type Testimonial struct {
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
	Text     string `json:"text"`
	Rating   int    `json:"rating"`
	Duration string `json:"duration"`
}

// VIPData - VIP sayfası verileri
// VIPData - VIP sayfası verileri
type VIPData struct {
	Title           string
	User            *models.User // Mevcut kullanıcı (giriş yapmışsa dolu)
	CurrentUser     *models.User // Aynı User, template uyumluluğu için
	IsAuthenticated bool
	IsVIP           bool
	VIPExpiry       *time.Time
	VIPStats        VIPStats
	Benefits        []VIPBenefit
	Testimonials    []Testimonial
	IsAdmin         bool // Admin kontrolü için
}

// GetVIPStatus - Kullanıcının VIP durumunu getir
func GetVIPStatus(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// X-User-ID header'ından kullanıcı ID'sini al
		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz kullanıcı ID")
			return
		}

		var isVIP bool
		var expiryDate sql.NullTime

		err = db.QueryRow(`
            SELECT is_vip, vip_expiry_date
            FROM users
            WHERE id = $1
        `, userID).Scan(&isVIP, &expiryDate)

		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Kullanıcı bulunamadı")
			} else {
				log.Printf("VIP durumu sorgu hatası: %v", err)
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			}
			return
		}

		// Kalan gün sayısını hesapla
		daysLeft := 0
		if expiryDate.Valid && isVIP {
			hours := time.Until(expiryDate.Time).Hours()
			if hours > 0 {
				daysLeft = int(hours / 24)
			} else {
				// Süresi dolmuş, VIP'i güncelle
				_, err = db.Exec(`UPDATE users SET is_vip = false WHERE id = $1`, userID)
				if err != nil {
					log.Printf("VIP süresi doldu güncelleme hatası: %v", err)
				}
				isVIP = false
				daysLeft = 0
			}
		}

		// VIP satın alma geçmişini getir
		var purchases []models.VIPPurchase
		rows, err := db.Query(`
            SELECT id, package, price, payment_method, purchased_at, expiry_date
            FROM vip_purchases
            WHERE user_id = $1
            ORDER BY purchased_at DESC
            LIMIT 5
        `, userID)

		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var p models.VIPPurchase
				err := rows.Scan(
					&p.ID, &p.Package, &p.Price, &p.PaymentMethod,
					&p.PurchasedAt, &p.ExpiryDate,
				)
				if err == nil {
					p.UserID = userID
					purchases = append(purchases, p)
				}
			}
		}

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"is_vip":            isVIP,
			"expiry_date":       expiryDate.Time,
			"expiry_date_valid": expiryDate.Valid,
			"days_remaining":    daysLeft,
			"purchases":         purchases,
		})
	}
}

// VIPPage - VIP üyelik sayfası
// VIPPage - VIP üyelik sayfası
func VIPPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")

		isAuth := false
		var user *models.User
		var isVIP bool
		var vipExpiry *time.Time
		isAdmin := false

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true

			if userID, ok := session.Values["user_id"].(int); ok {
				user = &models.User{}
				var vipExpiryDate sql.NullTime

				err := db.QueryRow(`
                    SELECT id, username, email, COALESCE(avatar, '/static/images/avatar.png'), 
                           is_vip, vip_expiry_date, COALESCE(points, 0)
                    FROM users 
                    WHERE id = $1
                `, userID).Scan(
					&user.ID, &user.Username, &user.Email, &user.Avatar,
					&user.IsVIP, &vipExpiryDate, &user.Points,
				)

				if err == nil {
					isVIP = user.IsVIP
					if vipExpiryDate.Valid {
						vipExpiry = &vipExpiryDate.Time
					}

					user.Country = ""
					user.Bio = ""
					user.Website = ""
					user.FullName = ""
					user.ReferralCode = ""
				}

				// Admin kontrolü - admins tablosundan kontrol et
				if user != nil && user.ID > 0 {
					var adminCount int
					db.QueryRow(`SELECT COUNT(*) FROM admins WHERE id = $1`, user.ID).Scan(&adminCount)
					isAdmin = (adminCount > 0)
				}
			}
		}

		// VIP istatistiklerini hesapla
		var stats VIPStats

		// Toplam VIP kullanıcı sayısı
		db.QueryRow(`
            SELECT COUNT(*) FROM users 
            WHERE is_vip = true AND (vip_expiry_date IS NULL OR vip_expiry_date > NOW())
        `).Scan(&stats.TotalVIPUsers)

		// VIP makinelerin sayısı
		db.QueryRow(`
            SELECT COUNT(*) FROM machines 
            WHERE is_vip_only = true AND is_active = true
        `).Scan(&stats.TotalVIPMachines)

		stats.AverageRating = 4.9

		// VIP planlarını vip_plans tablosundan çek
		type VIPPlan struct {
			ID           int      `json:"id"`
			Name         string   `json:"name"`
			Price        float64  `json:"price"`
			DurationDays int      `json:"duration_days"`
			Features     []string `json:"features"`
			Description  string   `json:"description"`
			IsActive     bool     `json:"is_active"`
			IsPopular    bool     `json:"is_popular"`
			DiscountNote string   `json:"discount_note"`
		}

		var vipPlans []VIPPlan

		rows, err := db.Query(`
			SELECT id, name, price, duration_days, COALESCE(features, '[]'), 
			       COALESCE(description, ''), is_active
			FROM vip_plans 
			WHERE is_active = true 
			ORDER BY "order" ASC, price ASC
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var plan VIPPlan
				var featuresJSON string
				rows.Scan(&plan.ID, &plan.Name, &plan.Price, &plan.DurationDays,
					&featuresJSON, &plan.Description, &plan.IsActive)

				// Features JSON'ını parse et
				if featuresJSON != "" && featuresJSON != "[]" {
					json.Unmarshal([]byte(featuresJSON), &plan.Features)
				} else {
					// Varsayılan özellikler
					plan.Features = []string{
						"Tüm VIP makinelere erişim",
						"Çift puan kazanma",
						"Özel VIP rozeti",
						"Öncelikli destek",
					}
				}

				// Yıllık planı popüler olarak işaretle
				if plan.DurationDays == 365 {
					plan.IsPopular = true
					plan.DiscountNote = "2 ay bedava"
				}

				vipPlans = append(vipPlans, plan)
			}
		}

		// Eğer veritabanında plan yoksa varsayılan planları kullan
		if len(vipPlans) == 0 {
			vipPlans = []VIPPlan{
				{
					ID: 1, Name: "Aylık", Price: 99.90, DurationDays: 30, IsActive: true,
					Features: []string{"Tüm VIP makinelere erişim", "Çift puan kazanma", "Özel VIP rozeti", "Öncelikli destek"},
				},
				{
					ID: 2, Name: "Yıllık", Price: 999.90, DurationDays: 365, IsActive: true, IsPopular: true, DiscountNote: "2 ay bedava",
					Features: []string{"Tüm VIP makinelere erişim", "Çift puan kazanma", "Özel VIP rozeti", "Öncelikli destek", "2 ay bedava", "Özel yıllık üye rozeti"},
				},
				{
					ID: 3, Name: "Ömür Boyu", Price: 2999.90, DurationDays: 36500, IsActive: true,
					Features: []string{"Tüm VIP makinelere erişim", "Çift puan kazanma", "Özel VIP rozeti", "Öncelikli destek", "Sınırsız süre", "Ömür boyu üye rozeti", "Tüm gelecek özellikler dahil"},
				},
			}
		}

		// İlk plan bilgilerini JavaScript için hazırla
		firstPlanName := ""
		firstPlanPrice := 0.0
		firstPlanDuration := 0
		if len(vipPlans) > 0 {
			firstPlanName = vipPlans[0].Name
			firstPlanPrice = vipPlans[0].Price
			firstPlanDuration = vipPlans[0].DurationDays
		}

		// VIP avantajları (sabit)
		benefits := []VIPBenefit{
			{Icon: "fa-lock-open", Title: "Özel VIP Makineler", Description: "Sadece VIP üyelere özel 50+ zorlu makineye erişim", Note: "Her ay yeni makineler ekleniyor"},
			{Icon: "fa-trophy", Title: "Çift Puan", Description: "Tüm çözümlerden 2 kat daha fazla puan kazanın", Note: "Normalde 100 puan, VIP'ler için 200 puan"},
			{Icon: "fa-medal", Title: "Özel Rozetler", Description: "VIP üyelere özel 10+ rozet ve profil görünümü", Note: "Profilinizde VIP rozeti görünsün"},
			{Icon: "fa-rocket", Title: "Öncelikli Destek", Description: "Sorularınıza öncelikli yanıt ve 7/24 destek", Note: "Ortalama 1 saat içinde yanıt"},
			{Icon: "fa-chart-line", Title: "Detaylı İstatistikler", Description: "Gelişmiş analitik ve performans grafikleri", Note: "Çözümlerinizin detaylı analizi"},
			{Icon: "fa-users", Title: "Özel Topluluk", Description: "VIP üyeler için özel Discord kanalı ve etkinlikler", Note: "Diğer VIP üyelerle tanışın"},
		}

		// Yorumlar
		testimonials := []Testimonial{
			{Username: "@root_hunter", Avatar: "/static/images/testimonials/user1.jpg", Text: "VIP makineler gerçekten zorlayıcı! Çift puan sayesinde leaderboard'da hızla yükseldim.", Rating: 5, Duration: "1 yıl"},
			{Username: "@byte_bender", Avatar: "/static/images/testimonials/user2.jpg", Text: "Özel Discord kanalı harika! Diğer VIP üyelerle fikir alışverişi yapmak çok değerli.", Rating: 5, Duration: "6 ay"},
			{Username: "@payload_master", Avatar: "/static/images/testimonials/user3.jpg", Text: "Öncelikli destek mükemmel çalışıyor. Sorun yaşadığımda çok hızlı çözüm alıyorum.", Rating: 5, Duration: "2 yıl"},
		}

		// Template verisi
		data := struct {
			Title             string
			User              *models.User
			CurrentUser       *models.User
			IsAuthenticated   bool
			IsVIP             bool
			VIPExpiry         *time.Time
			VIPStats          VIPStats
			Benefits          []VIPBenefit
			Testimonials      []Testimonial
			IsAdmin           bool
			VIPPlans          []VIPPlan
			FirstPlanName     string
			FirstPlanPrice    float64
			FirstPlanDuration int
		}{
			Title:             "VIP Üyelik - CTF HACK PLATFORMU",
			User:              user,
			CurrentUser:       user,
			IsAuthenticated:   isAuth,
			IsVIP:             isVIP,
			VIPExpiry:         vipExpiry,
			VIPStats:          stats,
			Benefits:          benefits,
			Testimonials:      testimonials,
			IsAdmin:           isAdmin,
			VIPPlans:          vipPlans,
			FirstPlanName:     firstPlanName,
			FirstPlanPrice:    firstPlanPrice,
			FirstPlanDuration: firstPlanDuration,
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"add": func(a, b int) int { return a + b },
			"sub": func(a, b int) int { return a - b },
			"mul": func(a, b int) int { return a * b },
			"div": func(a, b int) int {
				if b == 0 {
					return 0
				}
				return a / b
			},
			"formatPrice": func(price float64) string { return fmt.Sprintf("₺%.2f", price) },
			"formatDate":  func(t time.Time) string { return t.Format("02.01.2006") },
			"formatDaysLeft": func(t *time.Time) string {
				if t == nil {
					return "Süresiz"
				}
				days := int(time.Until(*t).Hours() / 24)
				if days < 0 {
					return "Süre Doldu"
				}
				return fmt.Sprintf("%d gün", days)
			},
			"loop": func(n int) []int {
				result := make([]int, n)
				for i := 0; i < n; i++ {
					result[i] = i
				}
				return result
			},
			"getMonthlyPrice": func(price float64, days int) string {
				if days == 0 {
					return ""
				}
				monthlyPrice := (price / float64(days)) * 30
				return fmt.Sprintf("(₺%.2f/ay)", monthlyPrice)
			},
			"field": func(obj interface{}, fieldName string) interface{} {
				val := reflect.ValueOf(obj)
				if val.Kind() == reflect.Ptr {
					val = val.Elem()
				}
				if val.Kind() != reflect.Struct {
					return nil
				}
				field := val.FieldByName(fieldName)
				if !field.IsValid() {
					return nil
				}
				return field.Interface()
			},
		}

		tmpl, err := template.New("vip.html").Funcs(funcMap).ParseFiles("templates/vip.html")
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Sayfa yüklenemedi", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
		}
	}
}

// PurchaseVIP - VIP satın alma işlemi (güncellenmiş)
func PurchaseVIP(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz kullanıcı ID")
			return
		}

		var req PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("purchase vip decode error: %v", err)
			writeError(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		// Plan bilgilerini veritabanından al
		var planID int
		var planName string
		var planPrice float64
		var durationDays int

		err = db.QueryRow(`
			SELECT id, name, price, duration_days 
			FROM vip_plans 
			WHERE name = $1 AND is_active = true
		`, req.Package).Scan(&planID, &planName, &planPrice, &durationDays)

		if err != nil {
			// Varsayılan fiyatlar
			prices := map[string]struct {
				Price float64
				Days  int
			}{
				"monthly":  {99.90, 30},
				"yearly":   {999.90, 365},
				"lifetime": {2999.90, 36500},
			}

			if p, ok := prices[req.Package]; ok {
				planPrice = p.Price
				durationDays = p.Days
			} else {
				writeError(w, http.StatusBadRequest, "Geçersiz paket tipi")
				return
			}
		}

		price := planPrice
		finalPrice := price

		// Kampanya kodu kontrolü
		if req.CampaignCode != "" {
			var discount int
			err := db.QueryRow(`
				SELECT discount_percent 
				FROM campaign_codes 
				WHERE code = $1 AND is_active = true 
				  AND expires_at > NOW()
			`, req.CampaignCode).Scan(&discount)

			if err == nil && discount > 0 {
				finalPrice = price * (1 - float64(discount)/100)
				log.Printf("Kampanya kodu uygulandı: %s, indirim: %d%%, yeni fiyat: %.2f",
					req.CampaignCode, discount, finalPrice)
			}
		}

		// VIP süresini hesapla
		var expiryDate time.Time
		now := time.Now()
		expiryDate = now.AddDate(0, 0, durationDays)

		// Transaction başlat
		tx, err := db.Begin()
		if err != nil {
			log.Printf("purchase vip transaction error: %v", err)
			writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			return
		}
		defer tx.Rollback()

		// Kullanıcıyı VIP yap
		_, err = tx.Exec(`
			UPDATE users 
			SET is_vip = true, vip_expiry_date = $1 
			WHERE id = $2
		`, expiryDate, userID)

		if err != nil {
			log.Printf("VIP güncelleme hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "VIP güncellenemedi")
			return
		}

		// Satın alma geçmişine ekle
		_, err = tx.Exec(`
			INSERT INTO vip_purchases 
				(user_id, package, plan_id, price, payment_method, purchased_at, expiry_date)
			VALUES ($1, $2, $3, $4, $5, NOW(), $6)
		`, userID, req.Package, planID, finalPrice, req.PaymentMethod, expiryDate)

		if err != nil {
			log.Printf("Satın alma kaydı hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "Satın alma kaydedilemedi")
			return
		}

		// Activity log'a ekle
		_, err = tx.Exec(`
			INSERT INTO activity_logs 
				(user_id, action_type, ip_address, created_at)
			VALUES ($1, $2, $3, NOW())
		`, userID, "vip_purchase", r.RemoteAddr)

		if err != nil {
			log.Printf("Activity log hatası: %v", err)
		}

		// Transaction'ı commit et
		err = tx.Commit()
		if err != nil {
			log.Printf("Transaction commit hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "İşlem tamamlanamadı")
			return
		}

		// Başarılı yanıt
		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"message":          "VIP üyelik başarıyla aktifleştirildi!",
			"expiry_date":      expiryDate,
			"package":          req.Package,
			"original_price":   price,
			"final_price":      finalPrice,
			"discount_applied": price-finalPrice > 0,
		})
	}
}

// AdminVIPManagement - Admin VIP yönetimi (JSON)
func AdminVIPManagement(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// VIP kullanıcıları listele
		rows, err := db.Query(`
            SELECT 
                u.id, u.username, u.email, u.is_vip, u.vip_expiry_date,
                COUNT(vp.id) as total_purchases,
                COALESCE(SUM(vp.price), 0) as total_spent,
                MAX(vp.purchased_at) as last_purchase
            FROM users u
            LEFT JOIN vip_purchases vp ON u.id = vp.user_id
            WHERE u.is_vip = true
            GROUP BY u.id
            ORDER BY u.vip_expiry_date DESC
            LIMIT 50
        `)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var vipUsers []map[string]interface{}
		for rows.Next() {
			var id int
			var username, email string
			var isVIP bool
			var expiryDate sql.NullTime
			var totalPurchases int
			var totalSpent float64
			var lastPurchase sql.NullTime

			err := rows.Scan(&id, &username, &email, &isVIP, &expiryDate, &totalPurchases, &totalSpent, &lastPurchase)
			if err != nil {
				continue
			}

			vipUsers = append(vipUsers, map[string]interface{}{
				"id":                id,
				"username":          username,
				"email":             email,
				"is_vip":            isVIP,
				"expiry_date":       expiryDate.Time,
				"expiry_date_valid": expiryDate.Valid,
				"total_purchases":   totalPurchases,
				"total_spent":       totalSpent,
				"last_purchase":     lastPurchase.Time,
			})
		}

		// Toplam VIP geliri
		var totalRevenue float64
		db.QueryRow(`SELECT COALESCE(SUM(price), 0) FROM vip_purchases`).Scan(&totalRevenue)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vip_users":     vipUsers,
			"total_revenue": totalRevenue,
			"total_vip":     len(vipUsers),
		})
	}
}

// AdminVIPPurchases - VIP satın alma geçmişi (JSON)
func AdminVIPPurchases(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
            SELECT 
                vp.id, vp.user_id, u.username, u.email,
                vp.package, vp.price, vp.payment_method,
                vp.purchased_at, vp.expiry_date
            FROM vip_purchases vp
            JOIN users u ON vp.user_id = u.id
            ORDER BY vp.purchased_at DESC
            LIMIT 100
        `)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var purchases []map[string]interface{}
		for rows.Next() {
			var id, userID int
			var username, email, package_, paymentMethod string
			var price float64
			var purchasedAt, expiryDate time.Time

			err := rows.Scan(&id, &userID, &username, &email, &package_, &price, &paymentMethod, &purchasedAt, &expiryDate)
			if err != nil {
				continue
			}

			purchases = append(purchases, map[string]interface{}{
				"id":             id,
				"user_id":        userID,
				"username":       username,
				"email":          email,
				"package":        package_,
				"price":          price,
				"payment_method": paymentMethod,
				"purchased_at":   purchasedAt,
				"expiry_date":    expiryDate,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(purchases)
	}
}

func AdminVIPPlans(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
            SELECT id, name, price, duration_days, features, is_active
            FROM vip_plans ORDER BY price
        `)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var plans []map[string]interface{}
		for rows.Next() {
			var id int
			var name string
			var price float64
			var duration int
			var features string
			var isActive bool
			rows.Scan(&id, &name, &price, &duration, &features, &isActive)
			plans = append(plans, map[string]interface{}{
				"id":            id,
				"name":          name,
				"price":         price,
				"duration_days": duration,
				"features":      features,
				"is_active":     isActive,
			})
		}

		json.NewEncoder(w).Encode(plans)
	}
}

func AdminCreateVIPPlan(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var plan struct {
			Name        string   `json:"name"`
			Price       float64  `json:"price"`
			Duration    int      `json:"duration_days"`
			Features    []string `json:"features"`
			Description string   `json:"description"`
			IsActive    bool     `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
			http.Error(w, "Geçersiz istek", http.StatusBadRequest)
			return
		}

		// Validasyon
		if plan.Name == "" || plan.Price <= 0 || plan.Duration <= 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Plan adı, fiyat ve süre zorunludur",
			})
			return
		}

		// Features array'ini string'e çevir
		featuresJSON, err := json.Marshal(plan.Features)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var planID int
		err = db.QueryRow(`
            INSERT INTO vip_plans (name, price, duration_days, features, description, is_active, created_at)
            VALUES ($1, $2, $3, $4, $5, $6, NOW())
            RETURNING id
        `, plan.Name, plan.Price, plan.Duration, string(featuresJSON),
			plan.Description, plan.IsActive).Scan(&planID)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"plan_id": planID,
			"message": "VIP planı oluşturuldu",
		})
	}
}

// AdminUpdateVIPPlan API - VIP planı güncelle
func AdminUpdateVIPPlan(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		planName := vars["id"] // "monthly", "yearly", "lifetime" veya "Aylık", "Yıllık" gelebilir

		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		// Plan ID'sini bul (isim veya ID olarak gelebilir)
		var existingPlanID int
		var existingPlanName string

		// Önce planın var olduğunu kontrol et
		if id, err := strconv.Atoi(planName); err == nil {
			// Sayısal ID geldi
			err = db.QueryRow("SELECT id, name FROM vip_plans WHERE id = $1", id).Scan(&existingPlanID, &existingPlanName)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "Plan bulunamadı")
				} else {
					log.Printf("Plan sorgulama hatası: %v", err)
					writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
				}
				return
			}
		} else {
			// String name geldi (monthly, yearly, lifetime veya Türkçe isim)
			var turkishName string
			switch planName {
			case "monthly":
				turkishName = "Aylık"
			case "yearly":
				turkishName = "Yıllık"
			case "lifetime":
				turkishName = "Ömür Boyu"
			default:
				turkishName = planName
			}

			err := db.QueryRow(`
				SELECT id, name FROM vip_plans 
				WHERE LOWER(name) = LOWER($1) OR name = $2
			`, planName, turkishName).Scan(&existingPlanID, &existingPlanName)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "Plan bulunamadı")
				} else {
					log.Printf("Plan sorgulama hatası: %v", err)
					writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
				}
				return
			}
		}

		// Features varsa JSON'a çevir
		if features, ok := updates["features"]; ok {
			if featuresList, ok := features.([]interface{}); ok {
				featuresJSON, err := json.Marshal(featuresList)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "Features JSON dönüştürme hatası")
					return
				}
				updates["features"] = string(featuresJSON)
			}
		}

		// Güncelleme sorgusunu oluştur
		query := "UPDATE vip_plans SET "
		var args []interface{}
		argCount := 1
		allowedFields := []string{"name", "price", "duration_days", "features", "description", "is_active", "\"order\""}

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

		if argCount == 1 {
			writeSuccess(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "Güncellenecek alan bulunamadı",
			})
			return
		}

		query += " WHERE id = $" + strconv.Itoa(argCount)
		args = append(args, existingPlanID)

		result, err := db.Exec(query, args...)
		if err != nil {
			log.Printf("Plan güncelleme hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			writeSuccess(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "Plan bulunamadı",
			})
			return
		}

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "VIP planı güncellendi",
		})
	}
}

// AdminDeleteVIPPlan - VIP planı silme API'si
func AdminDeleteVIPPlan(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		planID := vars["id"]

		// Plan ID'sini bul (isim veya ID olarak gelebilir)
		var existingPlanID int
		var planName string
		fmt.Println("Plan ID:", planID)
		// Önce planın var olduğunu kontrol et
		if id, err := strconv.Atoi(planID); err == nil {
			// Sayısal ID geldi
			err = db.QueryRow("SELECT id, name FROM vip_plans WHERE name = $1", id).Scan(&existingPlanID, &planName)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "Plan bulunamadı")
				} else {
					log.Printf("Plan sorgulama hatası: %v", err)
					writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
				}
				return
			}
		} else {
			// String name geldi (monthly, yearly, lifetime)
			var turkishName string
			switch planID {
			case "monthly":
				turkishName = "Aylık"
			case "yearly":
				turkishName = "Yıllık"
			case "lifetime":
				turkishName = "Ömür Boyu"
			default:
				turkishName = planID
			}

			err = db.QueryRow(`
				SELECT id, name FROM vip_plans 
				WHERE LOWER(name) = LOWER($1) OR name = $2
			`, planID, turkishName).Scan(&existingPlanID, &planName)
			if err != nil {
				if err == sql.ErrNoRows {
					writeError(w, http.StatusNotFound, "Plan bulunamadı")
				} else {
					log.Printf("Plan sorgulama hatası: %v", err)
					writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
				}
				return
			}
		}

		// Bu plana ait satın alma var mı kontrol et
		var purchaseCount int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM vip_purchases 
			WHERE LOWER(package) = LOWER($1)
		`, planName).Scan(&purchaseCount)
		if err != nil {
			log.Printf("Satın alma kontrol hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			return
		}

		if purchaseCount > 0 {
			// Satın alma varsa silme, sadece pasif yap
			_, err = db.Exec("UPDATE vip_plans SET is_active = false WHERE id = $1", existingPlanID)
			if err != nil {
				log.Printf("Plan güncelleme hatası: %v", err)
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
				return
			}
			writeSuccess(w, http.StatusOK, map[string]interface{}{
				"message": "Bu plana ait satın almalar olduğu için plan pasif hale getirildi",
			})
			return
		}

		// Satın alma yoksa direkt sil
		_, err = db.Exec("DELETE FROM vip_plans WHERE id = $1", existingPlanID)
		if err != nil {
			log.Printf("Plan silme hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			return
		}

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"message": "VIP planı silindi",
		})
	}
}

func AdminVIPPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		// VIP istatistiklerini getir
		var stats struct {
			TotalVIP       int
			ActiveVIP      int
			TotalRevenue   float64
			MonthlyRevenue float64
			ExpiringSoon   int
		}

		// Toplam VIP kullanıcı
		db.QueryRow("SELECT COUNT(*) FROM users WHERE is_vip = true").Scan(&stats.TotalVIP)

		// Aktif VIP (süresi dolmamış)
		db.QueryRow("SELECT COUNT(*) FROM users WHERE is_vip = true AND (vip_expiry_date > NOW() OR vip_expiry_date IS NULL)").Scan(&stats.ActiveVIP)

		// Toplam gelir (vip_purchases tablosundan)
		db.QueryRow("SELECT COALESCE(SUM(price), 0) FROM vip_purchases").Scan(&stats.TotalRevenue)

		// Aylık gelir
		db.QueryRow("SELECT COALESCE(SUM(price), 0) FROM vip_purchases WHERE purchased_at > NOW() - INTERVAL '30 days'").Scan(&stats.MonthlyRevenue)

		// Yakında bitecek VIP'ler (7 gün içinde)
		db.QueryRow(`
			SELECT COUNT(*) FROM users 
			WHERE is_vip = true 
			AND vip_expiry_date BETWEEN NOW() AND NOW() + INTERVAL '7 days'
		`).Scan(&stats.ExpiringSoon)

		// VIP PLANLARINI vip_plans TABLOSUNDAN ÇEK
		rows, err := db.Query(`
			SELECT id, name, price, duration_days, COALESCE(features, '[]'), 
			       COALESCE(description, ''), is_active, "order"
			FROM vip_plans 
			ORDER BY "order" ASC, price ASC
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var packages []map[string]interface{}
		for rows.Next() {
			var id int
			var name string
			var price float64
			var durationDays int
			var featuresJSON string
			var description string
			var isActive bool
			var order int

			rows.Scan(&id, &name, &price, &durationDays, &featuresJSON, &description, &isActive, &order)

			// Features JSON'ını parse et
			var features []string
			if featuresJSON != "" && featuresJSON != "[]" {
				json.Unmarshal([]byte(featuresJSON), &features)
			}

			// Aktif abone ve satış sayılarını hesapla (vip_purchases'tan)
			var subscriberCount, purchaseCount int
			db.QueryRow(`
				SELECT COUNT(DISTINCT user_id), COUNT(*) 
				FROM vip_purchases 
				WHERE package = $1 AND expiry_date > NOW()
			`, name).Scan(&subscriberCount, &purchaseCount)

			// Paket tipine göre ID belirle (frontend uyumluluğu için)
			packageID := ""
			switch {
			case durationDays <= 31:
				packageID = "monthly"
			case durationDays <= 366:
				packageID = "yearly"
			default:
				packageID = "lifetime"
			}

			packages = append(packages, map[string]interface{}{
				"ID":              packageID,
				"PlanID":          id,
				"Name":            name,
				"Price":           price,
				"DurationDays":    durationDays,
				"IsActive":        isActive,
				"SubscriberCount": subscriberCount,
				"PurchaseCount":   purchaseCount,
				"Features":        features,
				"Description":     description,
				"Order":           order,
			})
		}

		// Son VIP satışlarını getir
		rows, err = db.Query(`
			SELECT vp.id, u.id, u.username, COALESCE(u.avatar, '/static/images/avatar.png'), 
			       vp.package, vp.price, vp.purchased_at, vp.expiry_date
			FROM vip_purchases vp
			JOIN users u ON vp.user_id = u.id
			ORDER BY vp.purchased_at DESC
			LIMIT 10
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var purchases []map[string]interface{}
		for rows.Next() {
			var id, userID int
			var username, pkg, avatar string
			var price float64
			var purchasedAt, expiryDate time.Time

			rows.Scan(&id, &userID, &username, &avatar, &pkg, &price, &purchasedAt, &expiryDate)

			purchases = append(purchases, map[string]interface{}{
				"ID":          id,
				"UserID":      userID,
				"Username":    username,
				"UserAvatar":  avatar,
				"PlanName":    pkg,
				"Price":       price,
				"PurchasedAt": purchasedAt,
				"ExpiryDate":  expiryDate,
				"IsActive":    expiryDate.After(time.Now()),
			})
		}

		// Yakında bitecek VIP'ler
		rows, err = db.Query(`
			SELECT u.id, u.username, u.email, vp.package, vp.expiry_date
			FROM users u
			JOIN vip_purchases vp ON u.id = vp.user_id
			WHERE u.is_vip = true 
			AND vp.expiry_date BETWEEN NOW() AND NOW() + INTERVAL '7 days'
			ORDER BY vp.expiry_date ASC
			LIMIT 10
		`)

		var expiringVIPs []map[string]interface{}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var username, email, pkg string
				var expiryDate time.Time
				rows.Scan(&id, &username, &email, &pkg, &expiryDate)

				daysLeft := int(time.Until(expiryDate).Hours() / 24)

				expiringVIPs = append(expiringVIPs, map[string]interface{}{
					"ID":         id,
					"Username":   username,
					"Email":      email,
					"PlanName":   pkg,
					"ExpiryDate": expiryDate,
					"DaysLeft":   daysLeft,
				})
			}
		}

		// Admin bilgileri
		admin := struct {
			Username string
			Role     string
			Avatar   string
		}{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		// Template fonksiyonları
		funcMap := template.FuncMap{
			"formatMoney": func(amount float64) string {
				return fmt.Sprintf("₺%.2f", amount)
			},
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006")
			},
			"daysLeft": func(expiryDate time.Time) int {
				return int(time.Until(expiryDate).Hours() / 24)
			},
			"calculateDiscount": func(days int) int {
				if days >= 365 {
					return 25
				} else if days >= 90 {
					return 15
				} else if days >= 30 {
					return 0
				}
				return 0
			},
		}

		// Template verisi
		data := struct {
			Title        string
			Active       string
			Admin        interface{}
			Stats        interface{}
			Packages     []map[string]interface{}
			Purchases    []map[string]interface{}
			ExpiringVIPs []map[string]interface{}
		}{
			Title:        "VIP Yönetimi - Admin Panel",
			Active:       "vip",
			Admin:        admin,
			Stats:        stats,
			Packages:     packages,
			Purchases:    purchases,
			ExpiringVIPs: expiringVIPs,
		}

		tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/vip.html",
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
