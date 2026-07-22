package pool

import (
	"strings"
	"testing"
	"time"
)

func TestConfigDefaultsMatchApprovedV2Policy(t *testing.T) {
	cfg := DefaultConfig()

	checks := map[string]bool{
		"disabled":                    !cfg.Enabled,
		"observe mode":                cfg.Mode == ModeObserve,
		"five production lanes":       cfg.ProductionLanes == 5,
		"three probe lanes":           cfg.CandidateProbeLanes == 3,
		"one canary lane":             cfg.CanaryLanes == 1,
		"two minute probe":            cfg.ProbeInterval == 2*time.Minute,
		"99 percent success":          cfg.MinSuccessRate == 0.99,
		"three consecutive successes": cfg.MinConsecutiveSuccesses == 3,
		"p95 threshold":               cfg.MaxP95 == 2500*time.Millisecond,
		"severe p95":                  cfg.SevereDegradationP95 == 5*time.Second,
		"two failures":                cfg.FailureThreshold == 2,
		"three slow windows":          cfg.SlowWindowThreshold == 3,
		"20 percent improvement":      cfg.MinPerformanceImprovement == 0.20,
		"30 minute cooldown":          cfg.ReplacementCooldown == 30*time.Minute,
		"two hour observation":        cfg.ObservationPeriod == 2*time.Hour,
		"30 minute canary":            cfg.CanaryPeriod == 30*time.Minute,
		"rollback proxy six":          cfg.RollbackProxyID == 6,
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("default check failed: %s", name)
		}
	}
}

func TestV2GuardRejectsNonV2Targets(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		baseURL    string
	}{
		{name: "missing instance marker", instanceID: "", baseURL: "http://app:8080"},
		{name: "legacy instance", instanceID: "sub2api-preview", baseURL: "http://app:8080"},
		{name: "preview URL", instanceID: "sub2api-v2", baseURL: "https://preview.example.com"},
		{name: "v1 URL", instanceID: "sub2api-v2", baseURL: "https://api-v1.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.InstanceID = tt.instanceID
			cfg.Sub2APIBaseURL = tt.baseURL
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded for a non-V2 target")
			}
		})
	}
}

func TestV2GuardAcceptsExplicitV2Target(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.InstanceID = "sub2api-v2"
	cfg.Sub2APIBaseURL = "http://sub2api-v2-app:8080"
	cfg.MihomoBaseURL = "http://sub2api-v2-mihomo:9090"
	cfg.MihomoSecret = strings.Repeat("x", 32)

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTypesKeepExpectedAndActualLaneStateSeparate(t *testing.T) {
	lane := Lane{
		Number:          1,
		ExpectedNodeKey: "wgetcloud/hk-01",
		ActualNodeKey:   "wgetcloud/sg-03",
		ManagedProxyID:  17,
		Pinned:          true,
	}
	if lane.ExpectedNodeKey == lane.ActualNodeKey {
		t.Fatal("expected and actual node state must be independently represented")
	}
	if lane.ManagedProxyID != 17 || !lane.Pinned {
		t.Fatalf("unexpected lane: %+v", lane)
	}
}
