package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adaptiveAdminRepoStub struct {
	snapshot *service.AdaptivePoolSnapshot
	list     []service.AdaptivePoolSnapshot
	put      service.AdaptivePoolUpdate
	deleted  int64
}

func (s *adaptiveAdminRepoStub) GetAdaptivePoolSnapshot(_ context.Context, parentGroupID int64) (*service.AdaptivePoolSnapshot, error) {
	if s.snapshot == nil || s.snapshot.ParentGroupID != parentGroupID {
		return nil, service.ErrAdaptivePoolNotFound
	}
	cp := *s.snapshot
	return &cp, nil
}

func (s *adaptiveAdminRepoStub) ListAdaptivePoolSnapshots(context.Context) ([]service.AdaptivePoolSnapshot, error) {
	return append([]service.AdaptivePoolSnapshot(nil), s.list...), nil
}

func (s *adaptiveAdminRepoStub) ReplaceAdaptivePool(_ context.Context, input service.AdaptivePoolUpdate) (*service.AdaptivePoolSnapshot, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	s.put = input
	return &service.AdaptivePoolSnapshot{
		ParentGroupID:    input.ParentGroupID,
		Platform:         "openai",
		Enabled:          input.Enabled,
		AllowHybrid:      input.AllowHybrid,
		ConfigGeneration: 2,
		Members:          append([]service.AdaptiveLeafRef(nil), input.Members...),
	}, nil
}

func (s *adaptiveAdminRepoStub) DeleteAdaptivePool(_ context.Context, parentGroupID int64) error {
	s.deleted = parentGroupID
	return nil
}

func TestAdaptiveHandlerPutAndGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adaptiveAdminRepoStub{}
	h := NewAdaptiveHandler(repo)

	body := map[string]any{
		"enabled":      true,
		"allow_hybrid": true,
		"members": []map[string]any{
			{"leaf_group_id": 85, "enabled": true, "sort_order": 10},
			{"leaf_group_id": 17, "enabled": true, "sort_order": 20},
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/adaptive-groups/100", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "parent_group_id", Value: "100"}}
	h.Put(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(100), repo.put.ParentGroupID)
	require.True(t, repo.put.Enabled)
	require.True(t, repo.put.AllowHybrid)
	require.Len(t, repo.put.Members, 2)
	require.Contains(t, w.Body.String(), `"allow_hybrid":true`)

	repo.snapshot = &service.AdaptivePoolSnapshot{
		ParentGroupID: 100, Platform: "openai", Enabled: true, ConfigGeneration: 2,
		Members: repo.put.Members,
	}
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/adaptive-groups/100", nil)
	c2.Params = gin.Params{{Key: "parent_group_id", Value: "100"}}
	h.GetByParentID(c2)
	require.Equal(t, http.StatusOK, w2.Code)
	require.Contains(t, w2.Body.String(), `"parent_group_id":100`)
	require.Contains(t, w2.Body.String(), `"leaf_group_id":85`)
}

func TestAdaptiveHandlerPutRejectsEmptyEnabledPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAdaptiveHandler(&adaptiveAdminRepoStub{})
	body := []byte(`{"enabled":true,"members":[]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/adaptive-groups/100", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "parent_group_id", Value: "100"}}
	h.Put(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
