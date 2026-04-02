package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// AuthMiddleware returns HTTP middleware that validates JWT access tokens.
// Public paths (login, refresh, health) are excluded from authentication.
func AuthMiddleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			tokenStr := extractToken(r)
			if tokenStr == "" {
				writeUnauthorized(w)
				return
			}

			claims, err := svc.ValidateAccessToken(tokenStr)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := ContextWithUserID(r.Context(), claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isPublicPath(method, path string) bool {
	switch {
	case method == http.MethodPost && path == "/api/v1/auth/login":
		return true
	case method == http.MethodPost && path == "/api/v1/auth/refresh":
		return true
	case method == http.MethodGet && path == "/api/v1/health":
		return true
	case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/media/") && strings.HasSuffix(path, "/poster"):
		return true
	default:
		return false
	}
}

// extractToken gets the JWT from the Authorization header or token query param (SSE fallback).
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{"code": 401, "message": "unauthorized"})
}
