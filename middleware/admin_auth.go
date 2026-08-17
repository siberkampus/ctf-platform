// middleware/admin_auth.go
package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
)

func AdminAuth(store *sessions.CookieStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Session kontrolü
			session, _ := store.Get(r, "admin_session")

			if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
				// JWT token kontrolü
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" {
					http.Error(w, "Yetkisiz erişim", http.StatusUnauthorized)
					return
				}

				tokenParts := strings.Split(authHeader, " ")
				if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
					http.Error(w, "Geçersiz token formatı", http.StatusUnauthorized)
					return
				}

				// Token doğrulama ve admin yetkisi kontrolü
				claims, err := validateAdminToken(tokenParts[1])
				if err != nil {
					http.Error(w, "Geçersiz token", http.StatusUnauthorized)
					return
				}

				adminID := fmt.Sprintf("%v", claims["admin_id"])
				r.Header.Set("X-Admin-ID", adminID)
				r.Header.Set("X-Admin-Role", fmt.Sprintf("%v", claims["role"]))
			} else {
				role, _ := session.Values["role"].(string)

				// Sadece "admin" değil, yetkili tüm rolleri kontrol et
				if role != "admin" && role != "super_admin" {
					http.Error(w, "Admin yetkisi gerekli", http.StatusForbidden)
					return
				}

				// KRİTİK DÜZELTME: . (string) yerine fmt.Sprintf kullanıyoruz
				adminID := fmt.Sprintf("%v", session.Values["admin_id"])
				r.Header.Set("X-Admin-ID", adminID)
				r.Header.Set("X-Admin-Role", role)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func validateAdminToken(tokenString string) (map[string]interface{}, error) {
	// JWT token doğrulama
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte("admin-secret-key"), nil
	})

	if err != nil || !token.Valid {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("geçersiz token claims")
	}

	// Admin yetkisi kontrolü
	if claims["role"] != "admin" && claims["role"] != "super_admin" {
		return nil, fmt.Errorf("admin yetkisi gerekli")
	}

	return claims, nil
}
