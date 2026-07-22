package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

func TestAccountProbeUsesSingleDynamicAccountAndRestoresItsProxy(t *testing.T) {
	old := int64(6)
	admin := &fakeAdmin{
		accounts: []sub2api.Account{{ID: 42, ProxyID: &old}},
		testErr:  map[int64]error{},
	}
	prober := NewAccountProbe(admin, []sub2api.Proxy{{ID: 31}, {ID: 32}, {ID: 33}}, 6)
	result := prober.Probe(context.Background(), "V2-PROBE-2", "香港02")
	if !result.Success || !result.Reachable || result.Duration <= 0 {
		t.Fatalf("Probe() = %+v", result)
	}
	if got := strings.Join(admin.updates, ","); got != "42:32,42:6" {
		t.Fatalf("updates = %s", got)
	}
}

func TestAccountProbeWithNoAccountCannotPromoteNode(t *testing.T) {
	admin := &fakeAdmin{testErr: map[int64]error{}}
	prober := NewAccountProbe(admin, []sub2api.Proxy{{ID: 31}}, 6)
	result := prober.Probe(context.Background(), "V2-PROBE-1", "香港01")
	if result.Success || result.Failure != pool.FailureTransport || !strings.Contains(result.Message, "no eligible") {
		t.Fatalf("Probe() = %+v", result)
	}
}

func TestAccountProbeTreatsInvalidSSEAsFailureAndStillRestores(t *testing.T) {
	old := int64(21)
	admin := &fakeAdmin{
		accounts: []sub2api.Account{{ID: 7, ProxyID: &old}},
		testErr:  map[int64]error{7: context.DeadlineExceeded},
	}
	prober := NewAccountProbe(admin, []sub2api.Proxy{{ID: 31}}, 6)
	result := prober.Probe(context.Background(), "V2-PROBE-1", "香港01")
	if result.Success || strings.Join(admin.updates, ",") != "7:31,7:21" {
		t.Fatalf("Probe() = %+v updates=%v", result, admin.updates)
	}
}
