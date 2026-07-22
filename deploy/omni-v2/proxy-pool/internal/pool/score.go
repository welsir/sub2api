package pool

import "time"

type Metrics struct {
	SuccessRate          float64
	ConsecutiveSuccesses int
	ConsecutiveFailures  int
	P95                  time.Duration
	SlowWindows          int
}

func EligibleForProduction(metrics Metrics, cfg Config) bool {
	return metrics.SuccessRate >= cfg.MinSuccessRate &&
		metrics.ConsecutiveSuccesses >= cfg.MinConsecutiveSuccesses &&
		metrics.P95 > 0 && metrics.P95 <= cfg.MaxP95
}

func RequiresEmergencyReplacement(metrics Metrics, cfg Config) bool {
	return metrics.ConsecutiveFailures >= cfg.FailureThreshold
}

func ShouldPerformanceReplace(current, candidate Metrics, lastReplacement, now time.Time, cfg Config) bool {
	if current.SlowWindows < cfg.SlowWindowThreshold {
		return false
	}
	if now.Sub(lastReplacement) < cfg.ReplacementCooldown {
		return false
	}
	if !EligibleForProduction(candidate, cfg) || candidate.SuccessRate < current.SuccessRate {
		return false
	}
	if current.P95 <= 0 || candidate.P95 <= 0 {
		return false
	}
	maximumCandidateP95 := time.Duration(float64(current.P95) * (1 - cfg.MinPerformanceImprovement))
	return candidate.P95 <= maximumCandidateP95
}
