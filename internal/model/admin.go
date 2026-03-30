package model

import (
	"fmt"

	"hermeswa/database"
)

// AdminStats holds dashboard statistics
type AdminStats struct {
	TotalUsers          int `json:"totalUsers"`
	ActiveUsers         int `json:"activeUsers"`
	TotalInstances      int `json:"totalInstances"`
	ConnectedInstances  int `json:"connectedInstances"`
	ActiveWarmingRooms  int `json:"activeWarmingRooms"`
	ActiveWorkers       int `json:"activeWorkers"`
}

// ListUsersParams holds pagination and filter parameters
type ListUsersParams struct {
	Page   int
	Limit  int
	Search string
	Role   string
}

// PaginatedUsers holds paginated user list
type PaginatedUsers struct {
	Users []UserResponse `json:"users"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

// AdminUpdateUserRequest is the request payload for admin user update
type AdminUpdateUserRequest struct {
	Role     *string `json:"role,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	FullName *string `json:"full_name,omitempty"`
}

// UserInstanceResponse represents an instance assignment for a user
type UserInstanceResponse struct {
	InstanceID      string `json:"instanceId"`
	PermissionLevel string `json:"permissionLevel"`
	CreatedAt       string `json:"createdAt"`
}

// AssignInstanceRequest is the request payload for instance assignment
type AssignInstanceRequest struct {
	InstanceID      string `json:"instanceId"`
	PermissionLevel string `json:"permissionLevel,omitempty"`
}

// GetAllUsers retrieves paginated list of all users
func GetAllUsers(params ListUsersParams) (*PaginatedUsers, error) {
	db := database.AppDB

	countQuery := `SELECT COUNT(*) FROM users WHERE 1=1`
	dataQuery := `
		SELECT id, username, email, password_hash, full_name, avatar_url,
			auth_provider, oauth_provider_id, role, is_active, email_verified,
			created_at, updated_at, last_login_at
		FROM users WHERE 1=1
	`

	var args []interface{}
	argIdx := 1

	if params.Search != "" {
		placeholder := fmt.Sprintf("$%d", argIdx)
		filter := fmt.Sprintf(` AND (username ILIKE %s OR email ILIKE %s OR full_name ILIKE %s)`, placeholder, placeholder, placeholder)
		countQuery += filter
		dataQuery += filter
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	if params.Role != "" {
		filter := fmt.Sprintf(` AND role = $%d`, argIdx)
		countQuery += filter
		dataQuery += filter
		args = append(args, params.Role)
		argIdx++
	}

	// Get total count
	var total int
	err := db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, err
	}

	// Add pagination
	offset := (params.Page - 1) * params.Limit
	dataQuery += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, params.Limit, offset)

	rows, err := db.Query(dataQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserResponse
	for rows.Next() {
		var u User
		err := rows.Scan(
			&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.FullName, &u.AvatarURL,
			&u.AuthProvider, &u.OAuthProviderID, &u.Role, &u.IsActive, &u.EmailVerified,
			&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, u.ToResponse())
	}

	if users == nil {
		users = []UserResponse{}
	}

	return &PaginatedUsers{
		Users: users,
		Total: total,
		Page:  params.Page,
		Limit: params.Limit,
	}, rows.Err()
}

// AdminUpdateUser updates user fields by admin
func AdminUpdateUser(userID int64, req AdminUpdateUserRequest) (*User, error) {
	db := database.AppDB

	user, err := GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	if req.Role != nil {
		user.Role = *req.Role
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.FullName != nil {
		user.FullName.String = *req.FullName
		user.FullName.Valid = true
	}

	query := `
		UPDATE users
		SET role = $1, is_active = $2, full_name = $3, updated_at = NOW()
		WHERE id = $4
	`
	_, err = db.Exec(query, user.Role, user.IsActive, user.FullName, userID)
	if err != nil {
		return nil, err
	}

	// Revoke all sessions when account is disabled
	if req.IsActive != nil && !*req.IsActive {
		_ = RevokeAllUserTokens(userID)
	}

	return GetUserByID(userID)
}

// AdminDeleteUser deletes a user by ID
func AdminDeleteUser(userID int64) error {
	db := database.AppDB
	_, err := db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	return err
}

// GetUserInstanceDetails retrieves instance assignments for a user
func GetUserInstanceDetails(userID int64) ([]UserInstanceResponse, error) {
	db := database.AppDB

	query := `
		SELECT instance_id, permission_level, created_at
		FROM user_instances
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []UserInstanceResponse
	for rows.Next() {
		var inst UserInstanceResponse
		err := rows.Scan(&inst.InstanceID, &inst.PermissionLevel, &inst.CreatedAt)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}

	if instances == nil {
		instances = []UserInstanceResponse{}
	}

	return instances, rows.Err()
}

// RevokeInstanceFromUser removes a user-instance relationship
func RevokeInstanceFromUser(userID int64, instanceID string) error {
	db := database.AppDB
	result, err := db.Exec(
		`DELETE FROM user_instances WHERE user_id = $1 AND instance_id = $2`,
		userID, instanceID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNoPermission
	}
	return nil
}

// GetAdminStats retrieves dashboard statistics
func GetAdminStats() (*AdminStats, error) {
	db := database.AppDB
	stats := &AdminStats{}

	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers)
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_active = true`).Scan(&stats.ActiveUsers)
	db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&stats.TotalInstances)
	db.QueryRow(`SELECT COUNT(*) FROM instances WHERE is_connected = true`).Scan(&stats.ConnectedInstances)
	db.QueryRow(`SELECT COUNT(*) FROM warming_rooms WHERE status = 'ACTIVE'`).Scan(&stats.ActiveWarmingRooms)
	db.QueryRow(`SELECT COUNT(*) FROM outbox_worker_config WHERE enabled = true`).Scan(&stats.ActiveWorkers)

	return stats, nil
}

