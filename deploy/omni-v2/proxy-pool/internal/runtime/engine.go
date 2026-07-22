package runtime

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/mihomo"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

type Mihomo interface {
	ListProvider(ctx context.Context, provider string) ([]mihomo.ProviderNode, error)
	Current(ctx context.Context, group string) (string, error)
	SelectAndConfirm(ctx context.Context, group, node string) (string, error)
}

type ProbeFunc func(ctx context.Context, listener, node string) pool.ProbeResult

type nodeHealth struct {
	total       int
	successes   int
	consecutive int
	failures    int
	durations   []time.Duration
	slowWindows int
}

type Engine struct {
	cfg             pool.Config
	mihomo          Mihomo
	probe           ProbeFunc
	provider        string
	mu              sync.Mutex
	cursor          int
	health          map[string]*nodeHealth
	managedProxyIDs map[int]int64
	lastSwitched    map[int]time.Time
}

func NewEngine(cfg pool.Config, client Mihomo, probe ProbeFunc) *Engine {
	return &Engine{
		cfg:             cfg,
		mihomo:          client,
		probe:           probe,
		provider:        "wgetcloud",
		health:          make(map[string]*nodeHealth),
		managedProxyIDs: make(map[int]int64),
		lastSwitched:    make(map[int]time.Time),
	}
}

func (e *Engine) SetManagedTopology(topology sub2api.ManagedTopology) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for index, proxy := range topology.Lanes {
		e.managedProxyIDs[index+1] = proxy.ID
	}
}

func (e *Engine) PrepareCanary(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	stable := e.stableNodesLocked()
	if len(stable) == 0 {
		return "", fmt.Errorf("no stable node is eligible for V2 canary")
	}
	selected := stable[0].Name
	if _, err := e.mihomo.SelectAndConfirm(ctx, "V2-CANARY", selected); err != nil {
		return "", fmt.Errorf("select V2 canary node %q: %w", selected, err)
	}
	return selected, nil
}

func (e *Engine) Cycle(ctx context.Context) (pool.CycleResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	provided, err := e.mihomo.ListProvider(ctx, e.provider)
	if err != nil {
		return pool.CycleResult{}, fmt.Errorf("list mihomo provider: %w", err)
	}
	names := make([]string, 0, len(provided))
	for _, node := range provided {
		names = append(names, node.Name)
	}
	discovered := pool.DiscoverRealNodes(e.provider, names)
	if len(discovered) == 0 {
		return pool.CycleResult{}, fmt.Errorf("mihomo provider has no real nodes")
	}

	probeCount := min(e.cfg.CandidateProbeLanes, len(discovered))
	type probeJob struct {
		node  pool.Node
		group string
	}
	jobs := make([]probeJob, 0, probeCount)
	for index := 0; index < probeCount; index++ {
		node := discovered[(e.cursor+index)%len(discovered)]
		group := fmt.Sprintf("V2-PROBE-%d", index+1)
		if _, err := e.mihomo.SelectAndConfirm(ctx, group, node.Name); err != nil {
			e.record(node.Key, pool.ProbeResult{Failure: pool.FailureTransport})
			continue
		}
		jobs = append(jobs, probeJob{node: node, group: group})
	}
	results := make([]pool.ProbeResult, len(jobs))
	var probes sync.WaitGroup
	for index, job := range jobs {
		probes.Add(1)
		go func(index int, job probeJob) {
			defer probes.Done()
			results[index] = e.probe(ctx, job.group, job.node.Name)
		}(index, job)
	}
	probes.Wait()
	for index, job := range jobs {
		e.record(job.node.Key, results[index])
	}
	e.cursor = (e.cursor + probeCount) % len(discovered)

	lanes := make([]pool.Lane, 0, e.cfg.ProductionLanes)
	for number := 1; number <= e.cfg.ProductionLanes; number++ {
		group := fmt.Sprintf("V2-LANE-%d", number)
		actual, err := e.mihomo.Current(ctx, group)
		if err != nil {
			return pool.CycleResult{}, fmt.Errorf("read %s: %w", group, err)
		}
		lanes = append(lanes, pool.Lane{Number: number, ActualNodeKey: e.provider + "/" + actual, ManagedProxyID: e.managedProxyIDs[number]})
	}

	for index := range discovered {
		health := e.health[discovered[index].Key]
		if health == nil || health.total == 0 {
			continue
		}
		discovered[index].SuccessRate = float64(health.successes) / float64(health.total)
		discovered[index].ConsecutiveOK = health.consecutive
		discovered[index].ConsecutiveFail = health.failures
		discovered[index].P95 = percentile95(health.durations)
		metrics := pool.Metrics{
			SuccessRate:          discovered[index].SuccessRate,
			ConsecutiveSuccesses: health.consecutive,
			ConsecutiveFailures:  health.failures,
			P95:                  discovered[index].P95,
		}
		if pool.EligibleForProduction(metrics, e.cfg) {
			discovered[index].State = pool.NodeStable
		} else if health.failures >= e.cfg.FailureThreshold {
			discovered[index].State = pool.NodeQuarantined
		}
	}
	return pool.CycleResult{Nodes: discovered, Lanes: lanes, Proposal: e.proposalForLanesLocked(lanes)}, nil
}

func (e *Engine) record(key string, result pool.ProbeResult) {
	health := e.health[key]
	if health == nil {
		health = &nodeHealth{}
		e.health[key] = health
	}
	health.total++
	if result.Success {
		health.successes++
		health.consecutive++
		health.failures = 0
		if result.Duration > 0 {
			if result.Duration > e.cfg.MaxP95 {
				health.slowWindows++
			} else {
				health.slowWindows = 0
			}
			health.durations = append(health.durations, result.Duration)
			if len(health.durations) > 60 {
				health.durations = health.durations[len(health.durations)-60:]
			}
		}
		return
	}
	health.consecutive = 0
	health.failures++
}

func (e *Engine) proposalForLanesLocked(lanes []pool.Lane) *pool.Proposal {
	stable := e.stableNodesLocked()
	if len(stable) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for index, lane := range lanes {
		if index >= len(stable) {
			break
		}
		desired := stable[index]
		if lane.ActualNodeKey == desired.Key {
			continue
		}
		lastReplacement := e.lastSwitched[lane.Number]
		if !lastReplacement.IsZero() {
			current := e.metricsLocked(lane.ActualNodeKey)
			candidate := e.metricsLocked(desired.Key)
			if !pool.RequiresEmergencyReplacement(current, e.cfg) &&
				!pool.ShouldPerformanceReplace(current, candidate, lastReplacement, now, e.cfg) {
				continue
			}
		}
		laneNumber := lane.Number
		nodeName := desired.Name
		return &pool.Proposal{
			Description: fmt.Sprintf("switch V2 lane %d to %s", laneNumber, nodeName),
			Apply: func(ctx context.Context) error {
				e.mu.Lock()
				defer e.mu.Unlock()
				group := fmt.Sprintf("V2-LANE-%d", laneNumber)
				if _, err := e.mihomo.SelectAndConfirm(ctx, group, nodeName); err != nil {
					return fmt.Errorf("switch %s to %q: %w", group, nodeName, err)
				}
				e.lastSwitched[laneNumber] = time.Now().UTC()
				return nil
			},
		}
	}
	return nil
}

func (e *Engine) metricsLocked(key string) pool.Metrics {
	health := e.health[key]
	if health == nil || health.total == 0 {
		return pool.Metrics{}
	}
	return pool.Metrics{
		SuccessRate:          float64(health.successes) / float64(health.total),
		ConsecutiveSuccesses: health.consecutive,
		ConsecutiveFailures:  health.failures,
		P95:                  percentile95(health.durations),
		SlowWindows:          health.slowWindows,
	}
}

func (e *Engine) stableNodesLocked() []pool.Node {
	stable := make([]pool.Node, 0, len(e.health))
	for key, health := range e.health {
		if health.total == 0 {
			continue
		}
		metrics := pool.Metrics{
			SuccessRate:          float64(health.successes) / float64(health.total),
			ConsecutiveSuccesses: health.consecutive,
			ConsecutiveFailures:  health.failures,
			P95:                  percentile95(health.durations),
			SlowWindows:          health.slowWindows,
		}
		if !pool.EligibleForProduction(metrics, e.cfg) {
			continue
		}
		name := key
		if prefix := e.provider + "/"; len(key) > len(prefix) && key[:len(prefix)] == prefix {
			name = key[len(prefix):]
		}
		stable = append(stable, pool.Node{
			Key: key, Provider: e.provider, Name: name, State: pool.NodeStable,
			SuccessRate: metrics.SuccessRate, ConsecutiveOK: health.consecutive,
			ConsecutiveFail: health.failures, P95: metrics.P95,
		})
	}
	sort.Slice(stable, func(i, j int) bool {
		if stable[i].SuccessRate != stable[j].SuccessRate {
			return stable[i].SuccessRate > stable[j].SuccessRate
		}
		if stable[i].ConsecutiveOK != stable[j].ConsecutiveOK {
			return stable[i].ConsecutiveOK > stable[j].ConsecutiveOK
		}
		if stable[i].P95 != stable[j].P95 {
			return stable[i].P95 < stable[j].P95
		}
		return stable[i].Key < stable[j].Key
	})
	return stable
}

func percentile95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	copyOfValues := append([]time.Duration(nil), values...)
	sort.Slice(copyOfValues, func(i, j int) bool { return copyOfValues[i] < copyOfValues[j] })
	index := (95*len(copyOfValues) + 99) / 100
	if index < 1 {
		index = 1
	}
	return copyOfValues[index-1]
}
