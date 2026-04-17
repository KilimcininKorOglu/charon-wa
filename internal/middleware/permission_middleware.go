package middleware

import (
	"net/http"

	"charon/internal/model"
	"charon/internal/service"

	"github.com/labstack/echo/v4"
)

// RequireInstanceAccess ensures the user has permission to access the requested instance.
// For routes with :instanceId parameter.
// Logic: Admin has full access. Standard User must be linked to the instance in user_instances.
func RequireInstanceAccess() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get claims from context (set by JWTMiddleware)
			userClaims, ok := c.Get("user_claims").(*service.Claims)
			if !ok || userClaims == nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Authentication required",
				})
			}

			// Admin has full access
			if userClaims.Role == "admin" {
				return next(c)
			}

			// Viewer role is read-only; block all instance write operations
			if userClaims.Role == "viewer" {
				return c.JSON(http.StatusForbidden, map[string]interface{}{
					"success": false,
					"message": "Viewers do not have access to instance operations",
				})
			}

			// Get instanceId from path params
			instanceID := c.Param("instanceId")
			if instanceID == "" {
				instanceID = c.QueryParam("instanceId")
			}

			if instanceID == "" {
				return c.JSON(http.StatusBadRequest, map[string]interface{}{
					"success": false,
					"message": "Instance ID is required",
				})
			}

			// Check if user has ANY permission to this instance in DB
			_, err := model.CheckUserInstancePermission(userClaims.UserID, instanceID)
			if err != nil {
				if err == model.ErrNoPermission {
					return c.JSON(http.StatusForbidden, map[string]interface{}{
						"success": false,
						"message": "You do not have access to this instance",
					})
				}
				return c.JSON(http.StatusInternalServerError, map[string]interface{}{
					"success": false,
					"message": "Failed to verify instance access",
				})
			}

			return next(c)
		}
	}
}

// RequirePhoneNumberAccess ensures the user has permission to access instance by phone number.
// For routes with :phoneNumber parameter.
// Logic: Admin has full access. Standard User must be linked to the instance in user_instances.
func RequirePhoneNumberAccess() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get claims from context (set by JWTMiddleware)
			userClaims, ok := c.Get("user_claims").(*service.Claims)
			if !ok || userClaims == nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Authentication required",
				})
			}

			// Admin has full access
			if userClaims.Role == "admin" {
				return next(c)
			}

			// Viewer role is read-only; block all phone-number-scoped operations
			if userClaims.Role == "viewer" {
				return c.JSON(http.StatusForbidden, map[string]interface{}{
					"success": false,
					"message": "Viewers do not have access to instance operations",
				})
			}

			// Get phoneNumber from path params
			phoneNumber := c.Param("phoneNumber")
			if phoneNumber == "" {
				return c.JSON(http.StatusBadRequest, map[string]interface{}{
					"success": false,
					"message": "Phone number is required",
				})
			}

			// Get instance by phone number. Non-existence and permission-denied
			// both return a uniform 403 so callers cannot enumerate which phone
			// numbers exist on the platform.
			inst, err := model.GetActiveInstanceByPhoneNumber(phoneNumber)
			if err != nil {
				return c.JSON(http.StatusForbidden, map[string]interface{}{
					"success": false,
					"message": "You do not have access to this phone number",
				})
			}

			// Check if user has permission to this instance
			_, err = model.CheckUserInstancePermission(userClaims.UserID, inst.InstanceID)
			if err != nil {
				// Treat permission errors and verification errors identically from
				// the caller's perspective — log internally, expose uniform 403.
				return c.JSON(http.StatusForbidden, map[string]interface{}{
					"success": false,
					"message": "You do not have access to this phone number",
				})
			}

			// Store resolved instance ID for handlers to reuse (avoid duplicate DB queries)
			c.Set("resolved_instance_id", inst.InstanceID)

			return next(c)
		}
	}
}
