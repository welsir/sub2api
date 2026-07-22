package pool

import (
	"testing"
	"time"
)

func TestScoreRequiresApprovedStabilityThresholds(t *testing.T) {
	cfg := DefaultConfig()
	stable := Metrics{SuccessRate: 0.99, ConsecutiveSuccesses: 3, P95: 2400 * time.Millisecond}
	if !EligibleForProduction(stable, cfg) {
		t.Fatal("stable node was rejected")
	}

	for name, metrics := range map[string]Metrics{
		"success rate": {SuccessRate: 0.98, ConsecutiveSuccesses: 3, P95: time.Second},
		"streak":       {SuccessRate: 1, ConsecutiveSuccesses: 2, P95: time.Second},
		"p95":          {SuccessRate: 1, ConsecutiveSuccesses: 3, P95: 2600 * time.Millisecond},
	} {
		if EligibleForProduction(metrics, cfg) {
			t.Errorf("ineligible node accepted: %s", name)
		}
	}
}

func TestScoreEmergencyFailureBypassesReplacementCooldown(t *testing.T) {
	cfg := DefaultConfig()
	current := Metrics{ConsecutiveFailures: 2}
	if !RequiresEmergencyReplacement(current, cfg) {
		t.Fatal("two consecutive failures did not require replacement")
	}
}

func TestScorePerformanceReplacementNeedsThreeWindowsAndTwentyPercent(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()
	current := Metrics{SuccessRate: 1, ConsecutiveSuccesses: 10, P95: 3 * time.Second, SlowWindows: 3}
	candidate := Metrics{SuccessRate: 1, ConsecutiveSuccesses: 10, P95: 2400 * time.Millisecond}

	if !ShouldPerformanceReplace(current, candidate, now.Add(-time.Hour), now, cfg) {
		t.Fatal("qualified 25 percent improvement was rejected")
	}
	if ShouldPerformanceReplace(current, candidate, now.Add(-10*time.Minute), now, cfg) {
		t.Fatal("replacement cooldown was ignored")
	}
	current.SlowWindows = 2
	if ShouldPerformanceReplace(current, candidate, now.Add(-time.Hour), now, cfg) {
		t.Fatal("node with only two slow windows was replaced")
	}
	current.SlowWindows = 3
	candidate.P95 = 2500 * time.Millisecond
	if ShouldPerformanceReplace(current, candidate, now.Add(-time.Hour), now, cfg) {
		t.Fatal("candidate with less than 20 percent improvement was accepted")
	}
}
