package pool

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Enabled                   bool
	Mode                      Mode
	InstanceID                string
	Sub2APIBaseURL            string
	MihomoBaseURL             string
	MihomoSecret              string
	ProductionLanes           int
	CandidateProbeLanes       int
	CanaryLanes               int
	ProbeInterval             time.Duration
	MinSuccessRate            float64
	MinConsecutiveSuccesses   int
	MaxP95                    time.Duration
	SevereDegradationP95      time.Duration
	FailureThreshold          int
	SlowWindowThreshold       int
	MinPerformanceImprovement float64
	ReplacementCooldown       time.Duration
	ObservationPeriod         time.Duration
	CanaryPeriod              time.Duration
	RollbackProxyID           int64
}

func DefaultConfig() Config {
	return Config{
		Mode:                      ModeObserve,
		ProductionLanes:           5,
		CandidateProbeLanes:       3,
		CanaryLanes:               1,
		ProbeInterval:             2 * time.Minute,
		MinSuccessRate:            0.99,
		MinConsecutiveSuccesses:   3,
		MaxP95:                    2500 * time.Millisecond,
		SevereDegradationP95:      5 * time.Second,
		FailureThreshold:          2,
		SlowWindowThreshold:       3,
		MinPerformanceImprovement: 0.20,
		ReplacementCooldown:       30 * time.Minute,
		ObservationPeriod:         2 * time.Hour,
		CanaryPeriod:              30 * time.Minute,
		RollbackProxyID:           6,
	}
}

func (c Config) Validate() error {
	if c.Mode != ModeObserve && c.Mode != ModeAuto {
		return fmt.Errorf("mode must be observe or auto")
	}
	if c.ProductionLanes <= 0 || c.CandidateProbeLanes <= 0 || c.CanaryLanes <= 0 {
		return fmt.Errorf("lane counts must be positive")
	}
	if c.ProbeInterval <= 0 || c.MaxP95 <= 0 || c.SevereDegradationP95 <= c.MaxP95 ||
		c.ReplacementCooldown <= 0 || c.ObservationPeriod <= 0 || c.CanaryPeriod <= 0 {
		return fmt.Errorf("invalid timing thresholds")
	}
	if c.MinSuccessRate <= 0 || c.MinSuccessRate > 1 ||
		c.MinPerformanceImprovement <= 0 || c.MinPerformanceImprovement >= 1 {
		return fmt.Errorf("invalid ratio thresholds")
	}
	if c.MinConsecutiveSuccesses <= 0 || c.FailureThreshold <= 0 || c.SlowWindowThreshold <= 0 {
		return fmt.Errorf("sample thresholds must be positive")
	}
	if c.RollbackProxyID <= 0 {
		return fmt.Errorf("rollback proxy id must be positive")
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.InstanceID) != "sub2api-v2" {
		return fmt.Errorf("instance id must be sub2api-v2")
	}
	if err := validateV2URL("sub2api", c.Sub2APIBaseURL); err != nil {
		return err
	}
	if err := validateV2URL("mihomo", c.MihomoBaseURL); err != nil {
		return err
	}
	if len(strings.TrimSpace(c.MihomoSecret)) < 32 {
		return fmt.Errorf("mihomo secret must be at least 32 characters")
	}
	return nil
}

func validateV2URL(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s base url must be absolute", name)
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "preview") || strings.Contains(lower, "legacy") || strings.Contains(lower, "v1") {
		return fmt.Errorf("%s base url points to a forbidden non-v2 target", name)
	}
	return nil
}
