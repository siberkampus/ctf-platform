package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
)

func Auth(store *sessions.CookieStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, _ := store.Get(r, "session")
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

				token, err := jwt.Parse(tokenParts[1], func(token *jwt.Token) (interface{}, error) {
					return []byte("jwt-secret-key"), nil
				})

				if err != nil || !token.Valid {
					http.Error(w, "Geçersiz token", http.StatusUnauthorized)
					return
				}

				claims, ok := token.Claims.(jwt.MapClaims)
				if !ok {
					http.Error(w, "Geçersiz token claims", http.StatusUnauthorized)
					return
				}

				// JWT'de sayısal değerler float64 olarak gelir
				userID := fmt.Sprintf("%v", claims["user_id"])
				username, _ := claims["username"].(string)

				r.Header.Set("X-User-ID", userID)
				r.Header.Set("X-User-Username", username)
			} else {
				// Session'dan — user_id int olarak saklanıyor
				userID := fmt.Sprintf("%v", session.Values["user_id"])
				username, _ := session.Values["username"].(string)

				r.Header.Set("X-User-ID", userID)
				r.Header.Set("X-User-Username", username)
			}

			next.ServeHTTP(w, r)
		})
	}
}
