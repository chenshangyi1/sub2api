package operations

import (
	"context"
	"testing"
	"time"
)

func TestListRatePolicyRulesPostgresQuotesOffset(t *testing.T) {
	db := openOperationsPostgresSchema(t, "ops_rate_policy_offset")
	if err := db.AutoMigrate(&blGroupRateRuleSchema{}, &blAccountRateRuleSchema{}); err != nil {
		t.Fatalf("create rate policy tables: %v", err)
	}

	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	groupInput := RatePolicyInput{Enabled: true, Mode: "custom", Offset: 1.25, Expression: "source + 0.25"}
	if err := upsertRatePolicyRule(db, "bl_group_rate_rules", "group_id", 10, 20, groupInput, now); err != nil {
		t.Fatalf("insert group rate policy: %v", err)
	}

	service := &Service{db: db}
	rules, err := service.listRatePolicyRules(context.Background(), 10, RatePolicyTargetGroup)
	if err != nil {
		t.Fatalf("list group rate policies: %v", err)
	}
	rule, ok := rules[20]
	if !ok {
		t.Fatal("group rate policy 20 is missing")
	}
	if !rule.Enabled || rule.Mode != groupInput.Mode || rule.Offset != groupInput.Offset || rule.Expression != groupInput.Expression {
		t.Fatalf("group rate policy = %+v", rule)
	}

	accountInput := RatePolicyInput{Enabled: true, Mode: "locked", Offset: 2.5}
	if err := upsertRatePolicyRule(db, "bl_account_rate_rules", "account_id", 10, 30, accountInput, now); err != nil {
		t.Fatalf("insert account rate policy: %v", err)
	}
	accountInput.Offset = 3.75
	if err := upsertRatePolicyRule(db, "bl_account_rate_rules", "account_id", 10, 30, accountInput, now.Add(time.Minute)); err != nil {
		t.Fatalf("update account rate policy: %v", err)
	}
	accountRules, err := service.listRatePolicyRules(context.Background(), 10, RatePolicyTargetAccount)
	if err != nil {
		t.Fatalf("list account rate policies: %v", err)
	}
	accountRule, ok := accountRules[30]
	if !ok {
		t.Fatal("account rate policy 30 is missing")
	}
	if !accountRule.Enabled || accountRule.Mode != accountInput.Mode || accountRule.Offset != accountInput.Offset {
		t.Fatalf("account rate policy = %+v", accountRule)
	}
}
