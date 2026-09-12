// internal/handler/user_auth.go
package handler

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"charon/config"
	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// LoginRequest represents the login request payload
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	User model.UserResponse `json:"user"`
}

// writeUserAuditLog records a user-scoped action. A logging failure never fails
// the request, but it is always reported.
func writeUserAuditLog(c echo.Context, userID int64, username, action string) {
	err := model.LogAction(&model.AuditLog{
		UserID:       sql.NullInt64{Int64: userID, Valid: true},
		Action:       action,
		ResourceType: sql.NullString{String: "user", Valid: true},
		ResourceID:   sql.NullString{String: username, Valid: true},
		IPAddress:    sql.NullString{String: c.RealIP(), Valid: true},
		UserAgent:    sql.NullString{String: c.Request().UserAgent(), Valid: true},
	})
	if err != nil {
		log.Printf("⚠️ Failed to write %s audit log for user %d: %v", action, userID, err)
	}
}

// authenticateLogin resolves the credentials to a user. Every failure answers
// with the SAME generic response so attackers cannot distinguish account states
// (existence, lockout, disabled, etc.). The returned error is an already-written
// ErrorResponse.
func authenticateLogin(c echo.Context, req LoginRequest) (*model.User, error) {
	userID, lookupErr := model.GetUserIDByUsername(req.Username)
	if lookupErr == nil {
		if locked, _ := model.IsAccountLocked(userID); locked {
			return nil, ErrorResponse(c, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS", "account locked")
		}
	}

	user, err := service.AuthenticateUser(req.Username, req.Password)
	if err == nil {
		if resetErr := model.ResetFailedLogin(int(user.ID)); resetErr != nil {
			log.Printf("⚠️ Failed to reset the failed login counter for user %d: %v", user.ID, resetErr)
		}
		return user, nil
	}

	if errors.Is(err, model.ErrInvalidCredentials) {
		if lookupErr == nil {
			if incErr := model.IncrementFailedLogin(userID); incErr != nil {
				log.Printf("⚠️ Failed to increment the failed login counter for user %d: %v", userID, incErr)
			}
		}
		return nil, ErrorResponse(c, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS", "")
	}

	// Any other authentication error (disabled user, DB failure, etc.) must
	// NOT leak details to the client. Log server-side only.
	return nil, ErrorResponse(c, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS", err.Error())
}

// LoginUser handles user login with username/password
// POST /login
func LoginUser(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.Username == "" || req.Password == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Username and password are required", "MISSING_FIELDS", "")
	}

	user, errResp := authenticateLogin(c, req)
	if errResp != nil {
		return errResp
	}

	rawToken, err := service.CreateUserSession(user, c.RealIP(), c.Request().UserAgent())
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to create session", "SESSION_CREATION_FAILED", err.Error())
	}
	setSessionCookie(c, rawToken, service.GetSessionExpiry())

	writeUserAuditLog(c, user.ID, user.Username, "user.login")

	return SuccessResponse(c, http.StatusOK, "Login successful", AuthResponse{
		User: user.ToResponse(),
	})
}

// LogoutUser handles user logout by destroying the session
// POST /logout (public route — no middleware, reads cookie directly)
func LogoutUser(c echo.Context) error {
	// Read session cookie, validate for audit logging, then destroy
	cookie, err := c.Cookie("session")
	if err == nil && cookie.Value != "" {
		// Attempt to read session for audit before destroying
		session, validateErr := model.GetAuthSessionByToken(cookie.Value)
		if validateErr == nil && session != nil {
			writeUserAuditLog(c, session.UserID, session.Username, "user.logout")
		}

		if destroyErr := service.DestroySession(cookie.Value); destroyErr != nil {
			log.Printf("⚠️ Failed to destroy session: %v", destroyErr)
		}
	}
	clearSessionCookie(c)

	return SuccessResponse(c, http.StatusOK, "Logged out successfully", nil)
}

// GetCurrentUser returns the current authenticated user's profile
// GET /api/me
func GetCurrentUser(c echo.Context) error {
	// Get user from context (set by session middleware)
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

// parsePhoneRegionPreference validates a per-user region code. An empty value
// clears the preference, so the user follows the system default again. The
// second result is a written ErrorResponse for the caller to propagate.
func parsePhoneRegionPreference(c echo.Context, raw string) (sql.NullString, error) {
	region := strings.ToUpper(strings.TrimSpace(raw))
	if region == "" {
		return sql.NullString{}, nil
	}

	if !config.IsSupportedRegion(region) {
		return sql.NullString{}, ErrorResponse(c, http.StatusBadRequest,
			"Unknown region code", "INVALID_REGION",
			"Expected an ISO 3166-1 alpha-2 code such as TR, got "+region)
	}

	return sql.NullString{String: region, Valid: true}, nil
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
	if req.PhoneDefaultRegion != nil {
		region, errResp := parsePhoneRegionPreference(c, *req.PhoneDefaultRegion)
		if errResp != nil {
			return errResp
		}
		user.PhoneDefaultRegion = region
	}

	err = model.UpdateUser(user)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update user", "UPDATE_FAILED", err.Error())
	}

	writeUserAuditLog(c, user.ID, user.Username, "user.update")

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

	user, err := model.GetUserByID(userClaims.UserID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "User not found", "USER_NOT_FOUND", err.Error())
	}

	if errResp := verifyOldPassword(c, user, req.OldPassword); errResp != nil {
		return errResp
	}

	newPasswordHash, err := helper.HashPassword(req.NewPassword)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to hash password", "HASH_FAILED", err.Error())
	}

	if err := model.UpdateUserPassword(user.ID, newPasswordHash); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update password", "UPDATE_FAILED", err.Error())
	}

	// Destroy all sessions for security
	if err := service.DestroyAllUserSessions(user.ID); err != nil {
		log.Printf("❌ ERROR: Failed to destroy sessions: %v", err)
	} else {
		log.Printf("✅ SUCCESS: All sessions destroyed for user ID: %d", user.ID)
	}
	clearSessionCookie(c)

	writeUserAuditLog(c, user.ID, user.Username, "user.password_change")

	return SuccessResponse(c, http.StatusOK, "Password changed successfully. Please login again.", nil)
}

// verifyOldPassword confirms the account uses local auth and that the supplied
// password is its current one. The returned error is an already-written
// ErrorResponse.
func verifyOldPassword(c echo.Context, user *model.User, oldPassword string) error {
	if user.AuthProvider != "local" {
		return ErrorResponse(c, http.StatusBadRequest, "Cannot change password for OAuth users", "OAUTH_USER", "")
	}
	if !user.PasswordHash.Valid {
		return ErrorResponse(c, http.StatusBadRequest, "Password not set", "NO_PASSWORD", "")
	}

	if _, err := service.AuthenticateUser(user.Username, oldPassword); err != nil {
		return ErrorResponse(c, http.StatusUnauthorized, "Invalid old password", "INVALID_OLD_PASSWORD", "")
	}
	return nil
}
