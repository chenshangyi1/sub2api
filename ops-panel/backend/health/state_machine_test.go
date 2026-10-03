package health

import (
	"testing"
	"time"
)

func at(offset time.Duration) time.Time {
	return time.Unix(1_700_000_000, 0).Add(offset)
}

func TestHardFailureSuspendsImmediately(t *testing.T) {
	_, tr := Step(Config{}, Snapshot{}, ProbeResult{Success: false, StatusCode: 503, At: at(0)})
	if tr.To != StateSuspended {
		t.Fatalf("expected suspended, got %s", tr.To)
	}
	if !tr.Changed {
		t.Fatal("expected state change")
	}
}

func TestAuthFailureIsHardFailure(t *testing.T) {
	for _, code := range []int{401, 403, 404} {
		if Classify(ProbeResult{StatusCode: code}) != KindHardFailure {
			t.Fatalf("status %d should be hard failure", code)
		}
	}
}

func TestSoftFailuresDegradeThenSuspend(t *testing.T) {
	cfg := Config{FailureThreshold: 3}
	snap := Snapshot{}
	var tr Transition
	for i := 1; i <= 2; i++ {
		snap, tr = Step(cfg, snap, ProbeResult{Success: false, StatusCode: 0, At: at(time.Duration(i) * time.Minute)})
		if tr.To != StateDegraded {
			t.Fatalf("step %d expected degraded, got %s", i, tr.To)
		}
	}
	snap, tr = Step(cfg, snap, ProbeResult{Success: false, StatusCode: 429, At: at(3 * time.Minute)})
	if tr.To != StateSuspended {
		t.Fatalf("expected suspended after threshold, got %s", tr.To)
	}
	if snap.CooldownUntil == nil {
		t.Fatal("expected cooldown set")
	}
}

func TestRecoveryPathFromSuspended(t *testing.T) {
	cfg := Config{CooldownDuration: 5 * time.Minute, ObservingDuration: 5 * time.Minute, SuccessThreshold: 2, RecoveryStepPercent: 25}
	snap := Snapshot{State: StateSuspended, WeightPercent: 0}
	now := at(10 * time.Minute)

	// 冷却后的第一个成功 → observing
	snap, tr := Step(cfg, snap, ProbeResult{Success: true, At: now})
	if tr.To != StateObserving || snap.ConsecutiveSuccesses != 1 {
		t.Fatalf("expected observing, got %s successes=%d", tr.To, snap.ConsecutiveSuccesses)
	}
	// 观察窗内第二次成功 → recovering（25%）
	snap, tr = Step(cfg, snap, ProbeResult{Success: true, At: now.Add(6 * time.Minute)})
	if tr.To != StateRecovering || snap.WeightPercent != 25 {
		t.Fatalf("expected recovering 25%%, got %s %d%%", tr.To, snap.WeightPercent)
	}
	// 连续成功阶梯回升 → healthy
	snap, tr = Step(cfg, snap, ProbeResult{Success: true, At: now.Add(7 * time.Minute)})
	if tr.To != StateRecovering || snap.WeightPercent != 50 {
		t.Fatalf("expected recovering 50%%, got %s %d%%", tr.To, snap.WeightPercent)
	}
	snap, _ = Step(cfg, snap, ProbeResult{Success: true, At: now.Add(8 * time.Minute)})
	snap, tr = Step(cfg, snap, ProbeResult{Success: true, At: now.Add(9 * time.Minute)})
	if tr.To != StateHealthy || snap.WeightPercent != 100 {
		t.Fatalf("expected healthy 100%%, got %s %d%%", tr.To, snap.WeightPercent)
	}
}

func TestSoftFailureDuringRecoverySuspends(t *testing.T) {
	cfg := Config{}
	snap := Snapshot{State: StateRecovering, WeightPercent: 50}
	snap, tr := Step(cfg, snap, ProbeResult{Success: false, StatusCode: 0, At: at(1 * time.Minute)})
	if tr.To != StateSuspended {
		t.Fatalf("expected suspended during recovery, got %s", tr.To)
	}
}

func TestInvalidResponseDoesNotChangeState(t *testing.T) {
	snap := Snapshot{State: StateHealthy, WeightPercent: 100}
	snap, tr := Step(Config{}, snap, ProbeResult{Success: false, StatusCode: 400, At: at(0)})
	if tr.Changed || tr.To != StateHealthy {
		t.Fatalf("invalid response must not change state, got %s changed=%v", tr.To, tr.Changed)
	}
	if snap.ConsecutiveSuccesses != 0 {
		t.Fatalf("expected consecutive successes reset, got %d", snap.ConsecutiveSuccesses)
	}
}

func TestDisabledIsFrozen(t *testing.T) {
	snap := Snapshot{State: StateDisabled}
	for _, r := range []ProbeResult{
		{Success: true, At: at(0)},
		{Success: false, StatusCode: 503, At: at(time.Minute)},
	} {
		next, tr := Step(Config{}, snap, r)
		if tr.To != StateDisabled || next.State != StateDisabled {
			t.Fatalf("disabled must stay frozen, got %s", tr.To)
		}
	}
}
