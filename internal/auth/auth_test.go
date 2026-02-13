package auth

import (
	"testing"
)

func TestAuthenticator(t *testing.T) {
	auth := NewAuthenticator(DefaultConfig())

	// Test create user
	apiKey, err := auth.CreateUser("user1", "testuser", RoleAnalyst)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if apiKey == "" {
		t.Error("expected API key to be generated")
	}

	// Test validate API key
	user, err := auth.ValidateAPIKey(apiKey)
	if err != nil {
		t.Fatalf("failed to validate API key: %v", err)
	}
	if user.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", user.Username)
	}
	if user.Role != RoleAnalyst {
		t.Errorf("expected role 'analyst', got %s", user.Role)
	}

	// Test invalid API key
	_, err = auth.ValidateAPIKey("invalid-key")
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestJWTTokens(t *testing.T) {
	auth := NewAuthenticator(DefaultConfig())

	user := &User{
		ID:       "user1",
		Username: "testuser",
		Role:     RoleAdmin,
	}

	// Generate token
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" {
		t.Error("expected token to be generated")
	}

	// Validate token
	claims, err := auth.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, claims.UserID)
	}
	if claims.Role != RoleAdmin {
		t.Errorf("expected role admin, got %s", claims.Role)
	}

	// Test invalid token
	_, err = auth.ValidateToken("invalid.token.here")
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestRBAC(t *testing.T) {
	// Test admin has all permissions
	if !HasPermission(RoleAdmin, PermManageUsers) {
		t.Error("admin should have manage users permission")
	}
	if !HasPermission(RoleAdmin, PermExecuteQuery) {
		t.Error("admin should have execute query permission")
	}

	// Test analyst permissions
	if !HasPermission(RoleAnalyst, PermExecuteQuery) {
		t.Error("analyst should have execute query permission")
	}
	if HasPermission(RoleAnalyst, PermManageUsers) {
		t.Error("analyst should not have manage users permission")
	}

	// Test viewer permissions
	if !HasPermission(RoleViewer, PermExecuteQuery) {
		t.Error("viewer should have execute query permission")
	}
	if HasPermission(RoleViewer, PermExportQuery) {
		t.Error("viewer should not have export query permission")
	}
}

func TestCanAccessTool(t *testing.T) {
	// Admin can access all
	if !CanAccessTool(RoleAdmin, "describe_databases") {
		t.Error("admin should access describe_databases")
	}
	if !CanAccessTool(RoleAdmin, "clear_cache") {
		t.Error("admin should access clear_cache")
	}

	// Analyst can access most
	if !CanAccessTool(RoleAnalyst, "execute_query") {
		t.Error("analyst should access execute_query")
	}
	if !CanAccessTool(RoleAnalyst, "save_template") {
		t.Error("analyst should access save_template")
	}

	// Viewer has limited access
	if !CanAccessTool(RoleViewer, "execute_query") {
		t.Error("viewer should access execute_query")
	}
	if CanAccessTool(RoleViewer, "export_query") {
		t.Error("viewer should not access export_query")
	}
}

func TestUserManagement(t *testing.T) {
	auth := NewAuthenticator(DefaultConfig())

	// Create user
	_, err := auth.CreateUser("user2", "newuser", RoleViewer)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Get user
	user, err := auth.GetUser("user2")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if user.Username != "newuser" {
		t.Error("username mismatch")
	}

	// List users
	users := auth.ListUsers()
	if len(users) < 2 { // admin + user2
		t.Error("expected at least 2 users")
	}

	// Delete user
	err = auth.DeleteUser("user2")
	if err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}

	// Verify deleted
	_, err = auth.GetUser("user2")
	if err != ErrUserNotFound {
		t.Error("expected user not found after deletion")
	}
}
