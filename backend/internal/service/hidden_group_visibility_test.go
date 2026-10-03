package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupHiddenFromEndUsers(t *testing.T) {
	t.Parallel()

	require.False(t, (*Group)(nil).HiddenFromEndUsers())
	require.False(t, (&Group{}).HiddenFromEndUsers())
	require.False(t, (&Group{UserVisible: true, UserVisibleSet: true}).HiddenFromEndUsers())
	require.True(t, (&Group{UserVisible: false, UserVisibleSet: true}).HiddenFromEndUsers())
}

func TestGroupVisibleToUserAllowsAdminOnlyForHiddenGroups(t *testing.T) {
	t.Parallel()

	hidden := &Group{ID: 7, UserVisible: false, UserVisibleSet: true}
	visible := &Group{ID: 8, UserVisible: true, UserVisibleSet: true}
	user := &User{Role: RoleUser}
	admin := &User{Role: RoleAdmin}

	require.False(t, hidden.VisibleToUser(nil))
	require.False(t, hidden.VisibleToUser(user))
	require.True(t, hidden.VisibleToUser(admin))
	require.True(t, visible.VisibleToUser(user))
	require.True(t, visible.VisibleToUser(nil))
}

func TestCanUserBindGroupInternalHidesNonVisibleGroups(t *testing.T) {
	t.Parallel()

	svc := &APIKeyService{}
	user := &User{ID: 1, Role: RoleUser}
	admin := &User{ID: 2, Role: RoleAdmin}
	hidden := &Group{ID: 11, UserVisible: false, UserVisibleSet: true, SubscriptionType: SubscriptionTypeStandard}
	public := &Group{ID: 12, UserVisible: true, UserVisibleSet: true, SubscriptionType: SubscriptionTypeStandard}

	require.False(t, svc.canUserBindGroupInternal(user, hidden, nil))
	require.True(t, svc.canUserBindGroupInternal(admin, hidden, nil))
	require.True(t, svc.canUserBindGroupInternal(user, public, nil))
	require.False(t, svc.canUserBindGroupInternal(nil, public, nil))
}
