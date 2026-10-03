package notify

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
)

// Policy 通知去抖策略。所有字段都是面向"少烦用户"取向：
//   - BatchRateChanges：同次扫描中合并多条倍率相关通知
//   - MinChangePct：涨跌幅小于阈值时跳过推送（仍写入 RateChangeLog 表）
//   - BalanceLowCooldown：同渠道 balance_low 在窗口内不重复发送
//   - SendMaxAttempts：单条消息最多发送尝试次数（含首发），<=1 表示不重试
type Policy struct {
	NotificationPrefix                       string
	BatchRateChanges                         bool
	MinChangePct                             float64
	BalanceLowCooldown                       time.Duration
	SubscriptionDailyRemainingThresholdPct   float64
	SubscriptionWeeklyRemainingThresholdPct  float64
	SubscriptionMonthlyRemainingThresholdPct float64
	SubscriptionExpiryThreshold              time.Duration
	SubscriptionAlertCooldown                time.Duration
	SendMaxAttempts                          int
}

// CooldownStore Dispatcher 用来判断某个 (channelID, event) 是否还在冷却窗口。
//
// 抽象成 interface 是为了让 dispatcher 不依赖具体存储；
// 生产实现是 *storage.Notifications.TryClaimCooldown；
// 测试时可以注入一个内存 stub。
type CooldownStore interface {
	TryClaimCooldown(channelID uint, event storage.NotificationEvent, cooldown time.Duration) (bool, error)
}

// RateChange 是一条待发送的倍率相关记录（去抖 / 合并的基本单元）。
type RateChange struct {
	GroupName string
	OldRatio  float64
	NewRatio  float64
	OldComp   float64
	NewComp   float64
	ChangedAt time.Time
}

type RateStructureChange struct {
	Added   []RateChange
	Removed []RateChange
}

// ChangePctAbove 涨跌幅是否达到阈值。
// minPct = 0 表示不过滤。OldRatio = 0 时按"新出现的分组"处理，永远算"达到阈值"。
func (rc RateChange) ChangePctAbove(minPct float64) bool {
	if minPct <= 0 {
		return true
	}
	if rc.OldRatio == 0 {
		return true
	}
	pct := math.Abs(rc.NewRatio-rc.OldRatio) / math.Abs(rc.OldRatio) * 100
	return pct >= minPct
}

// BuildBatchMessage 把多条 RateChange 合并成一条 notify.Message。
// 当只有 1 条时仍走这个路径，但 Subject / Body 自然退化成单条提醒。
func BuildBatchMessage(channel *storage.Channel, changes []RateChange) Message {
	return BuildRateBatchMessage(channel, storage.EventRateChanged, changes)
}

func BuildRateBatchMessage(channel *storage.Channel, event storage.NotificationEvent, changes []RateChange) Message {
	if len(changes) == 0 {
		return Message{}
	}
	now := time.Now()
	if len(changes) == 1 {
		c := changes[0]
		if event == storage.EventRateAdded {
			return Message{
				Event:     storage.EventRateAdded,
				ChannelID: channel.ID,
				ModelName: c.GroupName,
				Subject:   fmt.Sprintf("【分组新增提醒】%s · %s", channel.Name, c.GroupName),
				Body: fmt.Sprintf(
					"渠道：%s\n新增分组：%s\n倍率：%g\n发现时间：%s",
					channel.Name, c.GroupName, c.NewRatio, now.Format("2006-01-02 15:04"),
				),
			}
		}
		if event == storage.EventRateRemoved {
			return Message{
				Event:     storage.EventRateRemoved,
				ChannelID: channel.ID,
				ModelName: c.GroupName,
				Subject:   fmt.Sprintf("【分组删除提醒】%s · %s", channel.Name, c.GroupName),
				Body: fmt.Sprintf(
					"渠道：%s\n删除分组：%s\n原倍率：%g\n发现时间：%s",
					channel.Name, c.GroupName, c.OldRatio, now.Format("2006-01-02 15:04"),
				),
			}
		}
		return Message{
			Event:     storage.EventRateChanged,
			ChannelID: channel.ID,
			ModelName: c.GroupName,
			Subject:   fmt.Sprintf("【倍率变化提醒】%s · %s", channel.Name, c.GroupName),
			Body: fmt.Sprintf(
				"渠道：%s\n分组倍率：%s 由 %g %s至 %g\n变化时间：%s",
				channel.Name, c.GroupName, c.OldRatio, arrowFor(c.OldRatio, c.NewRatio), c.NewRatio,
				now.Format("2006-01-02 15:04"),
			),
		}
	}

	// 合并多条：subject 简短，body 列出每条。
	var b strings.Builder
	switch event {
	case storage.EventRateAdded:
		fmt.Fprintf(&b, "渠道：%s\n共 %d 个新增分组：\n", channel.Name, len(changes))
		for _, c := range changes {
			fmt.Fprintf(&b, "  · %s：倍率 %g\n", c.GroupName, c.NewRatio)
		}
		fmt.Fprintf(&b, "时间：%s", now.Format("2006-01-02 15:04"))
		return Message{
			Event:     storage.EventRateAdded,
			ChannelID: channel.ID,
			ModelName: "",
			Subject:   fmt.Sprintf("【分组新增提醒】%s · %d 个分组", channel.Name, len(changes)),
			Body:      b.String(),
		}
	case storage.EventRateRemoved:
		fmt.Fprintf(&b, "渠道：%s\n共 %d 个删除分组：\n", channel.Name, len(changes))
		for _, c := range changes {
			fmt.Fprintf(&b, "  · %s：原倍率 %g\n", c.GroupName, c.OldRatio)
		}
		fmt.Fprintf(&b, "时间：%s", now.Format("2006-01-02 15:04"))
		return Message{
			Event:     storage.EventRateRemoved,
			ChannelID: channel.ID,
			ModelName: "",
			Subject:   fmt.Sprintf("【分组删除提醒】%s · %d 个分组", channel.Name, len(changes)),
			Body:      b.String(),
		}
	default:
		fmt.Fprintf(&b, "渠道：%s\n共 %d 个分组倍率变化：\n", channel.Name, len(changes))
		for _, c := range changes {
			fmt.Fprintf(&b, "  · %s：%g %s至 %g\n",
				c.GroupName, c.OldRatio, arrowFor(c.OldRatio, c.NewRatio), c.NewRatio)
		}
		fmt.Fprintf(&b, "时间：%s", now.Format("2006-01-02 15:04"))
	}

	// ModelName 在合并消息里没有单一值；填空，订阅过滤改在 Dispatcher 里按"先按订阅切片再合并"处理。
	return Message{
		Event:     storage.EventRateChanged,
		ChannelID: channel.ID,
		ModelName: "",
		Subject:   fmt.Sprintf("【倍率变化提醒】%s · %d 个分组变动", channel.Name, len(changes)),
		Body:      b.String(),
	}
}

func BuildRateStructureMessage(channel *storage.Channel, change RateStructureChange) Message {
	total := len(change.Added) + len(change.Removed)
	if channel == nil || total == 0 {
		return Message{}
	}
	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "渠道：%s\n共 %d 个分组变动", channel.Name, total)
	if len(change.Added) > 0 {
		fmt.Fprintf(&b, "\n\n新增 %d 个分组：\n", len(change.Added))
		for _, c := range change.Added {
			fmt.Fprintf(&b, "  · %s：倍率 %g\n", c.GroupName, c.NewRatio)
		}
	}
	if len(change.Removed) > 0 {
		fmt.Fprintf(&b, "\n删除 %d 个分组：\n", len(change.Removed))
		for _, c := range change.Removed {
			fmt.Fprintf(&b, "  · %s：原倍率 %g\n", c.GroupName, c.OldRatio)
		}
	}
	fmt.Fprintf(&b, "\n时间：%s", now.Format("2006-01-02 15:04"))

	return Message{
		Event:     storage.EventRateStructureChanged,
		ChannelID: channel.ID,
		ModelName: "",
		Subject:   fmt.Sprintf("[分组变动通知] %s · 新增 %d / 删除 %d", channel.Name, len(change.Added), len(change.Removed)),
		Body:      b.String(),
	}
}

func arrowFor(oldV, newV float64) string {
	switch {
	case newV > oldV:
		return "上涨"
	case newV < oldV:
		return "下调"
	default:
		return "调整"
	}
}
