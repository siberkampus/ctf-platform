package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ctf-platform/models"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ─── Veri yapıları ───────────────────────────────────────────────────────────

type MachineDetailData struct {
	Title           string
	User            *models.User
	IsAuthenticated bool
	Machine         models.Machine
	Questions       []models.Question
	RecentSolvers   []models.Solver
	UserProgress    models.UserProgress
	IsVIP           bool
	TimeRemaining   int
	ActiveSession   *models.MachineSession
	AccessURL       string
	AccessType      string
	IsAdmin         bool
}

type FlagSubmitRequest struct {
	QuestionID int    `json:"question_id"`
	Flag       string `json:"flag"`
}

type FlagSubmitResponse struct {
	Correct     bool   `json:"correct"`
	Message     string `json:"message"`
	Points      int    `json:"points,omitempty"`
	TotalPoints int    `json:"total_points,omitempty"`
	IsFirst     bool   `json:"is_first,omitempty"`
}

// ─── Docker yardımcıları ─────────────────────────────────────────────────────

func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

func pullImageIfMissing(ctx context.Context, cli *client.Client, imageName string) error {
	_, _, err := cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		log.Printf("✅ İmaj zaten var: %s", imageName)
		return nil
	}

	log.Printf("📥 İmaj çekiliyor: %s", imageName)
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	log.Printf("✅ İmaj çekildi: %s", imageName)
	return nil
}

func getAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func startDockerContainer(dockerImage, containerName string) (string, int, error) {
	ctx := context.Background()
	cli, err := newDockerClient()
	if err != nil {
		return "", 0, err
	}
	defer cli.Close()

	if err := pullImageIfMissing(ctx, cli, dockerImage); err != nil {
		return "", 0, err
	}

	hostPort, err := getAvailablePort()
	if err != nil {
		return "", 0, err
	}

	exposedPorts := nat.PortSet{
		"80/tcp":   struct{}{},
		"22/tcp":   struct{}{},
		"8080/tcp": struct{}{},
	}

	portBindings := nat.PortMap{
		"80/tcp":   []nat.PortBinding{{HostPort: strconv.Itoa(hostPort)}},
		"22/tcp":   []nat.PortBinding{{HostPort: strconv.Itoa(hostPort + 1)}},
		"8080/tcp": []nat.PortBinding{{HostPort: strconv.Itoa(hostPort + 2)}},
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:        dockerImage,
			ExposedPorts: exposedPorts,
			Tty:          true,
		},
		&container.HostConfig{
			Resources: container.Resources{
				Memory:   512 * 1024 * 1024,
				NanoCPUs: 500_000_000,
			},
			PortBindings: portBindings,
			NetworkMode:  "bridge",
		},
		nil, nil, containerName,
	)
	if err != nil {
		return "", 0, err
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", 0, err
	}

	log.Printf("🚀 Container başlatıldı: ID=%s, Port=%d", resp.ID, hostPort)
	return resp.ID, hostPort, nil
}

func stopDockerContainer(containerID string) error {
	ctx := context.Background()
	cli, err := newDockerClient()
	if err != nil {
		return err
	}
	defer cli.Close()

	timeout := 10
	if err := cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		log.Printf("Container durdurma uyarısı (%s): %v", containerID, err)
	}

	return cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

func getContainerAccessType(dockerImage string) string {
	webImages := []string{"sqli-labs", "dvwa", "wordpress", "nginx", "apache", "httpd", "php", "web", "http"}
	lowerImage := strings.ToLower(dockerImage)

	for _, webImg := range webImages {
		if strings.Contains(lowerImage, webImg) {
			return "web"
		}
	}
	return "terminal"
}

// ─── HTML Sayfa Handler'ı ─────────────────────────────────────────────────────

func MachineDetailPage(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, _ := strconv.Atoi(machineIDStr)

		session, _ := store.Get(r, "session")
		isAuth := false
		var user *models.User
		var userProgress models.UserProgress
		var activeSession *models.MachineSession
		var accessURL string
		var accessType string

		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
			isAuth = true
			userID, ok := session.Values["user_id"].(int)
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user = &models.User{}
			var avatar sql.NullString
			if err := db.QueryRow(`
				SELECT id, username, email,
				       COALESCE(avatar, '/static/images/avatar.png'),
				       is_vip, points, rank
				FROM users WHERE id = $1
			`, userID).Scan(
				&user.ID, &user.Username, &user.Email,
				&avatar, &user.IsVIP, &user.Points, &user.Rank,
			); err != nil {
				log.Printf("Kullanıcı sorgu hatası: %v", err)
			} else {
				user.Avatar = avatar.String
			}

			_ = db.QueryRow(`
				SELECT COUNT(DISTINCT question_id)
				FROM user_solutions
				WHERE user_id = $1 AND machine_id = $2
			`, userID, machineID).Scan(&userProgress.SolvedQuestions)

			_ = db.QueryRow(`
				SELECT COUNT(*) FROM machine_questions
				WHERE machine_id = $1 AND is_active = true
			`, machineID).Scan(&userProgress.TotalQuestions)

			var (
				sessionID   int
				containerID string
				startedAt   time.Time
				expiresAt   time.Time
				endedAt     sql.NullTime
				hostPort    sql.NullInt64
			)

			// UTC kullanarak sorgula
			err := db.QueryRow(`
				SELECT id, container_id, started_at, expires_at, ended_at, host_port
				FROM machine_sessions
				WHERE user_id = $1 AND machine_id = $2 AND ended_at IS NULL
				ORDER BY started_at DESC LIMIT 1
			`, userID, machineID).Scan(
				&sessionID, &containerID, &startedAt, &expiresAt, &endedAt, &hostPort,
			)

			// Şimdiki zamanı UTC olarak al
			now := time.Now().UTC()

			if err == nil && now.Before(expiresAt.UTC()) {
				activeSession = &models.MachineSession{
					ID:          sessionID,
					UserID:      userID,
					MachineID:   machineIDStr,
					ContainerID: containerID,
					StartedAt:   startedAt.UTC(),
					ExpiresAt:   expiresAt.UTC(),
					HostPort:    int(hostPort.Int64),
				}

				if hostPort.Valid && hostPort.Int64 > 0 {
					accessURL = fmt.Sprintf("http://localhost:%d", hostPort.Int64)
				}
			}
		}

		machine, err := fetchMachine(db, machineID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Makine bulunamadı", http.StatusNotFound)
			} else {
				log.Printf("Makine sorgu hatası: %v", err)
				http.Error(w, "Veritabanı hatası", http.StatusInternalServerError)
			}
			return
		}

		questions, err := fetchQuestions(db, machineID, user)
		if err != nil {
			log.Printf("Soru sorgu hatası: %v", err)
			http.Error(w, "Sorular getirilemedi", http.StatusInternalServerError)
			return
		}

		recentSolvers, err := getRecentSolvers(db, machineID, 10)
		if err != nil {
			log.Printf("Son çözenler sorgu hatası: %v", err)
			recentSolvers = []models.Solver{}
		}

		// Zaman hesaplaması - UTC kullan
		timeRemaining := 3600
		if activeSession != nil {
			now := time.Now().UTC()
			remainingSeconds := int(activeSession.ExpiresAt.Sub(now).Seconds())
			if remainingSeconds > 0 {
				timeRemaining = remainingSeconds
			} else {
				timeRemaining = 0
			}
		}

		if activeSession != nil && machine.DockerImage != "" {
			accessType = getContainerAccessType(machine.DockerImage)
		}
		isAdmin := false
		if user != nil {
			var count int
			err := db.QueryRow("SELECT COUNT(*) FROM admins WHERE id = $1", user.ID).Scan(&count)
			if err == nil && count > 0 {
				isAdmin = true
			}
		}
		data := MachineDetailData{
			Title:           machine.Name + " - CTF HACK PLATFORMU",
			User:            user,
			IsAuthenticated: isAuth,
			Machine:         machine,
			Questions:       questions,
			RecentSolvers:   recentSolvers,
			UserProgress:    userProgress,
			IsVIP:           user != nil && user.IsVIP,
			TimeRemaining:   timeRemaining,
			ActiveSession:   activeSession,
			AccessURL:       accessURL,
			AccessType:      accessType,
			IsAdmin:         isAdmin,
		}

		funcMap := template.FuncMap{
			"not": func(b bool) bool { return !b },
			"formatDate": func(t time.Time) string {
				return t.Format("02.01.2006")
			},
			"percentage": func(solved, total int) int {
				if total == 0 {
					return 0
				}
				return solved * 100 / total
			},
			"difficultyClass": func(d string) string {
				m := map[string]string{
					"easy": "difficulty-easy", "medium": "difficulty-medium",
					"hard": "difficulty-hard", "expert": "difficulty-expert",
				}
				if v, ok := m[d]; ok {
					return v
				}
				return "difficulty-easy"
			},
			"difficultyLabel": func(d string) string {
				m := map[string]string{
					"easy": "Kolay", "medium": "Orta",
					"hard": "Zor", "expert": "Uzman",
				}
				if v, ok := m[d]; ok {
					return v
				}
				return d
			},
		}

		tmpl, err := template.New("machine_detail.html").Funcs(funcMap).ParseFiles(
			"templates/machine_detail.html",
			"templates/partials/question_item.html",
			"templates/partials/solver_item.html",
		)
		if err != nil {
			log.Printf("Template yükleme hatası: %v", err)
			http.Error(w, "Sayfa yüklenemedi", http.StatusInternalServerError)
			return
		}

		if err = tmpl.Execute(w, data); err != nil {
			log.Printf("Template çalıştırma hatası: %v", err)
		}
	}
}

// ─── API: Makine Başlat ───────────────────────────────────────────────────────

func StartMachine(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, _ := strconv.Atoi(machineIDStr)

		session, _ := store.Get(r, "session")
		userID, ok := sessionUserID(session)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var (
			machineName string
			isVIPOnly   bool
			dockerImage string
		)
		err := db.QueryRow(`
			SELECT name, is_vip_only, docker_image
			FROM machines WHERE id = $1 AND is_active = true
		`, machineID).Scan(&machineName, &isVIPOnly, &dockerImage)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Makine bulunamadı")
			} else {
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			}
			return
		}

		if dockerImage == "" {
			writeError(w, http.StatusBadRequest, "Bu makine için Docker imajı tanımlanmamış")
			return
		}

		if isVIPOnly {
			var isVIP bool
			_ = db.QueryRow("SELECT is_vip FROM users WHERE id = $1", userID).Scan(&isVIP)
			if !isVIP {
				writeError(w, http.StatusForbidden, "Bu makine VIP üyelik gerektiriyor")
				return
			}
		}

		var activeCount int
		_ = db.QueryRow(`
			SELECT COUNT(*) FROM machine_sessions
			WHERE user_id = $1 AND machine_id = $2 AND ended_at IS NULL
		`, userID, machineID).Scan(&activeCount)
		if activeCount > 0 {
			writeError(w, http.StatusBadRequest, "Zaten aktif bir oturumunuz var")
			return
		}

		containerName := fmt.Sprintf("ctf_%d_%d_%d", machineID, userID, time.Now().UnixNano())

		containerID, hostPort, err := startDockerContainer(dockerImage, containerName)
		if err != nil {
			log.Printf("Docker başlatma hatası: %v", err)
			writeError(w, http.StatusInternalServerError, "Container başlatılamadı: "+err.Error())
			return
		}

		// UTC kullan - 1 saat
		now := time.Now().UTC()
		expiresAt := now.Add(1 * time.Hour)
		startedAt := now

		var sessionID int
		err = db.QueryRow(`
			INSERT INTO machine_sessions (user_id, machine_id, container_id, started_at, expires_at, host_port)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
		`, userID, machineID, containerID, startedAt, expiresAt, hostPort).Scan(&sessionID)
		if err != nil {
			log.Printf("Session oluşturma hatası: %v", err)
			_ = stopDockerContainer(containerID)
			writeError(w, http.StatusInternalServerError, "Oturum kaydedilemedi")
			return
		}

		_, _ = db.Exec(`
			INSERT INTO activity_logs (user_id, action_type, machine_id, ip_address, created_at)
			VALUES ($1, 'machine_start', $2, $3, $4)
		`, userID, machineID, r.RemoteAddr, time.Now().UTC())

		accessURL := fmt.Sprintf("http://localhost:%d", hostPort)

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"session_id":     sessionID,
			"container_id":   containerID,
			"message":        "Makine başlatıldı",
			"expires_at":     expiresAt,
			"time_remaining": 3600,
			"access_url":     accessURL,
			"access_type":    getContainerAccessType(dockerImage),
		})
	}
}

// ─── API: Makine Durdur ───────────────────────────────────────────────────────

func StopMachine(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, err := strconv.Atoi(machineIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz makine ID")
			return
		}

		session, _ := store.Get(r, "session")
		userID, ok := sessionUserID(session)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		// Oturum bilgilerini tam olarak al
		var sessionID int
		var containerID string
		var hostPort int

		err = db.QueryRow(`
			SELECT id, container_id, host_port 
			FROM machine_sessions
			WHERE user_id = $1 AND machine_id = $2 AND ended_at IS NULL
			ORDER BY started_at DESC LIMIT 1
		`, userID, machineID).Scan(&sessionID, &containerID, &hostPort)

		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Aktif oturum bulunamadı")
			} else {
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			}
			return
		}

		// Container'ı durdur, hata olsa bile DB'yi güncelle
		var dockerErr error
		if containerID != "" {
			dockerErr = stopDockerContainer(containerID)
			if dockerErr != nil {
				log.Printf("Docker durdurma hatası (SessionID: %d, Container: %s): %v",
					sessionID, containerID, dockerErr)
				// Container zaten yok olabilir, devam et
			}
		}

		// Transaction kullanarak DB güncellemesi yap
		tx, err := db.Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Transaction başlatılamadı")
			return
		}
		defer tx.Rollback()

		// Oturumu sonlandır
		result, err := tx.Exec(`
			UPDATE machine_sessions 
			SET ended_at = NOW() 
			WHERE id = $1 AND ended_at IS NULL
		`, sessionID)

		if err != nil {
			writeError(w, http.StatusInternalServerError, "Oturum sonlandırılamadı")
			return
		}

		rows, _ := result.RowsAffected()
		if rows == 0 {
			writeError(w, http.StatusNotFound, "Aktif oturum bulunamadı")
			return
		}

		// Activity log ekle
		_, err = tx.Exec(`
			INSERT INTO activity_logs (user_id, action_type, machine_id, ip_address, created_at)
			VALUES ($1, 'machine_stop', $2, $3, NOW())
		`, userID, machineID, r.RemoteAddr)

		if err != nil {
			log.Printf("Activity log eklenemedi: %v", err)
			// Kritik değil, devam et
		}

		if err = tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "Değişiklikler kaydedilemedi")
			return
		}

		// Başarılı yanıt
		responseMsg := "Makine durduruldu"
		if dockerErr != nil {
			responseMsg = "Makine durduruldu (Docker hatası: " + dockerErr.Error() + ")"
		}

		// Host port'u temizlemek için goroutine (opsiyonel)
		go func(port int) {
			if port > 0 {
				// Port'u temizleme işlemi (opsiyonel)
				log.Printf("Port %d temizlendi", port)
			}
		}(hostPort)

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"message":    responseMsg,
			"session_id": sessionID,
		})
	}
}

// ─── WebSocket Terminal (Gerçek Docker exec) ─────────────────────────────────

func TerminalWebSocket(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, _ := strconv.Atoi(machineIDStr)

		session, _ := store.Get(r, "session")
		userID, ok := sessionUserID(session)
		if !ok {
			log.Printf("❌ Terminal: Yetkisiz erişim - userID alınamadı")
			http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
			return
		}

		log.Printf("🔍 Terminal bağlantı isteği: user=%d, machine=%d", userID, machineID)

		// Aktif session kontrolü
		var containerID string
		var expiresAt time.Time
		err := db.QueryRow(`
			SELECT container_id, expires_at FROM machine_sessions
			WHERE user_id=$1 AND machine_id=$2 AND ended_at IS NULL
			ORDER BY started_at DESC LIMIT 1
		`, userID, machineID).Scan(&containerID, &expiresAt)

		if err != nil {
			log.Printf("❌ Terminal: Session bulunamadı - user=%d, machine=%d, hata=%v", userID, machineID, err)
			http.Error(w, "Aktif oturum bulunamadı. Önce makineyi başlatın.", http.StatusForbidden)
			return
		}

		if time.Now().After(expiresAt) {
			log.Printf("❌ Terminal: Session süresi dolmuş - user=%d, machine=%d", userID, machineID)
			http.Error(w, "Oturum süresi doldu. Makineyi yeniden başlatın.", http.StatusForbidden)
			return
		}

		log.Printf("✅ Terminal: Session bulundu - container=%s", containerID)

		// WebSocket bağlantısını yükselt
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("❌ Terminal: WebSocket yükseltme hatası: %v", err)
			return
		}
		defer conn.Close()

		// Docker client oluştur
		cli, err := newDockerClient()
		if err != nil {
			log.Printf("❌ Terminal: Docker client hatası: %v", err)
			conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Docker bağlantı hatası: %v\r\n", err)))
			return
		}
		defer cli.Close()

		// Container'ın çalışıp çalışmadığını kontrol et
		inspect, err := cli.ContainerInspect(context.Background(), containerID)
		if err != nil {
			log.Printf("❌ Terminal: Container inspect hatası: %v", err)
			conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Container bulunamadı: %v\r\n", err)))
			return
		}

		if !inspect.State.Running {
			log.Printf("❌ Terminal: Container çalışmıyor: %s", containerID)
			conn.WriteMessage(websocket.TextMessage, []byte("Container çalışmıyor. Makineyi yeniden başlatın.\r\n"))
			return
		}

		log.Printf("✅ Terminal: Container çalışıyor: %s", containerID)

		// Container'da çalışacak komutu belirle (önce bash, yoksa sh)
		shellCmd := []string{"/bin/bash"}
		execCfg := container.ExecOptions{
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
			Cmd:          shellCmd,
			Env:          []string{"TERM=xterm-256color"},
		}

		execID, err := cli.ContainerExecCreate(context.Background(), containerID, execCfg)
		if err != nil {
			log.Printf("⚠️ Bash bulunamadı, sh deneniyor...")
			shellCmd = []string{"/bin/sh"}
			execCfg.Cmd = shellCmd
			execID, err = cli.ContainerExecCreate(context.Background(), containerID, execCfg)
			if err != nil {
				log.Printf("❌ Terminal: Exec oluşturma hatası: %v", err)
				conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Terminal başlatılamadı: %v\r\n", err)))
				return
			}
		}

		// Exec attach
		hijack, err := cli.ContainerExecAttach(context.Background(), execID.ID, container.ExecAttachOptions{Tty: true})
		if err != nil {
			log.Printf("❌ Terminal: Exec attach hatası: %v", err)
			conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Terminal bağlantısı kurulamadı: %v\r\n", err)))
			return
		}
		defer hijack.Close()

		log.Printf("✅✅✅ Terminal bağlantısı BAŞARILI! user=%d, machine=%d, container=%s, shell=%v", userID, machineID, containerID, shellCmd)

		// Container'dan WebSocket'e (çıktı)
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := hijack.Reader.Read(buf)
				if n > 0 {
					if err2 := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err2 != nil {
						log.Printf("WebSocket yazma hatası: %v", err2)
						break
					}
				}
				if err != nil {
					log.Printf("Hijack okuma hatası: %v", err)
					break
				}
			}
			conn.Close()
		}()

		// WebSocket'ten Container'a (girdi)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket okuma hatası: %v", err)
				break
			}
			if _, err := hijack.Conn.Write(msg); err != nil {
				log.Printf("Hijack yazma hatası: %v", err)
				break
			}
		}
	}
}

// ─── API: Flag Gönder (Kısaltılmış) ──────────────────────────────────────────

func SubmitFlag(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, _ := strconv.Atoi(machineIDStr)

		session, _ := store.Get(r, "session")
		userID, ok := sessionUserID(session)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var req FlagSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz istek formatı")
			return
		}

		var correctHash string
		var points int
		err := db.QueryRow(`
			SELECT flag_hash, points_reward FROM machine_questions
			WHERE id = $1 AND machine_id = $2 AND is_active = true
		`, req.QuestionID, machineID).Scan(&correctHash, &points)
		if err != nil {
			writeError(w, http.StatusNotFound, "Soru bulunamadı")
			return
		}

		var alreadySolved bool
		_ = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_solutions WHERE user_id=$1 AND question_id=$2)`, userID, req.QuestionID).Scan(&alreadySolved)
		if alreadySolved {
			writeSuccess(w, http.StatusOK, FlagSubmitResponse{Correct: false, Message: "Bu soruyu zaten çözdünüz!"})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(correctHash), []byte(req.Flag)); err != nil {
			_, _ = db.Exec(`INSERT INTO submissions (user_id, machine_id, question_id, submitted_flag, status, ip_address, user_agent, created_at) VALUES ($1,$2,$3,$4,'rejected',$5,$6,NOW())`, userID, machineID, req.QuestionID, req.Flag, r.RemoteAddr, r.UserAgent())
			writeSuccess(w, http.StatusOK, FlagSubmitResponse{Correct: false, Message: "Yanlış flag! Tekrar dene."})
			return
		}

		tx, _ := db.Begin()
		tx.Exec(`INSERT INTO user_solutions (user_id, machine_id, question_id, solved_at) VALUES ($1,$2,$3,NOW())`, userID, machineID, req.QuestionID)

		var isVIP bool
		tx.QueryRow("SELECT is_vip FROM users WHERE id=$1", userID).Scan(&isVIP)
		finalPoints := points
		if isVIP {
			finalPoints = points * 2
		}

		tx.Exec("UPDATE users SET points = points + $1 WHERE id = $2", finalPoints, userID)
		tx.Exec(`INSERT INTO submissions (user_id, machine_id, question_id, submitted_flag, status, points_awarded, ip_address, user_agent, created_at) VALUES ($1,$2,$3,$4,'accepted',$5,$6,$7,NOW())`, userID, machineID, req.QuestionID, req.Flag, finalPoints, r.RemoteAddr, r.UserAgent())
		tx.Exec(`INSERT INTO activity_logs (user_id, action_type, machine_id, question_id, ip_address, created_at) VALUES ($1,'flag_submit',$2,$3,$4,NOW())`, userID, machineID, req.QuestionID, r.RemoteAddr)

		tx.Commit()

		var totalPoints int
		_ = db.QueryRow("SELECT points FROM users WHERE id=$1", userID).Scan(&totalPoints)

		writeSuccess(w, http.StatusOK, FlagSubmitResponse{Correct: true, Message: "Tebrikler! Doğru flag!", Points: finalPoints, TotalPoints: totalPoints})
	}
}

// ─── API: İpucu (Kısaltılmış) ────────────────────────────────────────────────

func GetHint(db *sql.DB, store *sessions.CookieStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		machineID, _ := strconv.Atoi(vars["id"])
		questionID, _ := strconv.Atoi(vars["questionId"])

		session, _ := store.Get(r, "session")
		userID, ok := sessionUserID(session)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Yetkisiz erişim")
			return
		}

		var questionTitle string
		var hint sql.NullString
		var hintCost int
		db.QueryRow(`SELECT hint, hint_cost, title FROM machine_questions WHERE id=$1 AND machine_id=$2`, questionID, machineID).Scan(&hint, &hintCost, &questionTitle)

		hintText := ""
		if hint.Valid {
			hintText = hint.String
		}

		var hintUsed bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM hint_usage WHERE user_id=$1 AND question_id=$2)`, userID, questionID).Scan(&hintUsed)

		var userPoints int
		db.QueryRow("SELECT points FROM users WHERE id=$1", userID).Scan(&userPoints)

		if hintUsed {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "hint": hintText, "question_title": questionTitle, "message": "Daha önce satın alındı", "total_points": userPoints})
			return
		}

		if userPoints < hintCost {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": fmt.Sprintf("Yetersiz puan! Gereken: %d puan", hintCost)})
			return
		}

		tx, _ := db.Begin()
		tx.Exec("UPDATE users SET points = points - $1 WHERE id = $2", hintCost, userID)
		tx.Exec(`INSERT INTO hint_usage (user_id, machine_id, question_id, used_at) VALUES ($1,$2,$3,NOW())`, userID, machineID, questionID)
		tx.Commit()

		db.QueryRow("SELECT points FROM users WHERE id=$1", userID).Scan(&userPoints)

		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "hint": hintText, "question_title": questionTitle, "cost": hintCost, "total_points": userPoints, "message": "İpucu satın alındı!"})
	}
}

// ─── Yardımcı fonksiyonlar ───────────────────────────────────────────────────

func fetchMachine(db *sql.DB, machineID int) (models.Machine, error) {
	var m models.Machine
	var dockerImage sql.NullString
	err := db.QueryRow(`SELECT id, name, description, difficulty, points_reward, is_vip_only, docker_image, created_at, is_active FROM machines WHERE id=$1`, machineID).Scan(&m.ID, &m.Name, &m.Description, &m.Difficulty, &m.PointsReward, &m.IsVIPOnly, &dockerImage, &m.CreatedAt, &m.IsActive)
	if dockerImage.Valid {
		m.DockerImage = dockerImage.String
	}
	db.QueryRow(`SELECT COUNT(DISTINCT user_id) FROM user_solutions WHERE machine_id=$1`, machineID).Scan(&m.SolverCount)
	db.QueryRow(`SELECT COUNT(*) FROM machine_questions WHERE machine_id=$1 AND is_active=true`, machineID).Scan(&m.TotalQuestions)
	return m, err
}

func fetchQuestions(db *sql.DB, machineID int, user *models.User) ([]models.Question, error) {
	rows, _ := db.Query(`SELECT id, question_order, title, description, points_reward, hint, hint_cost FROM machine_questions WHERE machine_id=$1 AND is_active=true ORDER BY question_order`, machineID)
	defer rows.Close()

	var questions []models.Question
	for rows.Next() {
		var q models.Question
		var hint sql.NullString
		rows.Scan(&q.ID, &q.QuestionOrder, &q.Title, &q.Description, &q.PointsReward, &hint, &q.HintCost)
		if hint.Valid {
			q.Hint = hint.String
		}
		if user != nil {
			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_solutions WHERE user_id=$1 AND question_id=$2)`, user.ID, q.ID).Scan(&q.Solved)
		}
		questions = append(questions, q)
	}
	return questions, nil
}

func getRecentSolvers(db *sql.DB, machineID int, limit int) ([]models.Solver, error) {
	rows, _ := db.Query(`SELECT u.id, u.username, COALESCE(u.avatar, '/static/images/avatar.png'), us.solved_at FROM user_solutions us JOIN users u ON us.user_id = u.id WHERE us.machine_id = $1 ORDER BY us.solved_at DESC LIMIT $2`, machineID, limit)
	defer rows.Close()

	var solvers []models.Solver
	for rows.Next() {
		var s models.Solver
		rows.Scan(&s.UserID, &s.Username, &s.Avatar, &s.SolvedAt)
		solvers = append(solvers, s)
	}
	return solvers, nil
}

func sessionUserID(session *sessions.Session) (int, bool) {
	val, ok := session.Values["user_id"]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func GetMachineDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		machineIDStr := vars["id"]
		machineID, _ := strconv.Atoi(machineIDStr)

		machine, err := fetchMachine(db, machineID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "Makine bulunamadı")
			} else {
				writeError(w, http.StatusInternalServerError, "Veritabanı hatası")
			}
			return
		}

		recentSolvers, _ := getRecentSolvers(db, machineID, 10)

		writeSuccess(w, http.StatusOK, map[string]interface{}{
			"machine":        machine,
			"recent_solvers": recentSolvers,
		})
	}
}

func StartCleanupScheduler(db *sql.DB) {
	ticker := time.NewTicker(5 * time.Minute) // Her 5 dakikada bir çalış
	go func() {
		for range ticker.C {
			cleanupExpiredSessions(db)
		}
	}()
}

func cleanupExpiredSessions(db *sql.DB) {
	// UTC zaman kullan
	now := time.Now().UTC()

	rows, err := db.Query(`
        SELECT id, container_id, user_id, machine_id, host_port
        FROM machine_sessions 
        WHERE ended_at IS NULL AND expires_at < $1
    `, now)
	if err != nil {
		log.Printf("Cleanup sorgu hatası: %v", err)
		return
	}
	defer rows.Close()

	var cleanedCount int
	for rows.Next() {
		var sessionID int
		var containerID string
		var userID, machineID int
		var hostPort int

		err := rows.Scan(&sessionID, &containerID, &userID, &machineID, &hostPort)
		if err != nil {
			log.Printf("Scan hatası: %v", err)
			continue
		}

		// Container'ı durdur
		if containerID != "" {
			if err := stopDockerContainer(containerID); err != nil {
				log.Printf("Container durdurma hatası (ID: %s): %v", containerID, err)
			} else {
				log.Printf("Container durduruldu: %s (User: %d, Machine: %d)",
					containerID, userID, machineID)
			}
		}

		// Oturumu sonlandır - UTC kullan
		_, err = db.Exec(`
            UPDATE machine_sessions 
            SET ended_at = $1 
            WHERE id = $2 AND ended_at IS NULL
        `, now, sessionID)

		if err != nil {
			log.Printf("Session güncelleme hatası (ID: %d): %v", sessionID, err)
		} else {
			cleanedCount++
			// Activity log ekle - UTC kullan
			_, err = db.Exec(`
                INSERT INTO activity_logs (user_id, action_type, machine_id, ip_address, created_at)
                VALUES ($1, 'auto_stop', $2, $3, $4)
            `, userID, machineID, "system", now)

			if err != nil {
				log.Printf("Activity log ekleme hatası: %v", err)
			}
		}
	}

	if cleanedCount > 0 {
		log.Printf("✓ %d süresi dolmuş oturum temizlendi", cleanedCount)
	}
}
