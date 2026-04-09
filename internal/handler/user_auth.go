// internal/handler/user_auth.go
package handler

import (
	"database/sql"
	"log"
	"net/http"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// RegisterRequest represents the registration request payload
type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name,omitempty"`
}

// LoginRequest represents the login request payload
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	User model.UserResponse `json:"user"`
}

// Register handles user registration
// POST /register
func Register(c echo.Context) error {
	var req RegisterRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Validate required fields
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Username, email, and password are required", "MISSING_FIELDS", "")
	}

	// Create user
	createReq := model.CreateUserRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
		Role:     "user", // Default role for registration
	}

	user, err := service.RegisterUser(createReq)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, err.Error(), "REGISTRATION_FAILED", "")
	}

	// Create session
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()
	rawToken, err := service.CreateUserSession(user, ipAddress, userAgent)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create session", "SESSION_CREATION_FAILED", err.Error())
	}
	setSessionCookie(c, rawToken, service.GetSessionExpiry())

	auditLog := &model.AuditLog{
		UserID:       sql.NullInt64{Int64: user.ID, Valid: true},
		Action:       "user.register",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: user.Username, Valid: true},
		IPAddress:    sql.NullString{String: ipAddress, Valid: true},
		UserAgent:    sql.NullString{String: userAgent, Valid: true},
	}

	err = model.LogAction(auditLog)
	if err != nil {
		log.Printf("❌ ERROR: Failed to log audit: %v", err)
	} else {
		log.Printf("✅ SUCCESS: Audit log saved successfully")
	}

	return SuccessResponse(c, http.StatusCreated, "User registered successfully", AuthResponse{
		User: user.ToResponse(),
	})
}

// LoginUser handles user login with username/password
// POST /login
func LoginUser(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Validate required fields
	if req.Username == "" || req.Password == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Username and password are required", "MISSING_FIELDS", "")
	}

	// Check account lockout before authentication
	userID, lookupErr := model.GetUserIDByUsername(req.Username)
	if lookupErr == nil {
		locked, _ := model.IsAccountLocked(userID)
		if locked {
			return ErrorResponse(c, http.StatusTooManyRequests, "Account temporarily locked due to too many failed login attempts. Try again later.", "ACCOUNT_LOCKED", "")
		}
	}

	// Authenticate user
	user, err := service.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		if err == model.ErrInvalidCredentials {
			// Track failed login attempt
			if lookupErr == nil {
				_ = model.IncrementFailedLogin(userID)
			}
			return ErrorResponse(c, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS", "")
		}
		return ErrorResponse(c, http.StatusBadRequest, err.Error(), "AUTHENTICATION_FAILED", "")
	}

	// Reset failed login counter on successful login
	_ = model.ResetFailedLogin(int(user.ID))

	// Create session
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()
	rawToken, err := service.CreateUserSession(user, ipAddress, userAgent)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create session", "SESSION_CREATION_FAILED", err.Error())
	}
	setSessionCookie(c, rawToken, service.GetSessionExpiry())

	// Log login
	err = model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: user.ID, Valid: true},
		Action:       "user.login",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: user.Username, Valid: true},
		IPAddress:    sql.NullString{String: ipAddress, Valid: true},
		UserAgent:    sql.NullString{String: userAgent, Valid: true},
	})
	if err != nil {
		log.Printf("⚠️ Failed to log audit: %v", err)
	}

	return SuccessResponse(c, http.StatusOK, "Login successful", AuthResponse{
		User: user.ToResponse(),
	})
}

// RefreshToken is a deprecated stub kept for compile compatibility until Phase 3
// removes the route from main.go. Session-based auth does not use refresh tokens.
// POST /refresh
func RefreshToken(c echo.Context) error {
	return ErrorResponse(c, http.StatusGone, "Refresh tokens are no longer supported. Use session-based auth.", "DEPRECATED", "")
}

// LogoutUser handles user logout by destroying the session
// POST /api/logout
func LogoutUser(c echo.Context) error {
	// Get user from context for audit logging
	userClaims, _ := c.Get("user_claims").(*service.Claims)

	// Read session cookie and destroy session
	cookie, err := c.Cookie("session")
	if err == nil && cookie.Value != "" {
		if destroyErr := service.DestroySession(cookie.Value); destroyErr != nil {
			log.Printf("⚠️ Failed to destroy session: %v", destroyErr)
		}
	}
	clearSessionCookie(c)

	// Log logout
	if userClaims != nil {
		_ = model.LogAction(&model.AuditLog{
			UserID:       sql.NullInt64{Int64: userClaims.UserID, Valid: true},
			Action:       "user.logout",
			ResourceType: sql.NullString{String: "user", Valid: true},
			ResourceID:   sql.NullString{String: userClaims.Username, Valid: true},
			IPAddress:    sql.NullString{String: c.RealIP(), Valid: true},
			UserAgent:    sql.NullString{String: c.Request().UserAgent(), Valid: true},
		})
	}

	return SuccessResponse(c, http.StatusOK, "Logged out successfully", nil)
}

// GetCurrentUser returns the current authenticated user's profile
// GET /api/me
func GetCurrentUser(c echo.Context) error {
	// Get user from context (set by JWT middleware)
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	// Get full user details
	user, err := model.GetUserByID(userClaims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "User not found", "USER_NOT_FOUND", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "User profile retrieved", user.ToResponse())
}

// UpdateCurrentUser updates the current user's profile
// PUT /api/me
func UpdateCurrentUser(c echo.Context) error {
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	var req model.UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Get current user
	user, err := model.GetUserByID(userClaims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "User not found", "USER_NOT_FOUND", err.Error())
	}

	// Update fields if provided
	if req.FullName != nil {
		user.FullName = sql.NullString{String: *req.FullName, Valid: true}
	}
	if req.AvatarURL != nil {
		user.AvatarURL = sql.NullString{String: *req.AvatarURL, Valid: true}
	}

	err = model.UpdateUser(user)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update user", "UPDATE_FAILED", err.Error())
	}

	// Log update
	_ = model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: user.ID, Valid: true},
		Action:       "user.update",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: user.Username, Valid: true},
		IPAddress:    sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent:    sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})

	return SuccessResponse(c, http.StatusOK, "User profile updated successfully", user.ToResponse())
}

// ChangePassword handles password change for local auth users
// PUT /api/me/password
func ChangePassword(c echo.Context) error {
	userClaims, ok := c.Get("user_claims").(*service.Claims)
	if !ok {
		return ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	var req model.ChangePasswordRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Old password and new password are required", "MISSING_FIELDS", "")
	}

	// Get user
	user, err := model.GetUserByID(userClaims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "User not found", "USER_NOT_FOUND", err.Error())
	}

	// Check if user is local auth
	if user.AuthProvider != "local" {
		return ErrorResponse(c, http.StatusBadRequest, "Cannot change password for OAuth users", "OAUTH_USER", "")
	}

	// Verify old password
	if !user.PasswordHash.Valid {
		return ErrorResponse(c, http.StatusBadRequest, "Password not set", "NO_PASSWORD", "")
	}

	// Authenticate with old password
	_, err = service.AuthenticateUser(user.Username, req.OldPassword)
	if err != nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Invalid old password", "INVALID_OLD_PASSWORD", "")
	}

	// Hash new password
	newPasswordHash, err := helper.HashPassword(req.NewPassword)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to hash password", "HASH_FAILED", err.Error())
	}

	// Update password
	err = model.UpdateUserPassword(user.ID, newPasswordHash)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update password", "UPDATE_FAILED", err.Error())
	}

	// Destroy all sessions for security
	err = service.DestroyAllUserSessions(user.ID)
	if err != nil {
		log.Printf("❌ ERROR: Failed to destroy sessions: %v", err)
	} else {
		log.Printf("✅ SUCCESS: All sessions destroyed for user ID: %d", user.ID)
	}
	clearSessionCookie(c)

	// Log password change
	err = model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: user.ID, Valid: true},
		Action:       "user.password_change",
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: user.Username, Valid: true},
		IPAddress:    sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent:    sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})
	if err != nil {
		log.Printf("⚠️ Failed to log password change audit: %v", err)
	}

	return SuccessResponse(c, http.StatusOK, "Password changed successfully. Please login again.", nil)
}
