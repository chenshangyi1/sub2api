package admin

import (
	"errors"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GroupMonitorHandler 分组级渠道监控管理后台 handler。
type GroupMonitorHandler struct {
	svc *service.GroupMonitorService
}

// NewGroupMonitorHandler 创建 handler。
func NewGroupMonitorHandler(svc *service.GroupMonitorService) *GroupMonitorHandler {
	return &GroupMonitorHandler{svc: svc}
}

// --- Request / Response ---

type groupMonitorCreateRequest struct {
	GroupID         int64  `json:"group_id" binding:"required"`
	Enabled         *bool  `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes" binding:"omitempty,min=5,max=1440"`
	ModelID         string `json:"model_id" binding:"max=100"`
	AutoRecover     *bool  `json:"auto_recover"`
	MaxOutputTokens int    `json:"max_output_tokens" binding:"omitempty,min=1,max=256"`
}

type groupMonitorUpdateRequest struct {
	Enabled         *bool  `json:"enabled"`
	IntervalMinutes *int   `json:"interval_minutes" binding:"omitempty,min=5,max=1440"`
	ModelID         string `json:"model_id" binding:"max=100"`
	AutoRecover     *bool  `json:"auto_recover"`
	MaxOutputTokens *int   `json:"max_output_tokens" binding:"omitempty,min=1,max=256"`
}

type groupMonitorResponse struct {
	ID              int64   `json:"id"`
	GroupID         int64   `json:"group_id"`
	GroupName       string  `json:"group_name"`
	Enabled         bool    `json:"enabled"`
	IntervalMinutes int     `json:"interval_minutes"`
	ModelID         string  `json:"model_id"`
	AutoRecover     bool    `json:"auto_recover"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	LastRunAt       *string `json:"last_run_at"`
	NextRunAt       string  `json:"next_run_at"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	AccountCount    int     `json:"account_count"`
	HealthyCount    int     `json:"healthy_count"`
	FailedCount     int     `json:"failed_count"`
	UnknownCount    int     `json:"unknown_count"`
}

func groupMonitorToResponse(m *service.GroupMonitor) *groupMonitorResponse {
	if m == nil {
		return nil
	}
	resp := &groupMonitorResponse{
		ID:              m.ID,
		GroupID:         m.GroupID,
		GroupName:       m.GroupName,
		Enabled:         m.Enabled,
		IntervalMinutes: m.IntervalMinutes,
		ModelID:         m.ModelID,
		AutoRecover:     m.AutoRecover,
		MaxOutputTokens: m.MaxOutputTokens,
		AccountCount:    m.AccountCount,
		HealthyCount:    m.HealthyCount,
		FailedCount:     m.FailedCount,
		UnknownCount:    m.UnknownCount,
		CreatedAt:       m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       m.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if m.LastRunAt != nil {
		s := m.LastRunAt.UTC().Format(time.RFC3339)
		resp.LastRunAt = &s
	}
	if m.NextRunAt != nil {
		resp.NextRunAt = m.NextRunAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func parseGroupMonitorID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_GROUP_MONITOR_ID", "invalid group monitor id"))
		return 0, false
	}
	return id, true
}

// --- Handlers ---

// List GET /api/v1/admin/group-monitors
func (h *GroupMonitorHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	params := service.GroupMonitorListParams{
		Page:     page,
		PageSize: pageSize,
		Enabled:  parseListEnabled(c.Query("enabled")),
		Search:   strings.TrimSpace(c.Query("search")),
	}

	items, total, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]*groupMonitorResponse, 0, len(items))
	for _, m := range items {
		out = append(out, groupMonitorToResponse(m))
	}
	response.Paginated(c, out, total, page, pageSize)
}

// Create POST /api/v1/admin/group-monitors
func (h *GroupMonitorHandler) Create(c *gin.Context) {
	var req groupMonitorCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", err.Error()))
		return
	}

	m := &service.GroupMonitor{
		GroupID:         req.GroupID,
		Enabled:         true,
		IntervalMinutes: req.IntervalMinutes,
		ModelID:         req.ModelID,
		AutoRecover:     false,
		MaxOutputTokens: req.MaxOutputTokens,
	}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	if req.AutoRecover != nil {
		m.AutoRecover = *req.AutoRecover
	}

	created, err := h.svc.Create(c.Request.Context(), m)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, groupMonitorToResponse(created))
}

type groupMonitorBatchCreateRequest struct {
	GroupIDs        []int64 `json:"group_ids" binding:"required,min=1"`
	Enabled         *bool   `json:"enabled"`
	IntervalMinutes int     `json:"interval_minutes" binding:"omitempty,min=5,max=1440"`
	ModelID         string  `json:"model_id" binding:"max=100"`
	AutoRecover     *bool   `json:"auto_recover"`
	MaxOutputTokens int     `json:"max_output_tokens" binding:"omitempty,min=1,max=256"`
}

type groupMonitorBatchCreateResult struct {
	Created []int64 `json:"created"`
	Skipped int     `json:"skipped"`
}

// BatchCreate POST /api/v1/admin/group-monitors/batch
// 批量创建：一次为多个分组创建监控；已存在的分组跳过（不报错）。
func (h *GroupMonitorHandler) BatchCreate(c *gin.Context) {
	var req groupMonitorBatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", err.Error()))
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	autoRecover := false
	if req.AutoRecover != nil {
		autoRecover = *req.AutoRecover
	}

	result := groupMonitorBatchCreateResult{Created: []int64{}, Skipped: 0}
	for _, gid := range req.GroupIDs {
		m := &service.GroupMonitor{
			GroupID:         gid,
			Enabled:         enabled,
			IntervalMinutes: req.IntervalMinutes,
			ModelID:         req.ModelID,
			AutoRecover:     autoRecover,
			MaxOutputTokens: req.MaxOutputTokens,
		}
		created, err := h.svc.Create(c.Request.Context(), m)
		if err != nil {
			if errors.Is(err, service.ErrGroupMonitorDuplicateGroup) {
				result.Skipped++
				continue
			}
			response.ErrorFrom(c, err)
			return
		}
		result.Created = append(result.Created, created.ID)
	}
	response.Success(c, result)
}

// Update PUT /api/v1/admin/group-monitors/:id
func (h *GroupMonitorHandler) Update(c *gin.Context) {
	id, ok := parseGroupMonitorID(c)
	if !ok {
		return
	}

	var req groupMonitorUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_REQUEST", err.Error()))
		return
	}

	existing, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	m := &service.GroupMonitor{
		Enabled:         existing.Enabled,
		IntervalMinutes: existing.IntervalMinutes,
		ModelID:         existing.ModelID,
		AutoRecover:     existing.AutoRecover,
		MaxOutputTokens: existing.MaxOutputTokens,
	}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	if req.IntervalMinutes != nil {
		m.IntervalMinutes = *req.IntervalMinutes
	}
	if req.ModelID != "" {
		m.ModelID = req.ModelID
	}
	if req.AutoRecover != nil {
		m.AutoRecover = *req.AutoRecover
	}
	if req.MaxOutputTokens != nil {
		m.MaxOutputTokens = *req.MaxOutputTokens
	}

	updated, err := h.svc.Update(c.Request.Context(), id, m)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, groupMonitorToResponse(updated))
}

// Delete DELETE /api/v1/admin/group-monitors/:id
func (h *GroupMonitorHandler) Delete(c *gin.Context) {
	id, ok := parseGroupMonitorID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "deleted"})
}

// Run POST /api/v1/admin/group-monitors/:id/run
func (h *GroupMonitorHandler) Run(c *gin.Context) {
	id, ok := parseGroupMonitorID(c)
	if !ok {
		return
	}
	results, err := h.svc.RunCheck(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, results)
}

// ListResults GET /api/v1/admin/group-monitors/:id/results
func (h *GroupMonitorHandler) ListResults(c *gin.Context) {
	id, ok := parseGroupMonitorID(c)
	if !ok {
		return
	}
	results, err := h.svc.ListResults(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, results)
}
