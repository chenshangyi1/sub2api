package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AdaptiveHandler manages Adaptive parent-group topology (admin only).
type AdaptiveHandler struct {
	repo     service.AdaptivePoolAdminRepository
	settings *service.SettingService
}

func NewAdaptiveHandler(repo service.AdaptivePoolAdminRepository) *AdaptiveHandler {
	return &AdaptiveHandler{repo: repo}
}

// SetSettingService wires Anti-Stall PRO settings (optional).
func (h *AdaptiveHandler) SetSettingService(settings *service.SettingService) {
	if h != nil {
		h.settings = settings
	}
}

type adaptiveLeafDTO struct {
	LeafGroupID int64 `json:"leaf_group_id"`
	Enabled     bool  `json:"enabled"`
	SortOrder   int   `json:"sort_order"`
}

type adaptivePoolDTO struct {
	ParentGroupID    int64             `json:"parent_group_id"`
	Platform         string            `json:"platform"`
	Enabled          bool              `json:"enabled"`
	AllowHybrid      bool              `json:"allow_hybrid"`
	ConfigGeneration int64             `json:"config_generation"`
	Members          []adaptiveLeafDTO `json:"members"`
}

type putAdaptivePoolRequest struct {
	Enabled     bool              `json:"enabled"`
	AllowHybrid bool              `json:"allow_hybrid"`
	Members     []adaptiveLeafDTO `json:"members"`
}

func snapshotToDTO(s *service.AdaptivePoolSnapshot) adaptivePoolDTO {
	if s == nil {
		return adaptivePoolDTO{Members: []adaptiveLeafDTO{}}
	}
	members := make([]adaptiveLeafDTO, 0, len(s.Members))
	for _, m := range s.Members {
		members = append(members, adaptiveLeafDTO{
			LeafGroupID: m.LeafGroupID,
			Enabled:     m.Enabled,
			SortOrder:   m.SortOrder,
		})
	}
	return adaptivePoolDTO{
		ParentGroupID:    s.ParentGroupID,
		Platform:         s.Platform,
		Enabled:          s.Enabled,
		AllowHybrid:      s.AllowHybrid,
		ConfigGeneration: s.ConfigGeneration,
		Members:          members,
	}
}

// List GET /admin/adaptive-groups
func (h *AdaptiveHandler) List(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "adaptive admin is not configured")
		return
	}
	items, err := h.repo.ListAdaptivePoolSnapshots(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]adaptivePoolDTO, 0, len(items))
	for i := range items {
		out = append(out, snapshotToDTO(&items[i]))
	}
	response.Success(c, gin.H{"items": out, "total": len(out)})
}

// GetByParentID GET /admin/adaptive-groups/:parent_group_id
func (h *AdaptiveHandler) GetByParentID(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "adaptive admin is not configured")
		return
	}
	parentID, err := strconv.ParseInt(c.Param("parent_group_id"), 10, 64)
	if err != nil || parentID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid parent_group_id")
		return
	}
	snapshot, err := h.repo.GetAdaptivePoolSnapshot(c.Request.Context(), parentID)
	if err != nil {
		if errors.Is(err, service.ErrAdaptivePoolNotFound) {
			response.Error(c, http.StatusNotFound, "adaptive pool not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, snapshotToDTO(snapshot))
}

// Put PUT /admin/adaptive-groups/:parent_group_id
// Full replacement of memberships for the Adaptive parent group.
func (h *AdaptiveHandler) Put(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "adaptive admin is not configured")
		return
	}
	parentID, err := strconv.ParseInt(c.Param("parent_group_id"), 10, 64)
	if err != nil || parentID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid parent_group_id")
		return
	}
	var req putAdaptivePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	members := make([]service.AdaptiveLeafRef, 0, len(req.Members))
	for _, m := range req.Members {
		members = append(members, service.AdaptiveLeafRef{
			LeafGroupID: m.LeafGroupID,
			Enabled:     m.Enabled,
			SortOrder:   m.SortOrder,
		})
	}
	snapshot, err := h.repo.ReplaceAdaptivePool(c.Request.Context(), service.AdaptivePoolUpdate{
		ParentGroupID: parentID,
		Enabled:       req.Enabled,
		AllowHybrid:   req.AllowHybrid,
		Members:       members,
	})
	if err != nil {
		// DB trigger validation messages surface as 400.
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, snapshotToDTO(snapshot))
}

// Delete DELETE /admin/adaptive-groups/:parent_group_id
func (h *AdaptiveHandler) Delete(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "adaptive admin is not configured")
		return
	}
	parentID, err := strconv.ParseInt(c.Param("parent_group_id"), 10, 64)
	if err != nil || parentID <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid parent_group_id")
		return
	}
	if err := h.repo.DeleteAdaptivePool(c.Request.Context(), parentID); err != nil {
		if errors.Is(err, service.ErrAdaptivePoolNotFound) {
			response.Error(c, http.StatusNotFound, "adaptive pool not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{"deleted": true, "parent_group_id": parentID})
}

// GetAntiStallPro GET /admin/anti-stall-pro
// Returns admin config: module_enabled + basic/pro/ultra tier parameters.
// Users pick a tier on their API key; these are the parameter sources.
func (h *AdaptiveHandler) GetAntiStallPro(c *gin.Context) {
	if h == nil || h.settings == nil {
		response.Success(c, service.DefaultAntiStallAdminConfig())
		return
	}
	response.Success(c, h.settings.GetAntiStallAdminConfig(c.Request.Context()))
}

// PutAntiStallPro PUT /admin/anti-stall-pro
// Accepts full AntiStallAdminConfig (module_enabled + basic/pro/ultra params).
func (h *AdaptiveHandler) PutAntiStallPro(c *gin.Context) {
	if h == nil || h.settings == nil {
		response.Error(c, http.StatusServiceUnavailable, "anti-stall settings unavailable")
		return
	}
	var req service.AntiStallAdminConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.settings.UpdateAntiStallAdminConfig(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, out)
}
