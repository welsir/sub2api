package pool

import "time"

type Mode string

const (
	ModeObserve Mode = "observe"
	ModeAuto    Mode = "auto"
)

type NodeState string

const (
	NodeUnverified  NodeState = "unverified"
	NodeStable      NodeState = "stable"
	NodeDegraded    NodeState = "degraded"
	NodeQuarantined NodeState = "quarantined"
)

type Lane struct {
	Number          int        `json:"number"`
	ExpectedNodeKey string     `json:"expected_node_key"`
	ActualNodeKey   string     `json:"actual_node_key"`
	ManagedProxyID  int64      `json:"managed_proxy_id"`
	Pinned          bool       `json:"pinned"`
	LastSwitchedAt  *time.Time `json:"last_switched_at,omitempty"`
}

type Node struct {
	Key             string        `json:"key"`
	Provider        string        `json:"provider"`
	Name            string        `json:"name"`
	State           NodeState     `json:"state"`
	SuccessRate     float64       `json:"success_rate"`
	ConsecutiveOK   int           `json:"consecutive_ok"`
	ConsecutiveFail int           `json:"consecutive_fail"`
	P95             time.Duration `json:"p95"`
	QuarantinedAt   *time.Time    `json:"quarantined_at,omitempty"`
}
