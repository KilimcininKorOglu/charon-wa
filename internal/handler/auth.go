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

	// Get current user from context (set by JWT middleware)
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
		return SuccessResponse(c, 200, "Session reconnected successfully", map[string]interface{}{
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

	// For non-admin users, atomically check the limit and create the instance.
	// This prevents concurrent requests from bypassing the per-user instance cap.
	if userClaims != nil && userClaims.Role != "admin" {
		maxInstances := helper.GetEnvAsInt("MAX_INSTANCES_PER_USER", 10)
		if err = model.CreateInstanceAtomic(instance, userClaims.UserID, maxInstances); err != nil {
			if err == model.ErrInstanceLimitReached {
				return ErrorResponse(c, 429, "Instance creation limit reached", "INSTANCE_LIMIT", fmt.Sprintf("Maximum %d instances per user", maxInstances))
			}
			return ErrorResponse(c, 500, "Failed to insert instance", "DB_INSERT_FAILED", err.Error())
		}
	} else {
		// Admin has no instance cap — use the plain insert path.
		if err = model.InsertInstance(instance); err != nil {
			return ErrorResponse(c, 500, "Failed to insert instance", "DB_INSERT_FAILED", err.Error())
		}
		if createdBy.Valid {
			if err = model.AssignInstanceToUser(createdBy.Int64, instanceID, "access"); err != nil {
				log.Printf("⚠️ Warning: Failed to assign access for instance %s to user %d: %v", instanceID, createdBy.Int64, err)
			}
		}
	}

	return SuccessResponse(c, 200, "Instance created, QR code required", map[string]interface{}{
		"instanceId": instanceID,
		"status":     "qr_required",
		"nextStep":   "Call GET /qr/:instanceId to get QR code",
	})
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
		return SuccessResponse(c, 200, "Already connected", map[string]interface{}{
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

	// Run QR generation in goroutine (background process)
	go func() {
		// Cleanup after completion
		defer func() {
			qrCancelMutex.Lock()
			delete(qrCancelFuncs, instanceID)
			qrCancelMutex.Unlock()
			cancel()
		}()

		// Get QR channel with context
		qrChan, err := session.Client.GetQRChannel(ctx)
		if err != nil {
			log.Printf("Failed to get QR channel for instance %s: %v", instanceID, err)

			// Broadcast sanitized error — raw whatsmeow detail stays in the server log.
			if service.Realtime != nil {
				errorEvt := ws.WsEvent{
					Event:     ws.EventInstanceError,
					Timestamp: time.Now().UTC(),
					Data: map[string]interface{}{
						"instance_id": instanceID,
						"code":        "qr_channel_failed",
						"error":       "Could not start QR session. Please try again.",
					},
				}
				service.Realtime.Publish(errorEvt)
			}
			return
		}

		// Connect client
		err = session.Client.Connect()
		if err != nil {
			log.Printf("Failed to connect client for instance %s: %v", instanceID, err)

			if service.Realtime != nil {
				errorEvt := ws.WsEvent{
					Event:     ws.EventInstanceError,
					Timestamp: time.Now().UTC(),
					Data: map[string]interface{}{
						"instance_id": instanceID,
						"code":        "connect_failed",
						"error":       "Could not connect the instance. Please try again.",
					},
				}
				service.Realtime.Publish(errorEvt)
			}
			return
		}

		// Listen to QR events
		for evt := range qrChan {
			// Check if context was cancelled or timed out
			select {
			case <-ctx.Done():
				println("\n✗ QR Generation cancelled or timeout for instance:", instanceID)

				// Broadcast cancel/timeout event
				if service.Realtime != nil {
					cancelEvt := ws.WsEvent{
						Event:     ws.EventQRTimeout,
						Timestamp: time.Now().UTC(),
						Data: map[string]interface{}{
							"instance_id": instanceID,
							"status":      "cancelled",
							"reason":      ctx.Err().Error(),
						},
					}
					service.Realtime.Publish(cancelEvt)
				}
				return

			default:
				// Continue handling events
			}

			if evt.Event == "code" {
				// Print QR string for debugging
				println("\n=== QR Code String ===")
				println(evt.Code)
				println("Instance ID:", instanceID)

				// Save QR to custom DB
				expiresAt := time.Now().Add(60 * time.Second)
				err := model.UpdateInstanceQR(instanceID, evt.Code, expiresAt)
				if err != nil {
					log.Printf("Failed to update QR info in database for instance %s: %v", instanceID, err)
				}

				// Broadcast QR via WebSocket
				if service.Realtime != nil {
					data := ws.QRGeneratedData{
						InstanceID:  instanceID,
						PhoneNumber: "",
						QRData:      evt.Code,
						ExpiresAt:   expiresAt,
					}

					evtWs := ws.WsEvent{
						Event:     ws.EventQRGenerated,
						Timestamp: time.Now().UTC(),
						Data:      data,
					}
					service.Realtime.Publish(evtWs)
				}

				println("QR sent via WebSocket. Waiting for scan or next QR refresh...")

			} else if evt.Event == "success" {
				println("\n✓ QR Scanned! Pairing successful for instance:", instanceID)

				// Broadcast success via WebSocket
				if service.Realtime != nil {
					successEvt := ws.WsEvent{
						Event:     ws.EventQRSuccess,
						Timestamp: time.Now().UTC(),
						Data: map[string]interface{}{
							"instance_id": instanceID,
							"status":      "connected",
						},
					}
					service.Realtime.Publish(successEvt)
				}
				return

			} else if evt.Event == "timeout" {
				println("\n✗ QR Timeout for instance:", instanceID)

				if service.Realtime != nil {
					timeoutEvt := ws.WsEvent{
						Event:     ws.EventQRTimeout,
						Timestamp: time.Now().UTC(),
						Data: map[string]interface{}{
							"instance_id": instanceID,
							"status":      "timeout",
						},
					}
					service.Realtime.Publish(timeoutEvt)
				}
				return

			} else if strings.HasPrefix(evt.Event, "err-") {
				println("\n✗ QR Error for instance:", instanceID, "->", evt.Event)

				if service.Realtime != nil {
					errorEvt := ws.WsEvent{
						Event:     ws.EventInstanceError,
						Timestamp: time.Now().UTC(),
						Data: map[string]interface{}{
							"instance_id": instanceID,
							"error":       evt.Event,
						},
					}
					service.Realtime.Publish(errorEvt)
				}
				return
			}
		}

		// Channel closed unexpectedly
		println("\n✗ QR channel closed for instance:", instanceID)

		if service.Realtime != nil {
			errorEvt := ws.WsEvent{
				Event:     ws.EventInstanceError,
				Timestamp: time.Now().UTC(),
				Data: map[string]interface{}{
					"instance_id": instanceID,
					"error":       "QR channel closed unexpectedly",
				},
			}
			service.Realtime.Publish(errorEvt)
		}
	}()

	// Return response immediately without waiting for QR generation to complete
	return SuccessResponse(c, 200, "QR generation started", map[string]interface{}{
		"status":      "generating",
		"message":     "QR codes will be sent via WebSocket. Listen to QR_GENERATED event.",
		"instance_id": instanceID,
		"timeout":     "3 minutes",
	})
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
			Data: map[string]interface{}{
				"instance_id": instanceID,
				"status":      "cancelled",
				"message":     "User cancelled QR generation",
			},
		}
		service.Realtime.Publish(cancelEvt)
	}

	return SuccessResponse(c, 200, "QR generation cancelled successfully", map[string]interface{}{
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

	return SuccessResponse(c, 200, "Status retrieved", map[string]interface{}{
		"instanceId":  instanceID,
		"isConnected": session.IsConnected,
		"jid":         session.JID,
	})
}

// GET /instances?all=true&page=1&limit=100
func GetAllInstances(c echo.Context) error {

	showAll := c.QueryParam("all") == "true"

	// Optional pagination (applies to admin path; non-admin is already filtered later).
	page, _ := strconv.Atoi(c.QueryParam("page"))
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	// Get current user claims
	userClaims, _ := c.Get("user_claims").(*service.Claims)
	isAdmin := userClaims != nil && userClaims.Role == "admin"

	// Admin sees full session_data; regular users never pull the BYTEA blob.
	dbInstances, err := model.GetAllInstances(limit, offset, isAdmin)
	if err != nil {
		return ErrorResponse(c, http.StatusInternalServerError, "Failed to get instances", "DB_QUERY_FAILED", err.Error())
	}

	// Get all sessions from memory (active sessions)
	sessions := service.GetAllSessions()
	var instances []model.InstanceResp

	// Create a map for quick permission check if not admin
	var allowedInstances map[string]bool
	if userClaims != nil && userClaims.Role != "admin" {
		allowedIDs, _ := model.GetUserInstances(userClaims.UserID)
		allowedInstances = make(map[string]bool)
		for _, id := range allowedIDs {
			allowedInstances[id] = true
		}
	}

	for _, inst := range dbInstances {
		// Filter by permission if not admin
		if allowedInstances != nil {
			if !allowedInstances[inst.InstanceID] {
				continue
			}
		}

		// Convert from model.Instance to model.InstanceResp (primitive strings)
		resp := model.ToResponse(inst)
		// Check if there's an active session for this instance
		session, found := sessions[inst.InstanceID]

		if found {
			resp.IsConnected = session.IsConnected
			resp.JID = session.JID

			if resp.IsConnected {
				resp.Status = "online"
			}
		}

		// Add info whether session exists in Whatsmeow memory
		resp.ExistsInWhatsmeow = found

		// Filter logic
		if !showAll && !resp.IsConnected {
			continue
		}
		instances = append(instances, resp)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Instances retrieved",
		"data": map[string]interface{}{
			"total":     len(instances),
			"instances": instances,
		},
	})
}

// POST /logout/:instanceId
func Logout(c echo.Context) error {
	instanceID := c.Param("instanceId")

	err := service.DeleteSession(instanceID)
	if err != nil {
		return ErrorResponse(c, 404, "Session not found", "SESSION_NOT_FOUND", err.Error())
	}

	return SuccessResponse(c, 200, "Logged out successfully", map[string]interface{}{
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

	return SuccessResponse(c, 200, "Instance deleted successfully", map[string]interface{}{
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

	return SuccessResponse(c, http.StatusOK, "Instance updated successfully", map[string]interface{}{
		"instanceId": instanceID,
	})
}
