package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// AdminAcademyPage - Akademi yönetim ana sayfası
func AdminAcademyPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "admin_session")
		username := session.Values["username"].(string)
		role := session.Values["role"].(string)

		admin := struct {
			Username string
			Role     string
			Avatar   string
		}{
			Username: username,
			Role:     role,
			Avatar:   "/static/images/avatar.png",
		}

		data := struct {
			Title  string
			Active string
			Admin  interface{}
		}{
			Title:  "Akademi Yönetimi - Admin Panel",
			Active: "academy",
			Admin:  admin,
		}

		tmpl, err := template.ParseFiles(
			"templates/admin/layout.html",
			"templates/admin/academy.html",
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, data)
	}
}

// AdminGetCategories - Kategorileri listele (API)
func AdminGetCategories(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query(`
			SELECT id, name, slug, COALESCE(description, ''), COALESCE(icon, 'fa-book'), sort_order, is_active, created_at
			FROM academy_categories
			ORDER BY sort_order
		`)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var categories []map[string]interface{}
		for rows.Next() {
			var id, sortOrder int
			var name, slug, description, icon string
			var isActive bool
			var createdAt time.Time
			rows.Scan(&id, &name, &slug, &description, &icon, &sortOrder, &isActive, &createdAt)
			categories = append(categories, map[string]interface{}{
				"id": id, "name": name, "slug": slug, "description": description,
				"icon": icon, "sort_order": sortOrder, "is_active": isActive, "created_at": createdAt,
			})
		}
		json.NewEncoder(w).Encode(categories)
	}
}

// AdminCreateCategory - Kategori oluştur
func AdminCreateCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
			SortOrder   int    `json:"sort_order"`
			IsActive    bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		if req.Slug == "" {
			req.Slug = strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
		}
		if req.Icon == "" {
			req.Icon = "fa-book"
		}

		var id int
		err := db.QueryRow(`
			INSERT INTO academy_categories (name, slug, description, icon, sort_order, is_active, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			RETURNING id
		`, req.Name, req.Slug, req.Description, req.Icon, req.SortOrder, req.IsActive).Scan(&id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id, "message": "Kategori oluşturuldu"})
	}
}

// AdminUpdateCategory - Kategori güncelle
func AdminUpdateCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		var req struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
			SortOrder   int    `json:"sort_order"`
			IsActive    bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		_, err := db.Exec(`
			UPDATE academy_categories 
			SET name = $1, slug = $2, description = $3, icon = $4, sort_order = $5, is_active = $6
			WHERE id = $7
		`, req.Name, req.Slug, req.Description, req.Icon, req.SortOrder, req.IsActive, id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Kategori güncellendi"})
	}
}

// AdminDeleteCategory - Kategori sil
func AdminDeleteCategory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		_, err := db.Exec("DELETE FROM academy_categories WHERE id = $1", id)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Kategori silindi"})
	}
}

// APIAnswerPracticalQuestion -
// APIAnswerPracticalQuestion - Uygulamalı soruya flag gönderme
func APIAnswerPracticalQuestion(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
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
			Flag string `json:"flag"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Geçersiz istek",
			})
			return
		}

		// Soruyu ve flag hash'ini al
		var hashedFlag string
		var pointsReward int
		var lessonID int
		err := db.QueryRow(`
			SELECT flag_hash, points_reward, lesson_id 
			FROM practical_questions WHERE id = $1 AND is_active = true
		`, questionID).Scan(&hashedFlag, &pointsReward, &lessonID)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Soru bulunamadı",
			})
			return
		}

		// Daha önce çözülmüş mü?
		var solved bool
		db.QueryRow(`
			SELECT EXISTS(SELECT 1 FROM user_practical_solutions 
			WHERE user_id = $1 AND question_id = $2 AND is_correct = true)
		`, userID, questionID).Scan(&solved)

		if solved {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "message": "Bu soruyu zaten çözdünüz",
			})
			return
		}

		// Flag'i doğrula
		isCorrect := verifyFlag(req.Flag, hashedFlag)
		points := 0

		if isCorrect {
			points = pointsReward
			// Kullanıcıya puan ekle
			db.Exec(`UPDATE users SET points = points + $1 WHERE id = $2`, points, userID)

			// Çözümü kaydet
			db.Exec(`
				INSERT INTO user_practical_solutions (user_id, question_id, submitted_flag, is_correct, points_awarded, solved_at)
				VALUES ($1, $2, $3, true, $4, NOW())
			`, userID, questionID, req.Flag, points)

			// Activity log
			db.Exec(`
				INSERT INTO activity_logs (user_id, action_type, machine_id, ip_address, created_at)
				VALUES ($1, 'academy_practical_solved', $2, $3, NOW())
			`, userID, lessonID, r.RemoteAddr)

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"correct": true,
				"points":  points,
				"message": fmt.Sprintf("Tebrikler! Doğru flag! %d puan kazandınız.", points),
			})
		} else {
			// Yanlış flag, sadece kaydet (puan verme)
			db.Exec(`
				INSERT INTO user_practical_solutions (user_id, question_id, submitted_flag, is_correct, points_awarded, solved_at)
				VALUES ($1, $2, $3, false, 0, NOW())
			`, userID, questionID, req.Flag)

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"correct": false,
				"message": "Yanlış flag! Tekrar deneyin.",
			})
		}
	}
}

// AdminGetLessons - Dersleri listele (API)
func AdminGetLessons(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query(`
			SELECT l.id, l.title, l.slug, l.category_id, c.name, l.difficulty, l.duration_minutes, 
				   l.points_reward, l.sort_order, l.is_published, l.created_at
			FROM academy_lessons l
			JOIN academy_categories c ON l.category_id = c.id
			ORDER BY l.created_at DESC
		`)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var lessons []map[string]interface{}
		for rows.Next() {
			var id, categoryID, duration, points, sortOrder int
			var title, slug, difficulty, categoryName string
			var isPublished bool
			var createdAt time.Time
			rows.Scan(&id, &title, &slug, &categoryID, &categoryName, &difficulty, &duration, &points, &sortOrder, &isPublished, &createdAt)
			lessons = append(lessons, map[string]interface{}{
				"id": id, "title": title, "slug": slug, "category_id": categoryID, "category_name": categoryName,
				"difficulty": difficulty, "duration_minutes": duration, "points_reward": points,
				"sort_order": sortOrder, "is_published": isPublished, "created_at": createdAt,
			})
		}
		json.NewEncoder(w).Encode(lessons)
	}
}

// AdminGetLesson - Tek bir ders getir
func AdminGetLesson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		var lesson struct {
			ID           int
			Title        string
			Slug         string
			CategoryID   int
			Content      string
			Difficulty   string
			Duration     int
			PointsReward int
			VideoURL     string
			SortOrder    int
			IsPublished  bool
		}

		err := db.QueryRow(`
			SELECT id, title, slug, category_id, COALESCE(content, ''), COALESCE(difficulty, 'beginner'),
				   COALESCE(duration_minutes, 0), COALESCE(points_reward, 50), COALESCE(video_url, ''),
				   COALESCE(sort_order, 0), is_published
			FROM academy_lessons WHERE id = $1
		`, id).Scan(&lesson.ID, &lesson.Title, &lesson.Slug, &lesson.CategoryID, &lesson.Content,
			&lesson.Difficulty, &lesson.Duration, &lesson.PointsReward, &lesson.VideoURL,
			&lesson.SortOrder, &lesson.IsPublished)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Ders bulunamadı"})
			return
		}

		json.NewEncoder(w).Encode(lesson)
	}
}

// AdminCreateLesson - Ders oluştur
func AdminCreateLesson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			Title        string `json:"title"`
			Slug         string `json:"slug"`
			CategoryID   int    `json:"category_id"`
			Content      string `json:"content"`
			Difficulty   string `json:"difficulty"`
			Duration     int    `json:"duration_minutes"`
			PointsReward int    `json:"points_reward"`
			VideoURL     string `json:"video_url"`
			SortOrder    int    `json:"sort_order"`
			IsPublished  bool   `json:"is_published"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		if req.Slug == "" {
			req.Slug = strings.ToLower(strings.ReplaceAll(req.Title, " ", "-"))
		}

		var id int
		err := db.QueryRow(`
			INSERT INTO academy_lessons (title, slug, category_id, content, difficulty, duration_minutes, 
				points_reward, video_url, sort_order, is_published, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
			RETURNING id
		`, req.Title, req.Slug, req.CategoryID, req.Content, req.Difficulty, req.Duration,
			req.PointsReward, req.VideoURL, req.SortOrder, req.IsPublished).Scan(&id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id, "message": "Ders oluşturuldu"})
	}
}

// AdminUpdateLesson - Ders güncelle
func AdminUpdateLesson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		var req struct {
			Title        string `json:"title"`
			Slug         string `json:"slug"`
			CategoryID   int    `json:"category_id"`
			Content      string `json:"content"`
			Difficulty   string `json:"difficulty"`
			Duration     int    `json:"duration_minutes"`
			PointsReward int    `json:"points_reward"`
			VideoURL     string `json:"video_url"`
			SortOrder    int    `json:"sort_order"`
			IsPublished  bool   `json:"is_published"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		_, err := db.Exec(`
			UPDATE academy_lessons 
			SET title = $1, slug = $2, category_id = $3, content = $4, difficulty = $5, 
				duration_minutes = $6, points_reward = $7, video_url = $8, sort_order = $9, 
				is_published = $10, updated_at = NOW()
			WHERE id = $11
		`, req.Title, req.Slug, req.CategoryID, req.Content, req.Difficulty, req.Duration,
			req.PointsReward, req.VideoURL, req.SortOrder, req.IsPublished, id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Ders güncellendi"})
	}
}

// AdminDeleteLesson - Ders sil
func AdminDeleteLesson(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		_, err := db.Exec("DELETE FROM academy_lessons WHERE id = $1", id)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Ders silindi"})
	}
}

// AdminGetQuestions - Soruları listele
func AdminGetQuestions(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		lessonID, _ := strconv.Atoi(vars["id"])

		rows, err := db.Query(`
			SELECT id, question_text, option_a, option_b, COALESCE(option_c, ''), COALESCE(option_d, ''),
				   correct_answer, COALESCE(explanation, ''), points_reward, sort_order, is_active
			FROM section_questions
			WHERE lesson_id = $1
			ORDER BY sort_order
		`, lessonID)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var questions []map[string]interface{}
		for rows.Next() {
			var id, points, sortOrder int
			var questionText, optionA, optionB, optionC, optionD, correctAnswer, explanation string
			var isActive bool
			rows.Scan(&id, &questionText, &optionA, &optionB, &optionC, &optionD, &correctAnswer, &explanation, &points, &sortOrder, &isActive)
			questions = append(questions, map[string]interface{}{
				"id": id, "question_text": questionText, "option_a": optionA, "option_b": optionB,
				"option_c": optionC, "option_d": optionD, "correct_answer": correctAnswer,
				"explanation": explanation, "points_reward": points, "sort_order": sortOrder, "is_active": isActive,
			})
		}
		json.NewEncoder(w).Encode(questions)
	}
}

// AdminCreateQuestion - Soru oluştur
func AdminCreateQuestionAcademy(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			LessonID      int    `json:"lesson_id"`
			QuestionText  string `json:"question_text"`
			OptionA       string `json:"option_a"`
			OptionB       string `json:"option_b"`
			OptionC       string `json:"option_c"`
			OptionD       string `json:"option_d"`
			CorrectAnswer string `json:"correct_answer"`
			Explanation   string `json:"explanation"`
			PointsReward  int    `json:"points_reward"`
			SortOrder     int    `json:"sort_order"`
			IsActive      bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		var id int
		err := db.QueryRow(`
			INSERT INTO section_questions (lesson_id, question_text, option_a, option_b, option_c, option_d,
				correct_answer, explanation, points_reward, sort_order, is_active, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
			RETURNING id
		`, req.LessonID, req.QuestionText, req.OptionA, req.OptionB, req.OptionC, req.OptionD,
			req.CorrectAnswer, req.Explanation, req.PointsReward, req.SortOrder, req.IsActive).Scan(&id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id, "message": "Soru oluşturuldu"})
	}
}

// AdminUpdateQuestion - Soru güncelle
func AdminUpdateQuestionAcademy(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		var req struct {
			QuestionText  string `json:"question_text"`
			OptionA       string `json:"option_a"`
			OptionB       string `json:"option_b"`
			OptionC       string `json:"option_c"`
			OptionD       string `json:"option_d"`
			CorrectAnswer string `json:"correct_answer"`
			Explanation   string `json:"explanation"`
			PointsReward  int    `json:"points_reward"`
			SortOrder     int    `json:"sort_order"`
			IsActive      bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		_, err := db.Exec(`
			UPDATE section_questions 
			SET question_text = $1, option_a = $2, option_b = $3, option_c = $4, option_d = $5,
				correct_answer = $6, explanation = $7, points_reward = $8, sort_order = $9, is_active = $10
			WHERE id = $11
		`, req.QuestionText, req.OptionA, req.OptionB, req.OptionC, req.OptionD,
			req.CorrectAnswer, req.Explanation, req.PointsReward, req.SortOrder, req.IsActive, id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Soru güncellendi"})
	}
}

// AdminDeleteQuestion - Soru sil
func AdminDeleteQuestionAcademy(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		_, err := db.Exec("DELETE FROM section_questions WHERE id = $1", id)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Soru silindi"})
	}
}

// AdminGetPracticalQuestions - Uygulamalı soruları listele
func AdminGetPracticalQuestions(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		lessonID, _ := strconv.Atoi(vars["id"])

		rows, err := db.Query(`
			SELECT id, title, description, docker_image, flag_hash, points_reward, difficulty, hint, hint_cost, is_active
			FROM practical_questions
			WHERE lesson_id = $1
			ORDER BY id
		`, lessonID)
		if err != nil {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		defer rows.Close()

		var questions []map[string]interface{}
		for rows.Next() {
			var id, points, hintCost int
			var title, description, dockerImage, flagHash, difficulty, hint string
			var isActive bool
			rows.Scan(&id, &title, &description, &dockerImage, &flagHash, &points, &difficulty, &hint, &hintCost, &isActive)
			questions = append(questions, map[string]interface{}{
				"id": id, "title": title, "description": description, "docker_image": dockerImage,
				"flag_hash": flagHash, "points_reward": points, "difficulty": difficulty,
				"hint": hint, "hint_cost": hintCost, "is_active": isActive,
			})
		}
		json.NewEncoder(w).Encode(questions)
	}
}

// AdminCreatePracticalQuestion - Uygulamalı soru oluştur
func AdminCreatePracticalQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			LessonID     int    `json:"lesson_id"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			DockerImage  string `json:"docker_image"`
			Flag         string `json:"flag"`
			PointsReward int    `json:"points_reward"`
			Difficulty   string `json:"difficulty"`
			Hint         string `json:"hint"`
			HintCost     int    `json:"hint_cost"`
			IsActive     bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		// Flag'i hashle
		hashedFlag, _ := hashFlag(req.Flag)

		var id int
		err := db.QueryRow(`
			INSERT INTO practical_questions (lesson_id, title, description, docker_image, flag_hash, points_reward, difficulty, hint, hint_cost, is_active, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
			RETURNING id
		`, req.LessonID, req.Title, req.Description, req.DockerImage, hashedFlag,
			req.PointsReward, req.Difficulty, req.Hint, req.HintCost, req.IsActive).Scan(&id)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id, "message": "Soru oluşturuldu"})
	}
}

// hashFlag - Flag'i hash'ler (bcrypt ile)
func hashFlag(flag string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(flag), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// verifyFlag - Flag'i doğrular
func verifyFlag(flag, hashedFlag string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedFlag), []byte(flag))
	return err == nil
}

// AdminAcademyQuestionsPage - Soru yönetim sayfası
func AdminAcademyQuestionsPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		lessonID, _ := strconv.Atoi(vars["id"])

		var lessonTitle string
		db.QueryRow("SELECT title FROM academy_lessons WHERE id = $1", lessonID).Scan(&lessonTitle)

		data := struct {
			Title       string
			Active      string
			Admin       interface{}
			LessonID    int
			LessonTitle string
		}{
			Title:       "Soru Yönetimi - Admin Panel",
			Active:      "academy",
			LessonID:    lessonID,
			LessonTitle: lessonTitle,
		}

		tmpl := template.Must(template.ParseFiles("templates/admin/layout.html", "templates/admin/academy_questions.html"))
		tmpl.Execute(w, data)
	}
}

// AdminUpdatePracticalQuestion - Pratik soru güncelle
func AdminUpdatePracticalQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		var req struct {
			Title        string `json:"title"`
			Description  string `json:"description"`
			DockerImage  string `json:"docker_image"`
			Flag         string `json:"flag"`
			PointsReward int    `json:"points_reward"`
			Difficulty   string `json:"difficulty"`
			Hint         string `json:"hint"`
			HintCost     int    `json:"hint_cost"`
			IsActive     bool   `json:"is_active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Geçersiz istek"})
			return
		}

		var err error
		if req.Flag != "" {
			hashedFlag, _ := hashFlag(req.Flag)
			_, err = db.Exec(`
                UPDATE practical_questions 
                SET title = $1, description = $2, docker_image = $3, flag_hash = $4,
                    points_reward = $5, difficulty = $6, hint = $7, hint_cost = $8, is_active = $9
                WHERE id = $10
            `, req.Title, req.Description, req.DockerImage, hashedFlag,
				req.PointsReward, req.Difficulty, req.Hint, req.HintCost, req.IsActive, id)
		} else {
			_, err = db.Exec(`
                UPDATE practical_questions 
                SET title = $1, description = $2, docker_image = $3,
                    points_reward = $4, difficulty = $5, hint = $6, hint_cost = $7, is_active = $8
                WHERE id = $9
            `, req.Title, req.Description, req.DockerImage,
				req.PointsReward, req.Difficulty, req.Hint, req.HintCost, req.IsActive, id)
		}

		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Soru güncellendi"})
	}
}

// AdminDeletePracticalQuestion - Pratik soru sil
func AdminDeletePracticalQuestion(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vars := mux.Vars(r)
		id, _ := strconv.Atoi(vars["id"])

		_, err := db.Exec("DELETE FROM practical_questions WHERE id = $1", id)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Soru silindi"})
	}
}
