package notify

import (
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
)

func TestSubscriptionMatchesLegacyAllEvents(t *testing.T) {
	sub := Subscription{
		ChannelIDs: []uint{1},
		Mode:       SubscriptionModeGroups,
		Groups:     []string{"beta"},
	}

	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventAnnouncement}) {
		t.Fatal("legacy subscription should match non-rate events")
	}
	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventRateChanged, ModelName: "beta"}) {
		t.Fatal("legacy subscription should match selected rate group")
	}
	if sub.Matches(Message{ChannelID: 1, Event: storage.EventRateChanged, ModelName: "gamma"}) {
		t.Fatal("legacy subscription should reject unselected rate group")
	}
}

func TestSubscriptionMatchesSpecifiedEvents(t *testing.T) {
	sub := Subscription{
		ChannelIDs: []uint{1},
		Mode:       SubscriptionModeAll,
		Events: []storage.NotificationEvent{
			storage.EventAnnouncement,
			storage.EventBalanceLow,
		},
	}

	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventAnnouncement}) {
		t.Fatal("subscription should match selected announcement event")
	}
	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventBalanceLow}) {
		t.Fatal("subscription should match selected balance event")
	}
	if sub.Matches(Message{ChannelID: 1, Event: storage.EventMonitorFailed}) {
		t.Fatal("subscription should reject unselected event")
	}
	if sub.Matches(Message{ChannelID: 2, Event: storage.EventAnnouncement}) {
		t.Fatal("subscription should reject another channel")
	}
}

func TestSubscriptionMatchesSpecifiedEventsAndGroups(t *testing.T) {
	sub := Subscription{
		ChannelIDs: []uint{1},
		Mode:       SubscriptionModeGroups,
		Groups:     []string{"beta"},
		Events: []storage.NotificationEvent{
			storage.EventRateChanged,
			storage.EventSubscriptionExpiring,
		},
	}

	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventRateChanged, ModelName: "beta"}) {
		t.Fatal("subscription should match selected rate event and group")
	}
	if sub.Matches(Message{ChannelID: 1, Event: storage.EventRateChanged, ModelName: "gamma"}) {
		t.Fatal("subscription should reject selected rate event with unselected group")
	}
	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventSubscriptionExpiring}) {
		t.Fatal("subscription should match selected non-rate event without group")
	}
	if sub.Matches(Message{ChannelID: 1, Event: storage.EventAnnouncement}) {
		t.Fatal("subscription should reject unselected non-rate event")
	}
}

// 多选渠道：一条规则覆盖多个上游，任一命中即放行。
func TestSubscriptionMatchesMultipleChannels(t *testing.T) {
	sub := Subscription{
		ChannelIDs: []uint{1, 2, 3},
		Mode:       SubscriptionModeAll,
	}

	if !sub.Matches(Message{ChannelID: 1, Event: storage.EventAnnouncement}) {
		t.Fatal("subscription should match first channel")
	}
	if !sub.Matches(Message{ChannelID: 2, Event: storage.EventBalanceLow}) {
		t.Fatal("subscription should match second channel")
	}
	if !sub.Matches(Message{ChannelID: 3, Event: storage.EventMonitorFailed}) {
		t.Fatal("subscription should match third channel")
	}
	if sub.Matches(Message{ChannelID: 4, Event: storage.EventAnnouncement}) {
		t.Fatal("subscription should reject channel not in list")
	}
}

// 解析旧格式 channel_id（单值）应自动规整为 ChannelIDs。
func TestParseSubscriptionsLegacyChannelID(t *testing.T) {
	list, err := ParseSubscriptions(`[{"channel_id":7,"mode":"all"}]`)
	if err != nil {
		t.Fatalf("parse legacy: %v", err)
	}
	if len(list) != 1 || len(list[0].ChannelIDs) != 1 || list[0].ChannelIDs[0] != 7 {
		t.Fatalf("legacy channel_id should migrate to ChannelIDs=[7], got %+v", list)
	}
}
