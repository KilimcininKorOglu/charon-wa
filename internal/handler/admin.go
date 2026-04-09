package handler

import (
	"net/http"
	"strconv"

	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// AdminCreateUserRequest represents the admin user creation request
type AdminCreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name,omitempty"`
	Role     string `json:"role,omitempty"`
}

// AdminCreateUser creates a new user (admin only)
// POST /api/admin/users
func AdminCreateUser(c echo.Context) error {
	var req AdminCreateUserRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Username, email, and password are required", "MISSING_FIELDS", "")
	}

	// Default role to "user" if not provided
	if req.Role == "" {
		req.Role = "user"
	}

	// Validate role
	validRoles := map[string]bool{"admin": true, "user": true, "viewer": true}
	if !validRoles[req.Role] {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid role", "INVALID_ROLE", "Valid roles: admin, user, viewer")
	}

	createReq := model.CreateUserRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
		Role:     req.Role,
	}

	user, err := service.RegisterUser(createReq)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, err.Error(), "USER_CREATION_FAILED", "")
	}

	return SuccessResponse(c, http.StatusCreated, "User created successfully", user.ToResponse())
}

// ListUsers returns paginated list of all users (admin only)
func ListUsers(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	params := model.ListUsersParams{
		Page:   page,
		Limit:  limit,
		Search: c.QueryParam("search"),
		Role:   c.QueryParam("role"),
	}

	result, err := model.GetAllUsers(params)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to fetch users", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Users retrieved", result)
}

// GetUser returns a single user by ID (admin only)
func GetUser(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	user, err := model.GetUserByID(userID)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "User not found", "NOT_FOUND", "")
	}

	return SuccessResponse(c, http.StatusOK, "User retrieved", user.ToResponse())
}

// UpdateUser updates user fields (admin only)
func UpdateUser(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	var req model.AdminUpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "INVALID_BODY", err.Error())
	}

	// Validate role if provided
	if req.Role != nil {
		validRoles := map[string]bool{"admin": true, "user": true, "viewer": true}
		if !validRoles[*req.Role] {
			return ErrorResponse(c, http.StatusBadRequest, "Invalid role", "INVALID_ROLE", "Valid roles: admin, user, viewer")
		}

		// Prevent admin self-demotion (could lock out all admin access)
		claims := getClaims(c)
		if claims != nil && claims.UserID == userID && *req.Role != "admin" {
			return ErrorResponse(c, http.StatusBadRequest, "Cannot demote your own admin account", "SELF_DEMOTE", "")
		}

		// Prevent demoting the last admin
		if *req.Role != "admin" {
			targetUser, err := model.GetUserByID(userID)
			if err == nil && targetUser.Role == "admin" {
				adminCount, err := model.CountAdminUsers()
				if err == nil && adminCount <= 1 {
					return ErrorResponse(c, http.StatusBadRequest, "Cannot demote the last admin user", "LAST_ADMIN", "")
				}
			}
		}
	}

	user, err := model.AdminUpdateUser(userID, req)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update user", "DB_ERROR", err.Error())
	}

	// Revoke sessions when user is deactivated or role changes
	if (req.IsActive != nil && !*req.IsActive) || req.Role != nil {
		_ = service.DestroyAllUserSessions(userID)
	}

	return SuccessResponse(c, http.StatusOK, "User updated", user.ToResponse())
}

// DeleteUser deletes a user (admin only)
func DeleteUser(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	// Prevent self-deletion
	claims := getClaims(c)
	if claims != nil && claims.UserID == userID {
		return ErrorResponse(c, http.StatusBadRequest, "Cannot delete your own account", "SELF_DELETE", "")
	}

	// Atomic delete with last-admin protection (prevents TOCTOU race)
	if err := model.AdminDeleteUserAtomic(userID); err != nil {
		if err.Error() == "cannot delete the last admin user" {
			return ErrorResponse(c, http.StatusBadRequest, "Cannot delete the last admin user", "LAST_ADMIN", "")
		}
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to delete user", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "User deleted", nil)
}

// GetUserInstances returns instance assignments for a user (admin only)
func GetUserInstances(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	instances, err := model.GetUserInstanceDetails(userID)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to fetch instances", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "User instances retrieved", instances)
}

// AssignInstance assigns an instance to a user (admin only)
func AssignInstance(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	var req model.AssignInstanceRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "INVALID_BODY", err.Error())
	}

	if req.InstanceID == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Instance ID is required", "MISSING_FIELD", "")
	}

	permission := req.PermissionLevel
	if permission == "" {
		permission = "full"
	}

	if err := model.AssignInstanceToUser(userID, req.InstanceID, permission); err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to assign instance", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Instance assigned to user", map[string]interface{}{
		"userId":     userID,
		"instanceId": req.InstanceID,
		"permission": permission,
	})
}

// RevokeInstance removes instance access from a user (admin only)
func RevokeInstance(c echo.Context) error {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", "INVALID_ID", "")
	}

	instanceID := c.Param("instanceId")
	if instanceID == "" {
		return ErrorResponse(c, http.StatusBadRequest, "Instance ID is required", "MISSING_FIELD", "")
	}

	if err := model.RevokeInstanceFromUser(userID, instanceID); err != nil {
		return ErrorResponse(c, http.StatusNotFound, "Instance assignment not found", "NOT_FOUND", "")
	}

	return SuccessResponse(c, http.StatusOK, "Instance access revoked", nil)
}

// GetStats returns dashboard statistics (admin only)
func GetStats(c echo.Context) error {
	stats, err := model.GetAdminStats()
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to fetch stats", "DB_ERROR", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Stats retrieved", stats)
}
