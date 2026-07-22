package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/mihomo"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
)

func TestEngineFiltersPseudoNodesAndRotatesThreeCandidates(t *testing.T) {
	fake := newFakeMihomo([]string{
		"香港01", "香港02", "香港03", "新加坡01", "新加坡02", "日本01",
		"剩余流量：500GB", "距离下次重置：12天", "套餐到期：2026-12-31",
	})
	engine := NewEngine(pool.DefaultConfig(), fake, func(context.Context, string, string) pool.ProbeResult {
		return pool.ProbeResult{Success: true, Reachable: true}
	})

	first, err := engine.Cycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Cycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Nodes) != 6 || len(first.Lanes) != 5 {
		t.Fatalf("first cycle nodes=%d lanes=%d", len(first.Nodes), len(first.Lanes))
	}
	if got := fake.selections[:3]; fmt.Sprint(got) != "[香港01 香港02 香港03]" {
		t.Fatalf("first candidates = %v", got)
	}
	if got := fake.selections[3:6]; fmt.Sprint(got) != "[新加坡01 新加坡02 日本01]" {
		t.Fatalf("second candidates = %v", got)
	}
	_ = second
}

func TestEnginePromotesNodeOnlyAfterThreeSuccessfulSamples(t *testing.T) {
	fake := newFakeMihomo([]string{"香港01", "香港02", "香港03"})
	engine := NewEngine(pool.DefaultConfig(), fake, func(context.Context, string, string) pool.ProbeResult {
		return pool.ProbeResult{Success: true, Reachable: true, Duration: 100}
	})
	for cycle := 1; cycle <= 3; cycle++ {
		result, err := engine.Cycle(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range result.Nodes {
			if cycle < 3 && node.State == pool.NodeStable {
				t.Fatalf("node became stable on cycle %d", cycle)
			}
			if cycle == 3 && node.State != pool.NodeStable {
				t.Fatalf("node state after three successes = %s", node.State)
			}
		}
	}
}

type fakeMihomo struct {
	nodes      []mihomo.ProviderNode
	current    map[string]string
	selections []string
}

func newFakeMihomo(names []string) *fakeMihomo {
	nodes := make([]mihomo.ProviderNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, mihomo.ProviderNode{Name: name, Alive: true})
	}
	current := map[string]string{}
	for i := 1; i <= 5; i++ {
		current[fmt.Sprintf("V2-LANE-%d", i)] = names[0]
	}
	return &fakeMihomo{nodes: nodes, current: current}
}

func (f *fakeMihomo) ListProvider(context.Context, string) ([]mihomo.ProviderNode, error) {
	return f.nodes, nil
}

func (f *fakeMihomo) Current(_ context.Context, group string) (string, error) {
	return f.current[group], nil
}

func (f *fakeMihomo) SelectAndConfirm(_ context.Context, group, node string) (string, error) {
	f.current[group] = node
	f.selections = append(f.selections, node)
	return node, nil
}
