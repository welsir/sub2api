package runtime

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/mihomo"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
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
}

type Engine struct {
	cfg      pool.Config
	mihomo   Mihomo
	probe    ProbeFunc
	provider string
	mu       sync.Mutex
	cursor   int
	health   map[string]*nodeHealth
}

func NewEngine(cfg pool.Config, client Mihomo, probe ProbeFunc) *Engine {
	return &Engine{
		cfg:      cfg,
		mihomo:   client,
		probe:    probe,
		provider: "wgetcloud",
		health:   make(map[string]*nodeHealth),
	}
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
	for index := 0; index < probeCount; index++ {
		node := discovered[(e.cursor+index)%len(discovered)]
		group := fmt.Sprintf("V2-PROBE-%d", index+1)
		if _, err := e.mihomo.SelectAndConfirm(ctx, group, node.Name); err != nil {
			e.record(node.Key, pool.ProbeResult{Failure: pool.FailureTransport})
			continue
		}
		result := e.probe(ctx, group, node.Name)
		e.record(node.Key, result)
	}
	e.cursor = (e.cursor + probeCount) % len(discovered)

	lanes := make([]pool.Lane, 0, e.cfg.ProductionLanes)
	for number := 1; number <= e.cfg.ProductionLanes; number++ {
		group := fmt.Sprintf("V2-LANE-%d", number)
		actual, err := e.mihomo.Current(ctx, group)
		if err != nil {
			return pool.CycleResult{}, fmt.Errorf("read %s: %w", group, err)
		}
		lanes = append(lanes, pool.Lane{Number: number, ActualNodeKey: e.provider + "/" + actual})
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
	return pool.CycleResult{Nodes: discovered, Lanes: lanes}, nil
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
