package pool

import (
	"context"
	"errors"
	"testing"
)

func TestAffinityHandlesZeroAndOneAccount(t *testing.T) {
	lanes := []Lane{{Number: 1, ManagedProxyID: 21}, {Number: 2, ManagedProxyID: 22}}
	if got := AssignAccounts(nil, lanes); len(got) != 0 {
		t.Fatalf("zero accounts assignment = %+v", got)
	}
	got := AssignAccounts([]ManagedAccount{{ID: 99, ProxyID: 6}}, lanes)
	if len(got) != 1 || (got[99] != 21 && got[99] != 22) {
		t.Fatalf("single account assignment = %+v", got)
	}
}

func TestAffinityIsStableWhenLaneSetDoesNotChange(t *testing.T) {
	accounts := []ManagedAccount{{ID: 1}, {ID: 2}, {ID: 3}}
	lanes := []Lane{{Number: 1, ManagedProxyID: 21}, {Number: 2, ManagedProxyID: 22}, {Number: 3, ManagedProxyID: 23}}
	first := AssignAccounts(accounts, lanes)
	second := AssignAccounts(accounts, lanes)
	for id, proxyID := range first {
		if second[id] != proxyID {
			t.Fatalf("account %d moved from %d to %d", id, proxyID, second[id])
		}
	}
}

func TestAffinityMigrationRollsBackFailedAccount(t *testing.T) {
	manager := &fakeAccountManager{accounts: map[int64]int64{1: 6}, testErr: errors.New("invalid SSE")}
	err := MigrateAccount(context.Background(), manager, ManagedAccount{ID: 1, ProxyID: 6}, 21)
	if err == nil {
		t.Fatal("MigrateAccount() succeeded")
	}
	if manager.accounts[1] != 6 {
		t.Fatalf("account proxy = %d, want rollback 6", manager.accounts[1])
	}
	if len(manager.updates) != 2 || manager.updates[0] != 21 || manager.updates[1] != 6 {
		t.Fatalf("updates = %+v", manager.updates)
	}
}

func TestAffinityMigrationKeepsSuccessfulTarget(t *testing.T) {
	manager := &fakeAccountManager{accounts: map[int64]int64{1: 6}}
	if err := MigrateAccount(context.Background(), manager, ManagedAccount{ID: 1, ProxyID: 6}, 21); err != nil {
		t.Fatalf("MigrateAccount() error = %v", err)
	}
	if manager.accounts[1] != 21 {
		t.Fatalf("account proxy = %d", manager.accounts[1])
	}
}

type fakeAccountManager struct {
	accounts map[int64]int64
	updates  []int64
	testErr  error
}

func (f *fakeAccountManager) UpdateAccountProxy(_ context.Context, accountID, proxyID int64) error {
	f.accounts[accountID] = proxyID
	f.updates = append(f.updates, proxyID)
	return nil
}

func (f *fakeAccountManager) TestAccount(context.Context, int64, string, string) error {
	return f.testErr
}
