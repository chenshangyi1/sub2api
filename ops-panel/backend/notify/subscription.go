package notify

import (
	"encoding/json"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

// SubscriptionMode 倍率分组过滤维度。
//
//   - all    订阅该上游命中事件的所有分组倍率
//   - groups 仅订阅该上游 + 指定分组（model_name）的倍率相关事件；
//     非倍率事件仍命中（分组过滤仅对倍率事件起作用）
type SubscriptionMode string

const (
	SubscriptionModeAll    SubscriptionMode = "all"
	SubscriptionModeGroups SubscriptionMode = "groups"
)

// Subscription 通知渠道对一组上游的订阅规则。
//
// ChannelIDs 支持多选：一条规则可同时覆盖多个上游，避免为每个上游重复配置。
// 兼容历史数据：解析时若仅有旧字段 channel_id（单值），自动转为 [channel_id]。
type Subscription struct {
	ChannelIDs []uint                      `json:"channel_ids"`
	Mode       SubscriptionMode            `json:"mode"`
	Groups     []string                    `json:"groups,omitempty"`
	Events     []storage.NotificationEvent `json:"events,omitempty"`
}

// UnmarshalJSON 兼容旧的 channel_id 单值格式：
// 旧数据 {"channel_id":1} 会被规整为 ChannelIDs=[1]，新数据直接用 channel_ids。
func (s *Subscription) UnmarshalJSON(data []byte) error {
	var raw struct {
		ChannelIDs []uint                      `json:"channel_ids"`
		ChannelID  uint                        `json:"channel_id"`
		Mode       SubscriptionMode            `json:"mode"`
		Groups     []string                    `json:"groups"`
		Events     []storage.NotificationEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ChannelIDs = raw.ChannelIDs
	if len(s.ChannelIDs) == 0 && raw.ChannelID != 0 {
		s.ChannelIDs = []uint{raw.ChannelID}
	}
	s.Mode = raw.Mode
	s.Groups = raw.Groups
	s.Events = raw.Events
	return nil
}

// ParseSubscriptions 容错解析 JSON 数组；空串或解析失败均返回 nil（视为"订阅一切"）。
func ParseSubscriptions(raw string) ([]Subscription, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" {
		return nil, nil
	}
	var list []Subscription
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Matches 判断这条订阅是否覆盖当前消息：
//   - 上游 ID 必须在 ChannelIDs 中
//   - Events 为空表示全部事件；非空时消息事件必须在 Events 中
//   - 倍率相关事件 + mode=groups 时，model_name 必须在 Groups 中
//   - 其它情况只要上游匹配即放行
func (s Subscription) Matches(msg Message) bool {
	if msg.ChannelID == 0 {
		return s.matchesEvent(msg.Event)
	}
	if !s.matchesChannel(msg.ChannelID) {
		return false
	}
	if !s.matchesEvent(msg.Event) {
		return false
	}
	if !isRateEvent(msg.Event) || s.Mode != SubscriptionModeGroups {
		return true
	}
	for _, g := range s.Groups {
		if g == msg.ModelName {
			return true
		}
	}
	return false
}

func (s Subscription) matchesChannel(id uint) bool {
	for _, c := range s.ChannelIDs {
		if c == id {
			return true
		}
	}
	return false
}

func (s Subscription) matchesEvent(event storage.NotificationEvent) bool {
	if len(s.Events) == 0 {
		return true
	}
	for _, e := range s.Events {
		if e == event {
			return true
		}
	}
	return false
}

func isRateEvent(event storage.NotificationEvent) bool {
	return event == storage.EventRateChanged ||
		event == storage.EventRateStructureChanged ||
		event == storage.EventRateAdded ||
		event == storage.EventRateRemoved
}

// AnyMatch 任意一条订阅命中即视为该通知渠道关心此消息。
// 调用方应在 len(subs) > 0 时才调；空切片由调用方按"订阅一切"处理。
func AnyMatch(subs []Subscription, msg Message) bool {
	for i := range subs {
		if subs[i].Matches(msg) {
			return true
		}
	}
	return false
}
