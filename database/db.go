// database/db.go
package database

import (
    "database/sql"
    "fmt"
    "log"
    "os"
    
    _ "github.com/lib/pq"
)

func Connect() (*sql.DB, error) {
    host := getEnv("DB_HOST", "localhost")
    port := getEnv("DB_PORT", "5432")
    user := getEnv("DB_USER", "postgres")
    password := getEnv("DB_PASSWORD", "muhammed")
    dbname := getEnv("DB_NAME", "ctf_platform")

    connStr := fmt.Sprintf(
        "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
        host, port, user, password, dbname,
    )

    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, err
    }

    // Bağlantıyı test et
    err = db.Ping()
    if err != nil {
        return nil, err
    }

    // Bağlantı havuzu ayarları
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(25)
    
    log.Println("Veritabanına başarıyla bağlandı")
    return db, nil
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func InitDB(db *sql.DB) error {
    // Kullanıcılar tablosu
    _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id SERIAL PRIMARY KEY,
            username VARCHAR(50) UNIQUE NOT NULL,
            email VARCHAR(100) UNIQUE NOT NULL,
            password_hash VARCHAR(255) NOT NULL,
            avatar TEXT,
            bio TEXT,
            location VARCHAR(100),
            website VARCHAR(255),
            is_vip BOOLEAN DEFAULT FALSE,
            vip_expiry_date TIMESTAMP,
            points INTEGER DEFAULT 0,
            rank INTEGER DEFAULT 0,
            two_factor_enabled BOOLEAN DEFAULT FALSE,
            created_at TIMESTAMP DEFAULT NOW(),
            last_login TIMESTAMP,
            is_active BOOLEAN DEFAULT TRUE
        )
    `)
    if err != nil {
        return err
    }

    // Makineler tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS machines (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) UNIQUE NOT NULL,
            description TEXT,
            difficulty VARCHAR(20) CHECK (difficulty IN ('easy', 'medium', 'hard', 'expert')),
            points_reward INTEGER DEFAULT 100,
            is_vip_only BOOLEAN DEFAULT FALSE,
            docker_image VARCHAR(255),
            creator_id INTEGER REFERENCES users(id),
            created_at TIMESTAMP DEFAULT NOW(),
            updated_at TIMESTAMP,
            is_active BOOLEAN DEFAULT TRUE
        )
    `)
    if err != nil {
        return err
    }

    // Makine soruları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS machine_questions (
            id SERIAL PRIMARY KEY,
            machine_id INTEGER REFERENCES machines(id) ON DELETE CASCADE,
            question_order INTEGER NOT NULL,
            title VARCHAR(200) NOT NULL,
            description TEXT,
            flag_hash VARCHAR(255) NOT NULL,
            points_reward INTEGER DEFAULT 50,
            hint TEXT,
            hint_cost INTEGER DEFAULT 10,
            is_active BOOLEAN DEFAULT TRUE,
            UNIQUE(machine_id, question_order)
        )
    `)
    if err != nil {
        return err
    }

    // Kullanıcı çözümleri tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS user_solutions (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            machine_id INTEGER REFERENCES machines(id) ON DELETE CASCADE,
            question_id INTEGER REFERENCES machine_questions(id) ON DELETE CASCADE,
            attempt_count INTEGER DEFAULT 1,
            used_hint BOOLEAN DEFAULT FALSE,
            solved_at TIMESTAMP DEFAULT NOW(),
            UNIQUE(user_id, question_id)
        )
    `)
    if err != nil {
        return err
    }

    // Kullanıcı oturumları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS user_sessions (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            session_token VARCHAR(255) UNIQUE NOT NULL,
            device VARCHAR(255),
            ip_address VARCHAR(45),
            location VARCHAR(100),
            user_agent TEXT,
            created_at TIMESTAMP DEFAULT NOW(),
            last_activity TIMESTAMP DEFAULT NOW(),
            expires_at TIMESTAMP NOT NULL,
            is_active BOOLEAN DEFAULT TRUE,
            terminated_at TIMESTAMP
        )
    `)
    if err != nil {
        return err
    }

    // Kullanıcı ayarları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS user_settings (
            user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
            email_notifications BOOLEAN DEFAULT TRUE,
            browser_notifications BOOLEAN DEFAULT TRUE,
            sound_enabled BOOLEAN DEFAULT FALSE,
            profile_public BOOLEAN DEFAULT TRUE,
            show_activity BOOLEAN DEFAULT TRUE,
            show_online_status BOOLEAN DEFAULT TRUE,
            theme VARCHAR(20) DEFAULT 'dark',
            font_size VARCHAR(20) DEFAULT 'medium',
            language VARCHAR(10) DEFAULT 'tr',
            updated_at TIMESTAMP DEFAULT NOW()
        )
    `)
    if err != nil {
        return err
    }

    // Başarılar tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS achievements (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            description TEXT,
            icon VARCHAR(255),
            points_reward INTEGER DEFAULT 0
        )
    `)
    if err != nil {
        return err
    }

    // Kullanıcı başarıları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS user_achievements (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            achievement_id INTEGER REFERENCES achievements(id) ON DELETE CASCADE,
            earned_at TIMESTAMP DEFAULT NOW(),
            UNIQUE(user_id, achievement_id)
        )
    `)
    if err != nil {
        return err
    }

    // VIP satın alma geçmişi tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS vip_purchases (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            package VARCHAR(20) NOT NULL,
            price DECIMAL(10,2) NOT NULL,
            payment_method VARCHAR(50),
            purchased_at TIMESTAMP DEFAULT NOW(),
            expiry_date TIMESTAMP NOT NULL
        )
    `)
    if err != nil {
        return err
    }

    // Kampanya kodları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS campaign_codes (
            id SERIAL PRIMARY KEY,
            code VARCHAR(50) UNIQUE NOT NULL,
            discount_percent INTEGER NOT NULL,
            expires_at TIMESTAMP NOT NULL,
            is_active BOOLEAN DEFAULT TRUE
        )
    `)
    if err != nil {
        return err
    }

    // Makine oturumları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS machine_sessions (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            machine_id INTEGER REFERENCES machines(id) ON DELETE CASCADE,
            container_id VARCHAR(255),
            started_at TIMESTAMP DEFAULT NOW(),
            expires_at TIMESTAMP NOT NULL,
            ended_at TIMESTAMP
        )
    `)
    if err != nil {
        return err
    }

    // İpucu kullanım logları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS hint_usage (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            machine_id INTEGER REFERENCES machines(id) ON DELETE CASCADE,
            question_id INTEGER REFERENCES machine_questions(id) ON DELETE CASCADE,
            used_at TIMESTAMP DEFAULT NOW()
        )
    `)
    if err != nil {
        return err
    }

    // Aktivite logları tablosu
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS activity_logs (
            id SERIAL PRIMARY KEY,
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
            action_type VARCHAR(50) NOT NULL,
            machine_id INTEGER REFERENCES machines(id) ON DELETE SET NULL,
            question_id INTEGER REFERENCES machine_questions(id) ON DELETE SET NULL,
            ip_address VARCHAR(45),
            created_at TIMESTAMP DEFAULT NOW()
        )
    `)
    if err != nil {
        return err
    }

    
    //Admin tablosu
      _, err = db.Exec(`
        CREATE TABLE admins (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    avatar TEXT,
    role VARCHAR(20) DEFAULT 'admin',
    last_login TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    is_active BOOLEAN DEFAULT TRUE
);
    `)
    if err != nil {
        return err
    }


    //Admin logları
       _, err = db.Exec(`
       CREATE TABLE admin_logs (
    id SERIAL PRIMARY KEY,
    admin_id INTEGER REFERENCES admins(id),
    action_type VARCHAR(50) NOT NULL,
    user_id INTEGER REFERENCES users(id),
    username VARCHAR(50),
    ip_address VARCHAR(45),
    details TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);
    `)
    if err != nil {
        return err
    }

    //Sistem ayarları
     _, err = db.Exec(`
       CREATE TABLE system_settings (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT,
    updated_at TIMESTAMP DEFAULT NOW()
);
    `)
    if err != nil {
        return err
    }
    
    // İndeksler
    _, err = db.Exec(`
        CREATE INDEX IF NOT EXISTS idx_user_solutions_user_id ON user_solutions(user_id);
        CREATE INDEX IF NOT EXISTS idx_user_solutions_machine_id ON user_solutions(machine_id);
        CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions(user_id);
        CREATE INDEX IF NOT EXISTS idx_user_sessions_token ON user_sessions(session_token);
        CREATE INDEX IF NOT EXISTS idx_machines_difficulty ON machines(difficulty);
        CREATE INDEX IF NOT EXISTS idx_machines_is_vip ON machines(is_vip_only);
        CREATE INDEX IF NOT EXISTS idx_activity_logs_user_id ON activity_logs(user_id);
        CREATE INDEX IF NOT EXISTS idx_activity_logs_created_at ON activity_logs(created_at);
    `)
    
    return err
}