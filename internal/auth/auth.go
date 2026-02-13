// Package auth provides JWT authentication and RBAC for QueryBridge.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken    = errors.New("invalid or expired token")
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrPermissionDenied = errors.New("permission denied")
	ErrUserNotFound    = errors.New("user not found")
)

// Role represents a user role.
type Role string

const (
	RoleAdmin   Role = "admin"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
)

// User represents an authenticated user.
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	APIKey    string    `json:"api_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	LastLogin time.Time `json:"last_login,omitempty"`
}

// Claims represents JWT claims.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
	jwt.RegisteredClaims
}

// Config holds authentication configuration.
type Config struct {
	JWTSecret       string        `json:"jwt_secret"`
	TokenExpiry     time.Duration `json:"token_expiry"`
	RefreshExpiry   time.Duration `json:"refresh_expiry"`
	APIKeyLength    int           `json:"api_key_length"`
}

// DefaultConfig returns default authentication configuration.
func DefaultConfig() Config {
	return Config{
		JWTSecret:     generateSecret(32),
		TokenExpiry:   time.Hour,
		RefreshExpiry: 24 * time.Hour * 7,
		APIKeyLength:  32,
	}
}

// Authenticator handles JWT token generation and validation.
type Authenticator struct {
	config Config
	users  map[string]*User // In-memory user store
	keys   map[string]string // API key -> user ID
	mu     sync.RWMutex
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(cfg Config) *Authenticator {
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = generateSecret(32)
	}
	if cfg.TokenExpiry == 0 {
		cfg.TokenExpiry = time.Hour
	}
	if cfg.APIKeyLength == 0 {
		cfg.APIKeyLength = 32
	}

	auth := &Authenticator{
		config: cfg,
		users:  make(map[string]*User),
		keys:   make(map[string]string),
	}

	// Create default admin user
	auth.CreateUser("admin", "admin", RoleAdmin)

	return auth
}

// CreateUser creates a new user and returns the API key.
func (a *Authenticator) CreateUser(id, username string, role Role) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	apiKey := generateSecret(a.config.APIKeyLength)

	user := &User{
		ID:        id,
		Username:  username,
		Role:      role,
		APIKey:    apiKey,
		CreatedAt: time.Now(),
	}

	a.users[id] = user
	a.keys[apiKey] = id

	return apiKey, nil
}

// ValidateAPIKey validates an API key and returns the user.
func (a *Authenticator) ValidateAPIKey(apiKey string) (*User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	userID, ok := a.keys[apiKey]
	if !ok {
		return nil, ErrInvalidAPIKey
	}

	user, ok := a.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	return user, nil
}

// GenerateToken creates a JWT token for a user.
func (a *Authenticator) GenerateToken(user *User) (string, error) {
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.config.TokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "querybridge",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.JWTSecret))
}

// ValidateToken validates a JWT token and returns the claims.
func (a *Authenticator) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(a.config.JWTSecret), nil
	})

	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GetUser retrieves a user by ID.
func (a *Authenticator) GetUser(id string) (*User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	user, ok := a.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// ListUsers returns all users (admin only).
func (a *Authenticator) ListUsers() []*User {
	a.mu.RLock()
	defer a.mu.RUnlock()

	users := make([]*User, 0, len(a.users))
	for _, u := range a.users {
		// Don't expose API keys in list
		userCopy := *u
		userCopy.APIKey = ""
		users = append(users, &userCopy)
	}
	return users
}

// DeleteUser removes a user.
func (a *Authenticator) DeleteUser(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user, ok := a.users[id]
	if !ok {
		return ErrUserNotFound
	}

	delete(a.keys, user.APIKey)
	delete(a.users, id)
	return nil
}

// UpdateLastLogin updates the user's last login time.
func (a *Authenticator) UpdateLastLogin(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if user, ok := a.users[id]; ok {
		user.LastLogin = time.Now()
	}
}

// generateSecret generates a random hex string.
func generateSecret(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
