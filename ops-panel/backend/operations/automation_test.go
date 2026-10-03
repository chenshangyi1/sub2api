package operations

import "testing"

func TestNormalizeAutomationTreatsEmptyPriorityGroupsAsAll(t *testing.T) {
	input := AutomationSettings{
		AccountPool: defaultPoolPolicy(),
		AccountPriority: AccountPriorityPolicy{
			Enabled:         true,
			Strategy:        "rate",
			TargetGroupIDs:  []int64{},
			SampleSize:      10,
			LookbackMinutes: 60,
		},
	}

	normalized, err := normalizeAutomation(input)
	if err != nil {
		t.Fatalf("normalize enabled all-groups priority policy: %v", err)
	}
	if !normalized.AccountPriority.Enabled || len(normalized.AccountPriority.TargetGroupIDs) != 0 {
		t.Fatalf("unexpected normalized priority policy: %+v", normalized.AccountPriority)
	}
}
