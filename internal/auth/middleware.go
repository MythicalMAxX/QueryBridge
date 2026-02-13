// Package auth provides HTTP middleware for authentication.
package auth

import (
	"context"
	"net/http"
	"strings"
)

// ContextKey is the type for context keys.
type ContextKey string

const (
	// UserContextKey is the context key for the authenticated user.
	UserContextKey ContextKey = "user"
	// ClaimsContextKey is the context key for JWT claims.
	ClaimsContextKey ContextKey = "claims"
)

// Middleware provides authentication middleware for HTTP handlers.
type Middleware struct {
	auth       *Authenticator
	publicPaths []string
}

// NewMiddleware creates a new authentication middleware.
func NewMiddleware(auth *Authenticator) *Middleware {
	return &Middleware{
		auth: auth,
		publicPaths: []string{
			"/api/auth/token",
			"/api/health",
			"/metrics",
		},
	}
}

// Authenticate validates the request and adds user context.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is public
		for _, path := range m.publicPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Skip auth for static files
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Support both "Bearer <token>" and "ApiKey <key>" formats
		var claims *Claims
		var err error

		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err = m.auth.ValidateToken(tokenString)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid token")
				return
			}
		} else if strings.HasPrefix(authHeader, "ApiKey ") {
			apiKey := strings.TrimPrefix(authHeader, "ApiKey ")
			user, err := m.auth.ValidateAPIKey(apiKey)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid API key")
				return
			}
			// Create claims from user
			claims = &Claims{
				UserID:   user.ID,
				Username: user.Username,
				Role:     user.Role,
			}
		} else {
			writeAuthError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		// Add claims to context
		ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole returns middleware that requires a specific role.
func (m *Middleware) RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaimsFromContext(r.Context())
			if claims == nil {
				writeAuthError(w, http.StatusUnauthorized, "not authenticated")
				return
			}

			// Check if user has any of the required roles
			hasRole := false
			for _, role := range roles {
				if claims.Role == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				writeAuthError(w, http.StatusForbidden, "insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission returns middleware that requires a specific permission.
func (m *Middleware) RequirePermission(permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaimsFromContext(r.Context())
			if claims == nil {
				writeAuthError(w, http.StatusUnauthorized, "not authenticated")
				return
			}

			if !HasPermission(claims.Role, permission) {
				writeAuthError(w, http.StatusForbidden, "insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetClaimsFromContext extracts claims from the request context.
func GetClaimsFromContext(ctx context.Context) *Claims {
	claims, ok := ctx.Value(ClaimsContextKey).(*Claims)
	if !ok {
		return nil
	}
	return claims
}

// writeAuthError writes a JSON error response.
func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":true,"message":"` + message + `"}`))
}
