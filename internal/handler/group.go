package handler

import (
	"context"

	"charon/internal/model"

	"github.com/labstack/echo/v4"
	"go.mau.fi/whatsmeow/types"
)

// Request for sending a group message
type SendGroupMessageRequest struct {
	GroupJID string `json:"groupJid" validate:"required"`
	Message  string `json:"message" validate:"required"`
}

// sendGroupMediaRequest is the JSON body of the two media-url group endpoints.
type sendGroupMediaRequest struct {
	GroupJID  string `json:"groupJid" validate:"required"`
	MediaURL  string `json:"mediaUrl" validate:"required"`
	Caption   string `json:"caption"`
	MediaType string `json:"mediaType"`
}

// parseGroupJID parses a group JID and rejects anything that is not on the
// group server. The second result is a written ErrorResponse for the caller to
// propagate.
func parseGroupJID(c echo.Context, raw string) (types.JID, error) {
	groupJID, err := types.ParseJID(raw)
	if err != nil {
		return types.JID{}, ErrorResponse(c, 400, "Invalid group JID", "INVALID_GROUP_JID", err.Error())
	}
	if groupJID.Server != types.GroupServer {
		return types.JID{}, ErrorResponse(c, 400, "Not a group JID", "NOT_GROUP_JID", "Group JID must end with @g.us")
	}
	return groupJID, nil
}

// listJoinedGroups returns the instance's joined groups in response shape. The
// second result is a written ErrorResponse for the caller to propagate.
func listJoinedGroups(c echo.Context, session *model.Session) ([]map[string]any, error) {
	groups, err := session.Client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil, ErrorResponse(c, 500, "Failed to get groups", "GET_GROUPS_FAILED", err.Error())
	}

	groupList := make([]map[string]any, 0, len(groups))
	for _, groupInfo := range groups {
		groupList = append(groupList, map[string]any{
			"jid":          groupInfo.JID.String(),
			"name":         groupInfo.Name,
			"topic":        groupInfo.Topic,
			"participants": len(groupInfo.Participants),
			"ownerJid":     groupInfo.OwnerJID.String(),
			"createdAt":    groupInfo.GroupCreated.Unix(),
		})
	}
	return groupList, nil
}

// GET /groups/:instanceId - List all groups
func GetGroups(c echo.Context) error {
	instanceID := c.Param("instanceId")

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	groupList, errResp := listJoinedGroups(c, session)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Groups retrieved", map[string]any{
		"total":  len(groupList),
		"groups": groupList,
	})
}

// GET /groups/by-number/:phoneNumber - List all groups
func GetGroupsByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	groupList, errResp := listJoinedGroups(c, session)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Groups retrieved", map[string]any{
		"from":   phoneNumber,
		"total":  len(groupList),
		"groups": groupList,
	})
}

// POST /send-group/:instanceId - Send text to group
func SendGroupMessage(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req SendGroupMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}
	if req.GroupJID == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'message' are required", "VALIDATION_ERROR", "")
	}

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, req.GroupJID)
	if errResp != nil {
		return errResp
	}

	resp, errResp := sendTextMessage(c, session, groupJID, req.Message)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Message sent to group", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
	})
}

// POST /send-group/by-number/:phoneNumber - Send text to group by sender number
func SendGroupMessageByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req SendGroupMessageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}
	if req.GroupJID == "" || req.Message == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'message' are required", "VALIDATION_ERROR", "")
	}

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, req.GroupJID)
	if errResp != nil {
		return errResp
	}

	resp, errResp := sendTextMessage(c, session, groupJID, req.Message)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Message sent to group", map[string]any{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
	})
}

// POST /send-group/:instanceId/media - Send media to group
func SendGroupMedia(c echo.Context) error {
	instanceID := c.Param("instanceId")

	groupJid := c.FormValue("groupJid")
	caption := c.FormValue("caption")

	if groupJid == "" {
		return ErrorResponse(c, 400, "Field 'groupJid' is required", "VALIDATION_ERROR", "")
	}

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, groupJid)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := readUploadedMedia(c)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, groupJID, fileData, mediaType, filename, caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  groupJid,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}

// POST /send-group/by-number/:phoneNumber/media - Send media to group by sender number
func SendGroupMediaByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	groupJid := c.FormValue("groupJid")
	caption := c.FormValue("caption")

	if groupJid == "" {
		return ErrorResponse(c, 400, "Field 'groupJid' is required", "VALIDATION_ERROR", "")
	}

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, groupJid)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := readUploadedMedia(c)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, groupJID, fileData, mediaType, filename, caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]any{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  groupJid,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}

// POST /send-group/:instanceId/media-url - Send media from URL to group
func SendGroupMediaURL(c echo.Context) error {
	instanceID := c.Param("instanceId")

	var req sendGroupMediaRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}
	if req.GroupJID == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	session, errResp := requireConnectedSession(c, instanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, req.GroupJID)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := downloadRemoteMedia(c, req.MediaURL, req.MediaType)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, groupJID, fileData, mediaType, filename, req.Caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]any{
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}

// POST /send-group/by-number/:phoneNumber/media-url - Send media from URL to group by sender number
func SendGroupMediaURLByNumber(c echo.Context) error {
	phoneNumber := c.Param("phoneNumber")

	var req sendGroupMediaRequest
	if err := c.Bind(&req); err != nil {
		return ErrorResponse(c, 400, "Invalid request body", "INVALID_REQUEST", err.Error())
	}
	if req.GroupJID == "" || req.MediaURL == "" {
		return ErrorResponse(c, 400, "Fields 'groupJid' and 'mediaUrl' are required", "VALIDATION_ERROR", "")
	}

	inst, errResp := resolveSenderInstance(c, phoneNumber)
	if errResp != nil {
		return errResp
	}

	session, errResp := requireConnectedSession(c, inst.InstanceID)
	if errResp != nil {
		return errResp
	}

	groupJID, errResp := parseGroupJID(c, req.GroupJID)
	if errResp != nil {
		return errResp
	}

	fileData, mediaType, filename, errResp := downloadRemoteMedia(c, req.MediaURL, req.MediaType)
	if errResp != nil {
		return errResp
	}

	resp, errResp := uploadAndSendMedia(c, session, groupJID, fileData, mediaType, filename, req.Caption)
	if errResp != nil {
		return errResp
	}

	return SuccessResponse(c, 200, "Media sent to group", map[string]any{
		"from":      phoneNumber,
		"messageId": resp.ID,
		"timestamp": resp.Timestamp.Unix(),
		"groupJid":  req.GroupJID,
		"mediaType": mediaType,
		"fileName":  filename,
		"fileSize":  len(fileData),
	})
}
