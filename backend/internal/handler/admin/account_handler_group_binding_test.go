package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAccountRequestDecodesAccountGroups(t *testing.T) {
	var req UpdateAccountRequest
	err := json.Unmarshal([]byte(`{
		"group_ids": [2],
		"account_groups": [{"account_id": 13, "group_id": 2, "priority": 1}]
	}`), &req)
	require.NoError(t, err)
	require.NotNil(t, req.GroupIDs)
	require.Equal(t, []int64{2}, *req.GroupIDs)
	require.Len(t, req.AccountGroups, 1)
	require.Equal(t, int64(13), req.AccountGroups[0].AccountID)
	require.Equal(t, int64(2), req.AccountGroups[0].GroupID)
	require.Equal(t, 1, req.AccountGroups[0].Priority)
}
