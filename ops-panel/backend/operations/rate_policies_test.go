package operations

import (
	"strings"
	"testing"
)

func TestRatePolicyBindingsOrEmptyReturnsJSONSafeSlice(t *testing.T) {
	bindings := map[string][]RatePolicyBinding{}
	value := ratePolicyBindingsOrEmpty(bindings, "account:1")
	if value == nil {
		t.Fatal("missing bindings must be represented by an empty slice")
	}
	if len(value) != 0 {
		t.Fatalf("missing bindings length = %d, want 0", len(value))
	}
}

func TestNormalizeRatePolicyInputRejectsRetiredProfitMode(t *testing.T) {
	_, err := normalizeRatePolicyInput(RatePolicyInput{Mode: "profit"})
	if err == nil || !strings.Contains(err.Error(), "unsupported rate policy mode") {
		t.Fatalf("normalize profit mode error = %v, want unsupported mode", err)
	}
}

func TestNormalizeRatePolicyInputPreservesMultipleBindings(t *testing.T) {
	input := RatePolicyInput{
		Enabled: true,
		Mode:    "max",
		Offset:  0.03,
		Bindings: []RatePolicyBindingInput{
			{ChannelID: 8, GroupID: "38"},
			{ChannelID: 9, GroupID: "59"},
		},
	}
	normalized, err := normalizeRatePolicyInput(input)
	if err != nil {
		t.Fatalf("normalize multi-source policy: %v", err)
	}
	if len(normalized.Bindings) != 2 {
		t.Fatalf("binding count = %d, want 2", len(normalized.Bindings))
	}
	if normalized.Bindings[0].GroupID != "38" || normalized.Bindings[1].GroupID != "59" {
		t.Fatalf("normalized bindings = %+v", normalized.Bindings)
	}
}

func TestDisabledRatePolicySourcesAreNotResolvedAsBindings(t *testing.T) {
	bindings := []RatePolicyBindingInput{
		{ChannelID: 8, GroupID: "38"},
		{ChannelID: 9, GroupID: "59"},
	}
	sources := map[string]RatePolicySource{
		"8:38": {ChannelID: 8, GroupID: "38", Enabled: true},
		"9:59": {ChannelID: 9, GroupID: "59", Enabled: false},
	}
	resolved, err := resolveRatePolicyBindings(sources, bindings)
	if err != nil {
		t.Fatalf("resolve bindings: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ChannelID != bindings[0].ChannelID || resolved[0].GroupID != bindings[0].GroupID {
		t.Fatalf("resolved bindings = %+v, want only enabled source", resolved)
	}
}

func TestEnabledRatePolicyBindingsHideDisabledSources(t *testing.T) {
	bindings := []RatePolicyBinding{
		{ChannelID: 8, GroupID: "38"},
		{ChannelID: 9, GroupID: "59"},
	}
	filtered := enabledRatePolicyBindings(bindings, []RatePolicySource{
		{ChannelID: 8, GroupID: "38", Enabled: true},
		{ChannelID: 9, GroupID: "59", Enabled: false},
	})
	if len(filtered) != 1 || filtered[0].ChannelID != 8 || filtered[0].GroupID != "38" {
		t.Fatalf("filtered bindings = %+v, want only enabled source", filtered)
	}
}

func TestValidateRatePolicyBindingCountRejectsMultipleAccountBindings(t *testing.T) {
	bindings := []RatePolicyBindingInput{
		{ChannelID: 8, GroupID: "38"},
		{ChannelID: 9, GroupID: "59"},
	}
	if err := validateRatePolicyBindingCount(RatePolicyTargetAccount, bindings); err == nil ||
		!strings.Contains(err.Error(), "exactly one source binding") {
		t.Fatalf("validate account bindings error = %v, want single-source rejection", err)
	}
	if err := validateRatePolicyBindingCount(RatePolicyTargetGroup, bindings); err != nil {
		t.Fatalf("validate group bindings: %v", err)
	}
}
