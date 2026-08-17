package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ctf-platform/models"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// AcademyHomePage - Akademi ana sayfası
func AcademyHomePage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	funcMap := template.FuncMap{
		"percentage": func(completed, total int) int {
			if total == 0 {
				return 0
			}
			return completed * 100 / total
		},
		"difficultyClass": func(d string) string {
			switch d {
			case "beginner":
				return "beginner"
			case "intermediate":
				return "intermediate"
			case "advanced":
				return "advanced"
			case "expert":
				return "expert"
			}
			return "beginner"
		},
		"difficultyLabel": func(d string) string {
			switch d {
			case "beginner":
				return "Başlangıç"
			case "intermediate":
				return "Orta"
			case "advanced":
				return "İleri"
			case "expert":
				return "Uzman"
			}
			return d
		},
	}

	tmpl := template.Must(template.New("academy.html").Funcs(funcMap).ParseFiles("templates/academy.html"))

	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		isAuth := false
		var user *models.User
		var isAdmin bool
		var userID int

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			userID = session.Values["user_id"].(int)

			user = &models.User{}
			var avatar sql.NullString
			err := db.QueryRow(`
				SELECT id, username, email, COALESCE(avatar, '/static/images/avatar.png'), 
					   points, is_vip
				FROM users WHERE id = $1
			`, userID).Scan(
				&user.ID, &user.Username, &user.Email, &avatar,
				&user.Points, &user.IsVIP,
			)
			if err == nil {
				user.Avatar = avatar.String
			}

			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM admins WHERE id = $1)`, userID).Scan(&isAdmin)
		}

		type LessonPreview struct {
			ID          int
			Title       string
			Slug        string
			Difficulty  string
			Duration    int
			Points      int
			TotalPoints int
			IsCompleted bool
		}

		type CategoryWithLessons struct {
			ID             int
			Name           string
			Slug           string
			Description    string
			Icon           string
			SortOrder      int
			LessonCount    int
			CompletedCount int
			Lessons        []LessonPreview
		}

		rows, err := db.Query(`
			SELECT c.id, c.name, c.slug, COALESCE(c.description, '') as description,
				   COALESCE(c.icon, 'fa-book') as icon, c.sort_order,
				   COUNT(l.id) as lesson_count,
				   COALESCE(SUM(CASE WHEN p.is_completed = true THEN 1 ELSE 0 END), 0) as completed_count
			FROM academy_categories c
			LEFT JOIN academy_lessons l ON c.id = l.category_id AND l.is_published = true
			LEFT JOIN academy_progress p ON l.id = p.lesson_id AND p.user_id = $1
			WHERE c.is_active = true
			GROUP BY c.id, c.name, c.slug, c.description, c.icon, c.sort_order
			ORDER BY c.sort_order
		`, userID)
		if err != nil {
			log.Printf("Kategori sorgu hatası: %v", err)
			http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var categories []CategoryWithLessons
		categoryIndex := map[int]int{}

		for rows.Next() {
			var cat CategoryWithLessons
			if err := rows.Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.Description,
				&cat.Icon, &cat.SortOrder, &cat.LessonCount, &cat.CompletedCount); err != nil {
				continue
			}
			categoryIndex[cat.ID] = len(categories)
			categories = append(categories, cat)
		}

		rows2, err := db.Query(`
			SELECT 
				l.id, l.title, l.slug, 
				COALESCE(l.difficulty, 'beginner'),
				l.duration_minutes, 
				l.points_reward,
				l.points_reward 
					+ COALESCE((SELECT SUM(points_reward) FROM section_questions WHERE lesson_id = l.id AND is_active = true), 0)
					+ COALESCE((SELECT SUM(points_reward) FROM practical_questions WHERE lesson_id = l.id AND is_active = true), 0)
				AS total_points,
				l.category_id,
				COALESCE(p.is_completed, false)
			FROM academy_lessons l
			LEFT JOIN academy_progress p ON l.id = p.lesson_id AND p.user_id = $1
			WHERE l.is_published = true
			ORDER BY l.category_id, l.sort_order
		`, userID)

		if err == nil {
			defer rows2.Close()
			for rows2.Next() {
				var lesson LessonPreview
				var categoryID int
				if err := rows2.Scan(
					&lesson.ID, &lesson.Title, &lesson.Slug, &lesson.Difficulty,
					&lesson.Duration, &lesson.Points, &lesson.TotalPoints,
					&categoryID, &lesson.IsCompleted,
				); err != nil {
					continue
				}
				if idx, ok := categoryIndex[categoryID]; ok {
					categories[idx].Lessons = append(categories[idx].Lessons, lesson)
				}
			}
		}

		var totalLessons, totalCompleted, totalPoints int
		db.QueryRow(`SELECT COUNT(*) FROM academy_lessons WHERE is_published = true`).Scan(&totalLessons)

		if isAuth {
			db.QueryRow(`
				SELECT COUNT(*) FROM academy_progress 
				WHERE user_id = $1 AND is_completed = true
			`, userID).Scan(&totalCompleted)

			db.QueryRow(`
				SELECT COALESCE(SUM(points_awarded), 0) FROM user_section_answers 
				WHERE user_id = $1
			`, userID).Scan(&totalPoints)
		}

		data := struct {
			Title           string
			User            *models.User
			IsAdmin         bool
			IsAuthenticated bool
			Categories      []CategoryWithLessons
			TotalLessons    int
			TotalCompleted  int
			TotalPoints     int
			Active          string
			CurrentYear     int
		}{
			Title:           "Akademi - CTF HACK PLATFORMU",
			User:            user,
			IsAdmin:         isAdmin,
			IsAuthenticated: isAuth,
			Categories:      categories,
			TotalLessons:    totalLessons,
			TotalCompleted:  totalCompleted,
			TotalPoints:     totalPoints,
			Active:          "academy",
			CurrentYear:     time.Now().Year(),
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template render hatası: %v", err)
			http.Error(w, "Sayfa render edilemedi: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// AcademyLessonPage - Ders detay sayfası
func AcademyLessonPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	// Template fonksiyonları
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"add":      func(a, b int) int { return a + b },
		"difficultyLabel": func(d string) string {
			switch d {
			case "beginner":
				return "Başlangıç"
			case "intermediate":
				return "Orta"
			case "advanced":
				return "İleri"
			case "expert":
				return "Uzman"
			case "easy":
				return "Kolay"
			case "medium":
				return "Orta"
			case "hard":
				return "Zor"
			}
			return d
		},
		"difficultyClass": func(d string) string {
			switch d {
			case "beginner", "easy":
				return "easy"
			case "intermediate", "medium":
				return "medium"
			case "advanced", "hard":
				return "hard"
			case "expert":
				return "expert"
			}
			return "easy"
		},
	}

	tmpl := template.Must(template.New("lesson.html").Funcs(funcMap).ParseFiles("templates/lesson.html"))

	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		slug := vars["slug"]

		session, _ := store.Get(r, "session")
		isAuth := false
		isAdmin := false
		var user *models.User
		var userID int

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			userID, _ = session.Values["user_id"].(int)
			user = getUserByID(db, userID)
			// Admin kontrolü - users tablosundaki role sütununa bak
			if user != nil {
				var role string
				err := db.QueryRow(`SELECT COALESCE(role, 'user') FROM users WHERE id = $1`, userID).Scan(&role)
				if err == nil && role == "admin" {
					isAdmin = true
				}
			}
		}
		var lesson struct {
			ID           int
			Title        string
			Content      string
			Slug         string
			CategoryID   int
			CategoryName string
			CategorySlug string
			Difficulty   string
			Duration     int
			PointsReward int
			VideoURL     string
			ViewCount    int
			PublishedAt  sql.NullTime
		}

		err := db.QueryRow(`
			SELECT l.id, l.title, l.content, l.slug, l.category_id, c.name, c.slug,
				   COALESCE(l.difficulty, 'beginner'), l.duration_minutes, l.points_reward,
				   COALESCE(l.video_url, ''), l.view_count, l.published_at
			FROM academy_lessons l
			JOIN academy_categories c ON l.category_id = c.id
			WHERE l.slug = $1 AND l.is_published = true
		`, slug).Scan(
			&lesson.ID, &lesson.Title, &lesson.Content, &lesson.Slug,
			&lesson.CategoryID, &lesson.CategoryName, &lesson.CategorySlug,
			&lesson.Difficulty, &lesson.Duration, &lesson.PointsReward,
			&lesson.VideoURL, &lesson.ViewCount, &lesson.PublishedAt,
		)

		if err != nil {
			http.Error(w, "Ders bulunamadı", http.StatusNotFound)
			return
		}

		// Görüntülenme sayısını arttır (goroutine ile - hata olursa sorun çıkarma)
		go func() {
			db.Exec(`UPDATE academy_lessons SET view_count = view_count + 1 WHERE id = $1`, lesson.ID)
		}()

		var isCompleted bool
		if isAuth {
			db.QueryRow(`
				SELECT COALESCE(is_completed, false) FROM academy_progress
				WHERE user_id = $1 AND lesson_id = $2
			`, userID, lesson.ID).Scan(&isCompleted)
		}

		type SectionQuestion struct {
			ID            int
			QuestionText  string
			OptionA       string
			OptionB       string
			OptionC       string
			OptionD       string
			CorrectAnswer string
			Explanation   string
			PointsReward  int
			Solved        bool
		}

		// Çözülmüş section sorularını bul
		solvedSectionIDs := map[int]bool{}
		if isAuth {
			sRows, err := db.Query(`
				SELECT DISTINCT question_id FROM user_section_answers
				WHERE user_id = $1
			`, userID)
			if err == nil {
				defer sRows.Close()
				for sRows.Next() {
					var qid int
					if err := sRows.Scan(&qid); err == nil {
						solvedSectionIDs[qid] = true
					}
				}
			}
		}

		// Section sorularını çek
		rows, err := db.Query(`
			SELECT id, question_text, option_a, option_b,
				   COALESCE(option_c, ''), COALESCE(option_d, ''),
				   correct_answer, COALESCE(explanation, ''), points_reward
			FROM section_questions
			WHERE lesson_id = $1 AND is_active = true
			ORDER BY sort_order
		`, lesson.ID)

		var sectionQuestions []SectionQuestion
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var q SectionQuestion
				if err := rows.Scan(&q.ID, &q.QuestionText, &q.OptionA, &q.OptionB, &q.OptionC, &q.OptionD,
					&q.CorrectAnswer, &q.Explanation, &q.PointsReward); err != nil {
					continue
				}
				q.Solved = solvedSectionIDs[q.ID]
				sectionQuestions = append(sectionQuestions, q)
			}
		}

		type PracticalQuestion struct {
			ID           int
			Title        string
			Description  string
			DockerImage  string
			PointsReward int
			Difficulty   string
			Hint         string
			HintCost     int
			Solved       bool
		}

		// Çözülmüş practical sorularını bul
		solvedPracticalIDs := map[int]bool{}
		if isAuth {
			pRows, err := db.Query(`
				SELECT DISTINCT question_id FROM user_practical_solutions
				WHERE user_id = $1 AND is_correct = true
			`, userID)
			if err == nil {
				defer pRows.Close()
				for pRows.Next() {
					var qid int
					if err := pRows.Scan(&qid); err == nil {
						solvedPracticalIDs[qid] = true
					}
				}
			}
		}

		// Practical sorularını çek
		rows2, err := db.Query(`
			SELECT id, title, description, COALESCE(docker_image, ''), points_reward, difficulty,
				   COALESCE(hint, ''), hint_cost
			FROM practical_questions
			WHERE lesson_id = $1 AND is_active = true
			ORDER BY id
		`, lesson.ID)

		var practicalQuestions []PracticalQuestion
		if err == nil {
			defer rows2.Close()
			for rows2.Next() {
				var q PracticalQuestion
				if err := rows2.Scan(&q.ID, &q.Title, &q.Description, &q.DockerImage, &q.PointsReward,
					&q.Difficulty, &q.Hint, &q.HintCost); err != nil {
					continue
				}
				q.Solved = solvedPracticalIDs[q.ID]
				practicalQuestions = append(practicalQuestions, q)
			}
		}
		// PublishedAt için güvenli zaman değeri
		var publishedAt time.Time
		if lesson.PublishedAt.Valid {
			publishedAt = lesson.PublishedAt.Time
		}

		// Template verisi - IsAuth yerine IsAuthenticated kullanıldı
		data := struct {
			Title              string
			IsAuthenticated    bool
			IsAdmin            bool
			User               *models.User
			Lesson             interface{}
			CategoryName       string
			CategorySlug       string
			SectionQuestions   []SectionQuestion
			PracticalQuestions []PracticalQuestion
			IsCompleted        bool
			LessonID           int
			PublishedAt        time.Time
		}{
			Title:              lesson.Title + " - Akademi",
			IsAuthenticated:    isAuth,
			IsAdmin:            isAdmin,
			User:               user,
			Lesson:             lesson,
			CategoryName:       lesson.CategoryName,
			CategorySlug:       lesson.CategorySlug,
			SectionQuestions:   sectionQuestions,
			PracticalQuestions: practicalQuestions,
			IsCompleted:        isCompleted,
			LessonID:           lesson.ID,
			PublishedAt:        publishedAt,
		}

		// Template'i çalıştır
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template render hatası: %v", err)
			http.Error(w, "Sayfa render edilemedi: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// APIAnswerSectionQuestion - Test sorusunu cevapla
func APIAnswerSectionQuestion(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		questionID, _ := strconv.Atoi(vars["questionId"])

		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Giriş yapmalısınız",
			})
			return
		}

		var req struct {
			Answer string `json:"answer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Geçersiz istek",
			})
			return
		}

		// Soruyu kontrol et
		var correctAnswer string
		var pointsReward int
		var lessonID int
		err := db.QueryRow(`
			SELECT correct_answer, points_reward, lesson_id 
			FROM section_questions WHERE id = $1
		`, questionID).Scan(&correctAnswer, &pointsReward, &lessonID)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Soru bulunamadı",
			})
			return
		}

		// Daha önce cevaplanmış mı?
		var exists bool
		db.QueryRow(`
    SELECT EXISTS(SELECT 1 FROM user_section_answers 
    WHERE user_id = $1 AND question_id = $2 AND is_correct = true)
`, userID, questionID).Scan(&exists)

		if exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Bu soruyu zaten cevapladınız",
			})
			return
		}

		isCorrect := strings.ToUpper(req.Answer) == correctAnswer
		points := 0
		if isCorrect {
			points = pointsReward
			//db.Exec(`UPDATE users SET points = points + $1 WHERE id = $2`, points, userID)
		}
		if !isCorrect {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"correct": false,
				"points":  0,
				"message": "Yanlış cevap! Tekrar deneyin.",
			})
			return
		}
		_, err = db.Exec(`
			INSERT INTO user_section_answers (user_id, question_id, selected_answer, is_correct, points_awarded)
			VALUES ($1, $2, $3, $4, $5)
		`, userID, questionID, req.Answer, isCorrect, points)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Kayıt hatası",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"correct": true,
			"points":  points,
			"message": fmt.Sprintf("Doğru cevap! %d puan kazandınız.", points),
		})
	}
}

// APICompleteLesson - Dersi tamamla
func APICompleteLesson(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		lessonID, _ := strconv.Atoi(vars["id"])

		session, _ := store.Get(r, "session")
		userID, ok := session.Values["user_id"].(int)
		if !ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Giriş yapmalısınız",
			})
			return
		}

		// Ders zaten tamamlandı mı?
		var isCompleted bool
		db.QueryRow(`
			SELECT is_completed FROM academy_progress 
			WHERE user_id = $1 AND lesson_id = $2
		`, userID, lessonID).Scan(&isCompleted)

		if isCompleted {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Bu dersi zaten tamamladınız",
			})
			return
		}

		// Tüm sorular çözüldü mü?
		var totalQuestions, solvedQuestions int
		db.QueryRow(`
			SELECT COUNT(*) FROM section_questions WHERE lesson_id = $1 AND is_active = true
		`, lessonID).Scan(&totalQuestions)

		db.QueryRow(`
			SELECT COUNT(*) FROM user_section_answers u
			JOIN section_questions q ON u.question_id = q.id
			WHERE q.lesson_id = $1 AND u.user_id = $2 AND u.is_correct = true
		`, lessonID, userID).Scan(&solvedQuestions)

		// Practical soruları kontrol et
		var totalPractical, solvedPractical int
		db.QueryRow(`
			SELECT COUNT(*) FROM practical_questions WHERE lesson_id = $1 AND is_active = true
		`, lessonID).Scan(&totalPractical)

		db.QueryRow(`
			SELECT COUNT(*) FROM user_practical_solutions u
			JOIN practical_questions q ON u.question_id = q.id
			WHERE q.lesson_id = $1 AND u.user_id = $2 AND u.is_correct = true
		`, lessonID, userID).Scan(&solvedPractical)

		if solvedQuestions < totalQuestions || solvedPractical < totalPractical {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Tüm soruları çözmelisiniz",
				"remaining_section":   totalQuestions - solvedQuestions,
				"remaining_practical": totalPractical - solvedPractical,
			})
			return
		}

		// Ders puanını al
		var pointsReward int
		db.QueryRow(`SELECT points_reward FROM academy_lessons WHERE id = $1`, lessonID).Scan(&pointsReward)

		// Dersi tamamla
		_, err := db.Exec(`
			INSERT INTO academy_progress (user_id, lesson_id, is_completed, completed_at)
			VALUES ($1, $2, true, NOW())
			ON CONFLICT (user_id, lesson_id) 
			DO UPDATE SET is_completed = true, completed_at = NOW()
		`, userID, lessonID)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Kayıt hatası",
			})
			return
		}

		// Puan ekle
		// Soru puanlarını topla
		var totalQuestionPoints int
		db.QueryRow(`
    SELECT COALESCE(SUM(q.points_reward), 0)
    FROM user_section_answers ua
    JOIN section_questions q ON ua.question_id = q.id
    WHERE q.lesson_id = $1 AND ua.user_id = $2 AND ua.is_correct = true
`, lessonID, userID).Scan(&totalQuestionPoints)

		// Practical soru puanlarını topla
		var totalPracticalPoints int
		db.QueryRow(`
    SELECT COALESCE(SUM(q.points_reward), 0)
    FROM user_practical_solutions ups
    JOIN practical_questions q ON ups.question_id = q.id
    WHERE q.lesson_id = $1 AND ups.user_id = $2 AND ups.is_correct = true
`, lessonID, userID).Scan(&totalPracticalPoints)

		// Toplam puanı tek seferde ekle
		totalPoints := pointsReward + totalQuestionPoints + totalPracticalPoints
		db.Exec(`UPDATE users SET points = points + $1 WHERE id = $2`, totalPoints, userID)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Tebrikler! Dersi tamamladınız ve %d puan kazandınız!", totalPoints),
			"points":  totalPoints,
		})
	}
}

// getUserByID - Basit kullanıcı bilgisi
func getUserByID(db *sql.DB, userID int) *models.User {
	var user models.User
	var avatar sql.NullString

	err := db.QueryRow(`
		SELECT 
			id, 
			username, 
			email, 
			COALESCE(avatar, '/static/images/avatar.png') as avatar,
			COALESCE(points, 0) as points,
			COALESCE(is_vip, false) as is_vip,
			COALESCE(bio, '') as bio,           -- ✅ bio için de COALESCE
			COALESCE(two_factor_enabled, false) as two_factor_enabled
		FROM users 
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&avatar,
		&user.Points,
		&user.IsVIP,
		&user.Bio, // ✅ artık doğrudan string alabilir
		&user.TwoFactorEnabled,
	)

	if err != nil {
		log.Printf("getUserByID error: %v", err)
		return nil
	}

	user.Avatar = avatar.String
	return &user
}
