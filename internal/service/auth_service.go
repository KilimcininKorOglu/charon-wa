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

// RegisterUser creates a new user account
func RegisterUser(req model.CreateUserRequest) (*model.User, error) {
	// Validate input
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return nil, errors.New("username, email, and password are required")
	}

	// Check if user already exists
	existingUser, _ := model.GetUserByUsername(req.Username)
	if existingUser != nil {
		return nil, errors.New("username already exists")
	}

	existingUser, _ = model.GetUserByEmail(req.Email)
	if existingUser != nil {
		return nil, errors.New("email already exists")
	}

	// Hash password
	passwordHash, err := helper.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	// Set default role if not provided
	role := req.Role
	if role == "" {
		role = "user"
	}

	// Validate role
	if role != "admin" && role != "user" && role != "viewer" {
		return nil, errors.New("invalid role")
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

	err = model.CreateUser(user)
	if err != nil {
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
