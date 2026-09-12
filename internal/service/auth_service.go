// internal/service/auth_service.go
package service

import (
	"database/sql"
	"errors"

	"charon/internal/helper"
	"charon/internal/model"
)

// Claims represents user identity extracted from session or API key
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// validateNewUser checks the required fields and rejects a username or email
// already in use.
func validateNewUser(req model.CreateUserRequest) error {
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return errors.New("username, email, and password are required")
	}

	if existingUser, _ := model.GetUserByUsername(req.Username); existingUser != nil {
		return errors.New("username already exists")
	}
	if existingUser, _ := model.GetUserByEmail(req.Email); existingUser != nil {
		return errors.New("email already exists")
	}
	return nil
}

// resolveUserRole applies the default role and rejects an unknown one.
func resolveUserRole(requested string) (string, error) {
	role := requested
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" && role != "viewer" {
		return "", errors.New("invalid role")
	}
	return role, nil
}

// RegisterUser creates a new user account
func RegisterUser(req model.CreateUserRequest) (*model.User, error) {
	if err := validateNewUser(req); err != nil {
		return nil, err
	}

	role, err := resolveUserRole(req.Role)
	if err != nil {
		return nil, err
	}

	passwordHash, err := helper.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	// Create user
	user := &model.User{
		Username:      req.Username,
		Email:         req.Email,
		PasswordHash:  sql.NullString{String: passwordHash, Valid: true},
		FullName:      sql.NullString{String: req.FullName, Valid: req.FullName != ""},
		AuthProvider:  "local",
		Role:          role,
		IsActive:      true,
		EmailVerified: false, // Email verification can be added later
	}

	if err := model.CreateUser(user); err != nil {
		return nil, err
	}

	return user, nil
}

// AuthenticateUser validates username/password and returns user if valid
func AuthenticateUser(username, password string) (*model.User, error) {
	// Get user by username
	user, err := model.GetUserByUsername(username)
	if err != nil {
		if err == model.ErrUserNotFound {
			return nil, model.ErrInvalidCredentials
		}
		return nil, err
	}

	// Check if user is active
	if !user.IsActive {
		return nil, errors.New("user account is disabled")
	}

	// Check auth provider - OAuth users cannot login with password
	if user.AuthProvider != "local" {
		return nil, errors.New("please use 'Sign in with " + user.AuthProvider + "' for this account")
	}

	// Verify password
	if !user.PasswordHash.Valid {
		return nil, errors.New("password not set for this account")
	}

	err = helper.VerifyPassword(user.PasswordHash.String, password)
	if err != nil {
		return nil, model.ErrInvalidCredentials
	}

	// Update last login timestamp
	_ = model.UpdateLastLogin(user.ID)

	return user, nil
}
