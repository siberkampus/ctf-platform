// middleware/logger.go
package middleware

import (
    "log"
    "net/http"
    "time"
)

func Logger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        // İsteği logla
        log.Printf("[%s] %s %s", r.Method, r.RequestURI, time.Since(start))
        
        next.ServeHTTP(w, r)
    })
}

// func CORS(next http.Handler) http.Handler {
//     return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//         w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")
//         w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
//         w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
//         w.Header().Set("Access-Control-Allow-Credentials", "true")
        
//         if r.Method == "OPTIONS" {
//             w.WriteHeader(http.StatusOK)
//             return
//         }
        
//         next.ServeHTTP(w, r)
//     })
// }