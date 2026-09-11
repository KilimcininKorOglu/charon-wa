package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"charon/internal/helper"
	"charon/internal/model"
	"charon/internal/service"
	"charon/internal/ws"

	"github.com/labstack/echo/v4"
)

// Store cancel functions for each instance
var qrCancelFuncs = make(map[string]context.CancelFunc)
var qrCancelMutex sync.RWMutex

//**********************************
//
// WHATSAPP INSTANCE AUTHENTICATION
//
//**********************************
//SECTION LOGIN WHATSAPP
//
//**********************************

// Generate random instance ID
func generateInstanceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// POST /login
func Login(c echo.Context) error {
	instanceID := generateInstanceID()

	// payload input
	var req struct {
		Circle string `json:"circle"`
	}
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "BAD_REQUEST", err.Error())
	}
	if strings.TrimSpace(req.Circle) == "" {
		return ErrorResponse(c, 400, "Field 'circle' is required", "CIRCLE_REQUIRED", "")
	}

	// Get current user from context (set by session middleware)
	userClaims, _ := c.Get("user_claims").(*service.Claims)

	// Only admin and user roles may create instances
	if userClaims != nil && userClaims.Role == "viewer" {
		return ErrorResponse(c, 403, "Viewers cannot create instances", "FORBIDDEN", "")
	}

	session, err := service.CreateSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 400, "Failed to create session", "CREATE_SESSION_FAILED", err.Error())
	}

	// Check if already logged in before
	if session.Client.Store.ID != nil {
		err = session.Client.Connect()
		if err != nil {
			return ErrorResponse(c, 500, "Failed to connect", "CONNECT_FAILED", err.Error())
		}

		session.IsConnected = true
		return SuccessResponse(c, 200, "Session reconnected successfully", map[string]any{
			"instanceId": instanceID,
			"status":     "connected",
			"jid":        session.Client.Store.ID.String(),
		})
	}

	var createdBy sql.NullInt64
	if userClaims != nil {
		createdBy = sql.NullInt64{Int64: userClaims.UserID, Valid: true}
	}

	instance := &model.Instance{
		InstanceID:  instanceID,
		Status:      "qr_required",
		IsConnected: false,
		CreatedAt:   time.Now(),
		Circle:      req.Circle,
		CreatedBy:   createdBy,
	}

	if errResp := persistNewInstance(c, instance, userClaims); errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Instance created, QR code required", map[string]any{
		"instanceId": instanceID,
		"status":     "qr_required",
		"nextStep":   "Call GET /qr/:instanceId to get QR code",
	})
}

// persistNewInstance stores a freshly created instance and grants its creator
// access. A non-admin goes through the atomic path, which checks the per-user
// cap in the same transaction so concurrent requests cannot bypass it.
func persistNewInstance(c echo.Context, instance *model.Instance, userClaims *service.Claims) error {
	if userClaims != nil && userClaims.Role != "admin" {
		maxInstances := helper.GetEnvAsInt("MAX_INSTANCES_PER_USER", 10)
		err := model.CreateInstanceAtomic(instance, userClaims.UserID, maxInstances)
		if errors.Is(err, model.ErrInstanceLimitReached) {
			return ErrorResponse(c, 429, "Instance creation limit reached", "INSTANCE_LIMIT",
				fmt.Sprintf("Maximum %d instances per user", maxInstances))
		}
		if err != nil {
			return ErrorResponse(c, 500, "Failed to insert instance", "DB_INSERT_FAILED", err.Error())
		}
		return nil
	}

	// Admin has no instance cap — use the plain insert path.
	if err := model.InsertInstance(instance); err != nil {
		return ErrorResponse(c, 500, "Failed to insert instance", "DB_INSERT_FAILED", err.Error())
	}
	if instance.CreatedBy.Valid {
		if err := model.AssignInstanceToUser(instance.CreatedBy.Int64, instance.InstanceID, "access"); err != nil {
			log.Printf("⚠️ Warning: Failed to assign access for instance %s to user %d: %v",
				instance.InstanceID, instance.CreatedBy.Int64, err)
		}
	}
	return nil
}

// GET /qr/:instanceId
func GetQR(c echo.Context) error {

	instanceID := c.Param("instanceId")

	// Check if QR generation is already in progress
	qrCancelMutex.RLock()
	_, exists := qrCancelFuncs[instanceID]
	qrCancelMutex.RUnlock()

	if exists {
		return ErrorResponse(c, 409, "QR generation already in progress, please wait", "QR_IN_PROGRESS", "Please wait or cancel the current QR generation first.")
	}

	// Check session in memory
	session, err := service.GetSession(instanceID)
	// If session doesn't exist (e.g. after logout), create a new session
	if err != nil || session == nil {
		fmt.Println("⚠ Session not found in memory, creating new session for instance:", instanceID)
		// CREATE new session with the SAME instance ID
		session, err = service.CreateSession(instanceID)
		if err != nil {
			return ErrorResponse(c, 500, "Failed to create session", "CREATE_SESSION_FAILED", err.Error())
		}
		fmt.Println("✓ New session created for existing instance:", instanceID)
	}

	if session.IsConnected {
		return SuccessResponse(c, 200, "Already connected", map[string]any{
			"status": "already_connected",
			"jid":    session.Client.Store.ID.String(),
		})
	}

	// Create context with 3-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)

	// Store cancel function
	qrCancelMutex.Lock()
	qrCancelFuncs[instanceID] = cancel
	qrCancelMutex.Unlock()

	go runQRGeneration(ctx, cancel, session, instanceID)

	// Return response immediately without waiting for QR generation to complete
	return SuccessResponse(c, 200, "QR generation started", map[string]any{
		"status":      "generating",
		"message":     "QR codes will be sent via WebSocket. Listen to QR_GENERATED event.",
		"instance_id": instanceID,
		"timeout":     "3 minutes",
	})
}

// publishWSEvent broadcasts one event when a realtime publisher is configured.
func publishWSEvent(event string, data any) {
	if service.Realtime == nil {
		return
	}
	service.Realtime.Publish(ws.WsEvent{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

// runQRGeneration drives one QR pairing attempt to completion and reports every
// step over the WebSocket. It always releases the instance's cancel slot.
func runQRGeneration(ctx context.Context, cancel context.CancelFunc, session *model.Session, instanceID string) {
	defer func() {
		qrCancelMutex.Lock()
		delete(qrCancelFuncs, instanceID)
		qrCancelMutex.Unlock()
		cancel()
	}()

	qrChan, err := session.Client.GetQRChannel(ctx)
	if err != nil {
		log.Printf("Failed to get QR channel for instance %s: %v", instanceID, err)
		// Broadcast sanitized error — raw whatsmeow detail stays in the server log.
		publishWSEvent(ws.EventInstanceError, map[string]any{
			"instance_id": instanceID,
			"code":        "qr_channel_failed",
			"error":       "Could not start QR session. Please try again.",
		})
		return
	}

	if err := session.Client.Connect(); err != nil {
		log.Printf("Failed to connect client for instance %s: %v", instanceID, err)
		publishWSEvent(ws.EventInstanceError, map[string]any{
			"instance_id": instanceID,
			"code":        "connect_failed",
			"error":       "Could not connect the instance. Please try again.",
		})
		return
	}

	for evt := range qrChan {
		select {
		case <-ctx.Done():
			println("\n✗ QR Generation cancelled or timeout for instance:", instanceID)
			publishWSEvent(ws.EventQRTimeout, map[string]any{
				"instance_id": instanceID,
				"status":      "cancelled",
				"reason":      ctx.Err().Error(),
			})
			return
		default:
		}

		if done := handleQREvent(instanceID, evt.Event, evt.Code); done {
			return
		}
	}

	// Channel closed unexpectedly
	println("\n✗ QR channel closed for instance:", instanceID)
	publishWSEvent(ws.EventInstanceError, map[string]any{
		"instance_id": instanceID,
		"error":       "QR channel closed unexpectedly",
	})
}

// handleQREvent processes one QR channel event and reports whether the pairing
// attempt is finished.
func handleQREvent(instanceID, event, code string) bool {
	switch {
	case event == "code":
		publishQRCode(instanceID, code)
		return false

	case event == "success":
		println("\n✓ QR Scanned! Pairing successful for instance:", instanceID)
		publishWSEvent(ws.EventQRSuccess, map[string]any{
			"instance_id": instanceID,
			"status":      "connected",
		})
		return true

	case event == "timeout":
		println("\n✗ QR Timeout for instance:", instanceID)
		publishWSEvent(ws.EventQRTimeout, map[string]any{
			"instance_id": instanceID,
			"status":      "timeout",
		})
		return true

	case strings.HasPrefix(event, "err-"):
		println("\n✗ QR Error for instance:", instanceID, "->", event)
		publishWSEvent(ws.EventInstanceError, map[string]any{
			"instance_id": instanceID,
			"error":       event,
		})
		return true

	default:
		return false
	}
}

// publishQRCode stores a freshly issued QR code and broadcasts it. The code is
// valid for 60 seconds, after which whatsmeow issues a replacement.
func publishQRCode(instanceID, code string) {
	println("\n=== QR Code String ===")
	println(code)
	println("Instance ID:", instanceID)

	expiresAt := time.Now().Add(60 * time.Second)
	if err := model.UpdateInstanceQR(instanceID, code, expiresAt); err != nil {
		log.Printf("Failed to update QR info in database for instance %s: %v", instanceID, err)
	}

	publishWSEvent(ws.EventQRGenerated, ws.QRGeneratedData{
		InstanceID:  instanceID,
		PhoneNumber: "",
		QRData:      code,
		ExpiresAt:   expiresAt,
	})

	println("QR sent via WebSocket. Waiting for scan or next QR refresh...")
}

// DELETE /qr/:instanceId - Cancel QR generation
func CancelQR(c echo.Context) error {
	instanceID := c.Param("instanceId")

	qrCancelMutex.RLock()
	cancel, exists := qrCancelFuncs[instanceID]
	qrCancelMutex.RUnlock()

	if !exists {
		return ErrorResponse(c, 404, "No active QR generation", "NO_QR_SESSION", "No QR generation in progress for this instance.")
	}

	println("\n✗ User cancelled QR generation for instance:", instanceID)
	// Cancel QR generation
	cancel()

	// Broadcast cancel event via WebSocket
	if service.Realtime != nil {
		cancelEvt := ws.WsEvent{
			Event:     ws.EventQRCancelled,
			Timestamp: time.Now().UTC(),
			Data: map[string]any{
				"instance_id": instanceID,
				"status":      "cancelled",
				"message":     "User cancelled QR generation",
			},
		}
		service.Realtime.Publish(cancelEvt)
	}

	return SuccessResponse(c, 200, "QR generation cancelled successfully", map[string]any{
		"instance_id": instanceID,
		"status":      "cancelled",
	})
}

// GET /status/:instanceId
func GetStatus(c echo.Context) error {
	instanceID := c.Param("instanceId")

	session, err := service.GetSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", "")
	}

	return SuccessResponse(c, 200, "Status retrieved", map[string]any{
		"instanceId":  instanceID,
		"isConnected": session.IsConnected,
		"jid":         session.JID,
	})
}

// GET /instances?all=true&page=1&limit=100
func GetAllInstances(c echo.Context) error {

	showAll := c.QueryParam("all") == "true"
	limit, offset := instanceListPaging(c)

	// Get current user claims
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	isAdmin := userClaims != nil && userClaims.Role == "admin"

	// Admin sees full session_data; regular users never pull the BYTEA blob.
	dbInstances, err := model.GetAllInstances(limit, offset, isAdmin)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to get instances", "DB_QUERY_FAILED", err.Error())
	}

	allowedInstances := allowedInstanceFilter(userClaims, isAdmin)
	sessions := service.GetAllSessions()
	var instances []model.InstanceResp

	for _, inst := range dbInstances {
		if allowedInstances != nil && !allowedInstances[inst.InstanceID] {
			continue
		}

		resp := instanceRespWithSession(inst, sessions)
		if !showAll && !resp.IsConnected {
			continue
		}
		instances = append(instances, resp)
	}

	return SuccessResponse(c, http.StatusOK, "Instances retrieved", map[string]any{
		"total":     len(instances),
		"instances": instances,
	})
}

// allowedInstanceFilter returns the set of instance IDs the caller may see, or
// nil for "no filter" — an admin sees every instance.
func allowedInstanceFilter(userClaims *service.Claims, isAdmin bool) map[string]bool {
	if isAdmin || userClaims == nil {
		return nil
	}

	allowedIDs, _ := model.GetUserInstances(userClaims.UserID)
	allowed := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}
	return allowed
}

// instanceListPaging reads the page and limit query params for the instance
// list. The limit is clamped to 1..500 and defaults to 100.
func instanceListPaging(c echo.Context) (limit, offset int) {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	limit, _ = strconv.Atoi(c.QueryParam("limit"))

	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	page = max(page, 1)

	return limit, (page - 1) * limit
}

// instanceRespWithSession converts a stored instance to its response form and
// overlays the live state of its in-memory session, when one exists.
func instanceRespWithSession(inst model.Instance, sessions map[string]*model.Session) model.InstanceResp {
	resp := model.ToResponse(inst)

	session, found := sessions[inst.InstanceID]
	if found {
		resp.IsConnected = session.IsConnected
		resp.JID = session.JID
		if resp.IsConnected {
			resp.Status = "online"
		}
	}
	resp.ExistsInWhatsmeow = found

	return resp
}

// POST /logout/:instanceId
func Logout(c echo.Context) error {
	instanceID := c.Param("instanceId")

	err := service.DeleteSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", err.Error())
	}

	return SuccessResponse(c, 200, "Logged out successfully", map[string]any{
		"instanceId": instanceID,
	})
}

// DELETE /instances/:instanceId
func DeleteInstance(c echo.Context) error {
	instanceID := c.Param("instanceId")

	err := service.DeleteInstance(instanceID)
	if err != nil {
		// Instance not found
		if errors.Is(err, service.ErrInstanceNotFound) {
			return ErrorResponse(c, 404,
				"Instance not found",
				"INSTANCE_NOT_FOUND",
				err.Error(),
			)
		}

		// Instance still connected / not yet logged out
		if errors.Is(err, service.ErrInstanceStillConnected) {
			return ErrorResponse(c, 400,
				"Instance is still connected. Please logout first.",
				"INSTANCE_STILL_CONNECTED",
				err.Error(),
			)
		}

		// Other error (DB / internal)
		return ErrorResponse(c, 500,
			"Failed to delete instance",
			"DELETE_INSTANCE_FAILED",
			err.Error(),
		)
	}

	return SuccessResponse(c, 200, "Instance deleted successfully", map[string]any{
		"instanceId": instanceID,
	})
}

// PATCH /instances/:instanceId
func UpdateInstanceFields(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req model.UpdateInstanceFieldsRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	// Validate at least one field is provided
	if req.Used == nil && req.Description == nil && req.Circle == nil {
		return ErrorResponse(c, http.StatusBadRequest, "At least one field (used, description, or circle) must be provided", "NO_FIELDS", "")
	}

	err := model.UpdateInstanceFields(instanceID, &req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResponse(c, http.StatusNotFound, "Instance not found", "INSTANCE_NOT_FOUND", "")
		}
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to update instance", "UPDATE_FAILED", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Instance updated successfully", map[string]any{
		"instanceId": instanceID,
	})
}
