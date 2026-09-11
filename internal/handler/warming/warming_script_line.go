package warming

import (
	"fmt"
	"net/http"

	"charon/internal/handler"
	warmingModel "charon/internal/model/warming"
	warmingService "charon/internal/service/warming"

	"github.com/labstack/echo/v4"
)

// scriptLineValidationRules are the request-level failures shared by the create
// and update paths.
var scriptLineValidationRules = []errorRule{
	{sentinel: warmingService.ErrScriptLineActorRoleInvalid, status: http.StatusBadRequest, code: "ACTOR_ROLE_INVALID"},
	{sentinel: warmingService.ErrScriptLineMessageContentRequired, status: http.StatusBadRequest, code: "MESSAGE_CONTENT_REQUIRED"},
	{sentinel: warmingService.ErrScriptLineSequenceOrderInvalid, status: http.StatusBadRequest, code: "SEQUENCE_ORDER_INVALID"},
}

// scriptNotFoundRule maps the service's "script not found" wording onto a 404.
var scriptNotFoundRule = errorRule{
	substring: "script not found", status: http.StatusNotFound,
	message: "Script not found", code: "SCRIPT_NOT_FOUND",
}

// duplicateSequenceRule maps a sequence_order collision onto a 409.
var duplicateSequenceRule = errorRule{
	substring: "already exists", status: http.StatusConflict, code: "DUPLICATE_SEQUENCE",
}

// lineNotFoundRule maps a missing line onto a 404.
var lineNotFoundRule = errorRule{
	sentinel: warmingService.ErrScriptLineNotFound, status: http.StatusNotFound,
	message: "Script line not found", code: "NOT_FOUND",
}

// scriptLineResponses converts a slice of lines to their response form.
func scriptLineResponses(lines []warmingModel.WarmingScriptLine) []warmingModel.WarmingScriptLineResponse {
	responses := make([]warmingModel.WarmingScriptLineResponse, 0, len(lines))
	for _, line := range lines {
		responses = append(responses, warmingModel.ToWarmingScriptLineResponse(line))
	}
	return responses
}

// CreateWarmingScriptLine handles POST /warming/scripts/:scriptId/lines
func CreateWarmingScriptLine(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}

	var req warmingModel.CreateWarmingScriptLineRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "You don't have permission to manage lines for this script"); errResp != nil {
		return errResp
	}

	line, err := warmingService.CreateWarmingScriptLineService(scriptID, &req)
	if err != nil {
		rules := append(append([]errorRule{}, scriptLineValidationRules...), scriptNotFoundRule, duplicateSequenceRule)
		return mapServiceError(c, err, rules, "Failed to create script line", "CREATE_FAILED")
	}

	resp := warmingModel.ToWarmingScriptLineResponse(*line)
	return handler.SuccessResponse(c, http.StatusOK, "Script line created successfully", resp)
}

// GetAllWarmingScriptLines handles GET /warming/scripts/:scriptId/lines
func GetAllWarmingScriptLines(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	// Reading also allows public scripts, so this is not the ownership check.
	if errResp := requireScriptReadAccess(c, scriptID, cl, "You don't have permission to view lines for this script"); errResp != nil {
		return errResp
	}

	lines, err := warmingService.GetAllWarmingScriptLinesService(scriptID)
	if err != nil {
		return mapServiceError(c, err, []errorRule{scriptNotFoundRule}, "Failed to get script lines", "GET_FAILED")
	}

	responses := scriptLineResponses(lines)
	return handler.SuccessResponse(c, http.StatusOK, "Script lines retrieved successfully", map[string]any{
		"total": len(responses),
		"lines": responses,
	})
}

// GetWarmingScriptLineByID handles GET /warming/scripts/:scriptId/lines/:id
func GetWarmingScriptLineByID(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}
	lineID, errResp := pathInt64(c, "id", "line ID", "INVALID_LINE_ID")
	if errResp != nil {
		return errResp
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "Access denied"); errResp != nil {
		return errResp
	}

	line, err := warmingService.GetWarmingScriptLineByIDService(scriptID, lineID)
	if err != nil {
		return mapServiceError(c, err, []errorRule{lineNotFoundRule}, "Failed to get script line", "GET_FAILED")
	}

	resp := warmingModel.ToWarmingScriptLineResponse(*line)
	return handler.SuccessResponse(c, http.StatusOK, "Script line retrieved successfully", resp)
}

// UpdateWarmingScriptLine handles PUT /warming/scripts/:scriptId/lines/:id
func UpdateWarmingScriptLine(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}
	lineID, errResp := pathInt64(c, "id", "line ID", "INVALID_LINE_ID")
	if errResp != nil {
		return errResp
	}

	var req warmingModel.UpdateWarmingScriptLineRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "You don't have permission to update this line"); errResp != nil {
		return errResp
	}

	if err := warmingService.UpdateWarmingScriptLineService(scriptID, lineID, &req); err != nil {
		rules := append(append([]errorRule{}, scriptLineValidationRules...), lineNotFoundRule, duplicateSequenceRule)
		return mapServiceError(c, err, rules, "Failed to update script line", "UPDATE_FAILED")
	}

	return handler.SuccessResponse(c, http.StatusOK, "Line updated successfully", map[string]any{
		"id": lineID,
	})
}

// DeleteWarmingScriptLine handles DELETE /warming/scripts/:scriptId/lines/:id
func DeleteWarmingScriptLine(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}
	lineID, errResp := pathInt64(c, "id", "line ID", "INVALID_LINE_ID")
	if errResp != nil {
		return errResp
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "You don't have permission to delete this line"); errResp != nil {
		return errResp
	}

	if err := warmingService.DeleteWarmingScriptLineService(scriptID, lineID); err != nil {
		return mapServiceError(c, err, []errorRule{lineNotFoundRule}, "Failed to delete script line", "DELETE_FAILED")
	}

	return handler.SuccessResponse(c, http.StatusOK, "Line deleted successfully", map[string]any{
		"id": lineID,
	})
}

// GenerateWarmingScriptLines handles POST /warming/scripts/:scriptId/lines/generate
func GenerateWarmingScriptLines(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}

	var req struct {
		LineCount int    `json:"lineCount"`
		Category  string `json:"category"`
	}
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}
	if req.Category == "" {
		req.Category = "casual"
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "You don't have permission to modify this script"); errResp != nil {
		return errResp
	}

	// Validate category by checking if templates exist in database
	if _, err := warmingService.GetConversationTemplatesFromDB(req.Category); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("No templates found for category '%s'. Please create templates first or use existing categories.", req.Category),
			"INVALID_CATEGORY", "")
	}

	lines, err := warmingService.GenerateWarmingScriptLinesService(scriptID, req.Category, req.LineCount)
	if err != nil {
		return mapServiceError(c, err, []errorRule{
			{sentinel: warmingService.ErrTemplateNotFound, status: http.StatusBadRequest,
				message: fmt.Sprintf("No templates found for category '%s'", req.Category), code: "INVALID_CATEGORY"},
			scriptNotFoundRule,
			{substring: "line_count", status: http.StatusBadRequest, code: "INVALID_LINE_COUNT"},
		}, "Failed to generate script lines", "GENERATE_FAILED")
	}

	responses := scriptLineResponses(lines)
	return handler.SuccessResponse(c, http.StatusOK,
		fmt.Sprintf("%d script lines generated successfully", len(responses)), map[string]any{
			"created":  len(responses),
			"category": req.Category,
			"lines":    responses,
		})
}

// ReorderWarmingScriptLines handles PUT /warming/scripts/:scriptId/lines/reorder
func ReorderWarmingScriptLines(c echo.Context) error {
	scriptID, errResp := pathInt64(c, "scriptId", "script ID", "INVALID_SCRIPT_ID")
	if errResp != nil {
		return errResp
	}

	var req warmingModel.ReorderScriptLinesRequest
	if err := c.Bind(&req); err != nil {
		return handler.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST", err.Error())
	}
	if len(req.Lines) == 0 {
		return handler.ErrorResponse(c, http.StatusBadRequest, "No lines provided for reordering", "EMPTY_LINES", "")
	}

	cl, errResp := requireCaller(c)
	if errResp != nil {
		return errResp
	}
	if errResp := requireScriptOwner(c, scriptID, cl, "You don't have permission to reorder lines for this script"); errResp != nil {
		return errResp
	}

	if err := warmingModel.ReorderScriptLines(scriptID, &req); err != nil {
		return mapServiceError(c, err, []errorRule{
			{substring: "not found", status: http.StatusNotFound, code: "NOT_FOUND"},
		}, "Failed to reorder script lines", "REORDER_FAILED")
	}

	return handler.SuccessResponse(c, http.StatusOK, "Script lines reordered successfully", map[string]any{
		"updated": len(req.Lines),
	})
}
