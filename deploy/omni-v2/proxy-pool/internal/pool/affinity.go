package pool

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
)

const (
	probeModel  = "gpt-5.4-mini"
	probePrompt = "hi"
)

type ManagedAccount struct {
	ID      int64
	ProxyID int64
}

type AccountManager interface {
	UpdateAccountProxy(ctx context.Context, accountID, proxyID int64) error
	TestAccount(ctx context.Context, accountID int64, model, prompt string) error
}

func AssignAccounts(accounts []ManagedAccount, lanes []Lane) map[int64]int64 {
	assignments := make(map[int64]int64, len(accounts))
	if len(lanes) == 0 {
		return assignments
	}
	for _, account := range accounts {
		var selected int64
		var highest uint64
		for index, lane := range lanes {
			score := rendezvousScore(account.ID, lane.Number)
			if index == 0 || score > highest {
				highest = score
				selected = lane.ManagedProxyID
			}
		}
		assignments[account.ID] = selected
	}
	return assignments
}

func rendezvousScore(accountID int64, laneNumber int) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strconv.FormatInt(accountID, 10)))
	_, _ = hash.Write([]byte{'/'})
	_, _ = hash.Write([]byte(strconv.Itoa(laneNumber)))
	return hash.Sum64()
}

func MigrateAccount(ctx context.Context, manager AccountManager, account ManagedAccount, targetProxyID int64) error {
	if account.ProxyID == targetProxyID {
		return nil
	}
	if err := manager.UpdateAccountProxy(ctx, account.ID, targetProxyID); err != nil {
		return fmt.Errorf("update V2 account %d proxy: %w", account.ID, err)
	}
	if err := manager.TestAccount(ctx, account.ID, probeModel, probePrompt); err != nil {
		if rollbackErr := manager.UpdateAccountProxy(ctx, account.ID, account.ProxyID); rollbackErr != nil {
			return fmt.Errorf("test V2 account %d after migration: %v; rollback failed: %w", account.ID, err, rollbackErr)
		}
		return fmt.Errorf("test V2 account %d after migration: %w; old proxy restored", account.ID, err)
	}
	return nil
}
