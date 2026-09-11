// internal/handler/warming/context.go
package warming

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"charon/internal/handler"
	warmingModel "charon/internal/model/warming"

	"github.com/labstack/echo/v4"
)

// caller carries the authenticated identity every warming handler needs.
type caller struct {
	UserID  int64
	IsAdmin bool
}

// requireCaller reads the identity the auth middleware put on the context. The
// second result is a written ErrorResponse for the caller to propagate.
func requireCaller(c echo.Context) (caller, error) {
	userID, ok := c.Get("user_id").(int64)
	if !ok {
		return caller{}, handler.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", "")
	}

	role, ok := c.Get("role").(string)
	if !ok {
		role = "user"
	}

	return caller{UserID: userID, IsAdmin: role == "admin"}, nil
}

// pathInt64 reads an int64 path parameter. The second result is a written
// ErrorResponse naming the parameter, for the caller to propagate.
func pathInt64(c echo.Context, param, label, code string) (int64, error) {
	value, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil {
		return 0, handler.ErrorResponse(c, http.StatusBadRequest, "Invalid "+label, code, err.Error())
	}
	return value, nil
}

// requireScriptOwner confirms the caller owns the parent script. An admin
// passes unconditionally. The result is a written ErrorResponse, or nil.
func requireScriptOwner(c echo.Context, scriptID int64, cl caller, denyMessage string) error {
	if cl.IsAdmin {
		return nil
	}

	isOwner, err := warmingModel.CheckScriptOwnership(int(scriptID), cl.UserID)
	if err != nil || !isOwner {
		return handler.ErrorResponse(c, http.StatusForbidden, denyMessage, "FORBIDDEN", "")
	}
	return nil
}

// requireScriptReadAccess allows reading a script the caller owns, plus any
// public script (one with no owner). An admin reads everything.
func requireScriptReadAccess(c echo.Context, scriptID int64, cl caller, denyMessage string) error {
	if cl.IsAdmin {
		return nil
	}

	script, err := warmingModel.GetWarmingScriptByID(int(scriptID))
	if err != nil {
		return handler.ErrorResponse(c, http.StatusNotFound, "Script not found", "SCRIPT_NOT_FOUND", "")
	}
	if script.CreatedBy.Valid && script.CreatedBy.Int64 != cl.UserID {
		return handler.ErrorResponse(c, http.StatusForbidden, denyMessage, "FORBIDDEN", "")
	}
	return nil
}

// errorRule maps one service error onto an HTTP response. Exactly one of
// sentinel and substring is set.
type errorRule struct {
	sentinel  error  // matched with errors.Is
	substring string // matched against err.Error()
	status    int
	message   string // empty means "use err.Error()"
	code      string
}

// mapServiceError renders the first matching rule, falling back to a 500 with
// fallbackMessage. Details always stay in the server log.
func mapServiceError(c echo.Context, err error, rules []errorRule, fallbackMessage, fallbackCode string) error {
	for _, rule := range rules {
		if !matchesRule(err, rule) {
			continue
		}
		message := rule.message
		if message == "" {
			message = err.Error()
		}
		return handler.ErrorResponse(c, rule.status, message, rule.code, "")
	}
	return handler.ErrorResponse(c, http.StatusInternalServerError, fallbackMessage, fallbackCode, err.Error())
}

// matchesRule reports whether err is the rule's target.
func matchesRule(err error, rule errorRule) bool {
	if rule.sentinel != nil {
		return errors.Is(err, rule.sentinel)
	}
	return rule.substring != "" && strings.Contains(err.Error(), rule.substring)
}
