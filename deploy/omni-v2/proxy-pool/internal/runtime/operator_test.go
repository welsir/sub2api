package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

type fakeAdmin struct {
	accounts []sub2api.Account
	topology sub2api.ManagedTopology
	updates  []string
	testErr  map[int64]error
}

func (f *fakeAdmin) EnsureManagedProxies(context.Context, string, int) (sub2api.ManagedTopology, error) {
	return f.topology, nil
}

func (f *fakeAdmin) ListEligibleOpenAIOAuth(context.Context) ([]sub2api.Account, error) {
	return append([]sub2api.Account(nil), f.accounts...), nil
}

func (f *fakeAdmin) UpdateAccountProxy(_ context.Context, accountID, proxyID int64) error {
	f.updates = append(f.updates, fmt.Sprintf("%d:%d", accountID, proxyID))
	for index := range f.accounts {
		if f.accounts[index].ID == accountID {
			value := proxyID
			f.accounts[index].ProxyID = &value
		}
	}
	return nil
}

func (f *fakeAdmin) TestAccount(_ context.Context, accountID int64, _, _ string) error {
	return f.testErr[accountID]
}

func TestOperatorCanaryHandlesZeroAndOneAccount(t *testing.T) {
	admin := &fakeAdmin{topology: topologyForTest(), testErr: map[int64]error{}}
	operator := NewOperator(admin, 5, 6)
	result, err := operator.StartCanary(context.Background())
	if err != nil || !result.Skipped || result.AccountID != 0 {
		t.Fatalf("zero-account canary = %+v, %v", result, err)
	}

	old := int64(6)
	admin.accounts = []sub2api.Account{{ID: 42, ProxyID: &old}}
	result, err = operator.StartCanary(context.Background())
	if err != nil {
		t.Fatalf("one-account canary error = %v", err)
	}
	if result.AccountID != 42 || result.FromProxyID != 6 || result.ToProxyID != 30 || len(admin.updates) != 1 {
		t.Fatalf("one-account canary = %+v updates=%v", result, admin.updates)
	}
}

func TestOperatorReconcileUsesCurrentAccountsAndRollsBackFailedAccount(t *testing.T) {
	old := int64(6)
	admin := &fakeAdmin{
		topology: topologyForTest(),
		accounts: []sub2api.Account{{ID: 1, ProxyID: &old}, {ID: 2, ProxyID: &old}},
		testErr:  map[int64]error{2: fmt.Errorf("bad SSE")},
	}
	operator := NewOperator(admin, 5, 6)
	result, err := operator.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() succeeded, want account failure")
	}
	if result.Migrated != 1 || result.Failed != 1 || len(admin.updates) != 3 {
		t.Fatalf("result=%+v updates=%v", result, admin.updates)
	}
	if admin.updates[2] != "2:6" {
		t.Fatalf("failed account was not restored: %v", admin.updates)
	}
}

func TestOperatorRollbackForcesAllCurrentAccountsToProxySix(t *testing.T) {
	laneA, laneB := int64(21), int64(22)
	admin := &fakeAdmin{
		topology: topologyForTest(),
		accounts: []sub2api.Account{{ID: 1, ProxyID: &laneA}, {ID: 2, ProxyID: &laneB}},
		testErr:  map[int64]error{},
	}
	operator := NewOperator(admin, 5, 6)
	result, err := operator.Rollback(context.Background())
	if err != nil || result.Migrated != 2 {
		t.Fatalf("Rollback() = %+v, %v", result, err)
	}
	if fmt.Sprint(admin.updates) != "[1:6 2:6]" {
		t.Fatalf("updates = %v", admin.updates)
	}
}

func topologyForTest() sub2api.ManagedTopology {
	return sub2api.ManagedTopology{
		Lanes: []sub2api.Proxy{
			{ID: 21, Name: "v2-stable-lane-1", Port: 19081},
			{ID: 22, Name: "v2-stable-lane-2", Port: 19082},
			{ID: 23, Name: "v2-stable-lane-3", Port: 19083},
			{ID: 24, Name: "v2-stable-lane-4", Port: 19084},
			{ID: 25, Name: "v2-stable-lane-5", Port: 19085},
		},
		Canary: sub2api.Proxy{ID: 30, Name: "v2-stable-canary", Port: 19201},
	}
}
