package service

import "time"

type AccountGroup struct {
	AccountID int64     `json:"account_id,omitempty"`
	GroupID   int64     `json:"group_id"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at,omitempty"`

	Account *Account
	Group   *Group
}
