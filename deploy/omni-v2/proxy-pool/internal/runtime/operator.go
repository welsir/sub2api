package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/welsir/sub2api-v2-proxy-pool/internal/pool"
	"github.com/welsir/sub2api-v2-proxy-pool/internal/sub2api"
)

type Admin interface {
	EnsureManagedProxies(ctx context.Context, host string, laneCount int) (sub2api.ManagedTopology, error)
	ListEligibleOpenAIOAuth(ctx context.Context) ([]sub2api.Account, error)
	UpdateAccountProxy(ctx context.Context, accountID, proxyID int64) error
	TestAccount(ctx context.Context, accountID int64, model, prompt string) error
}

type OperationResult struct {
	Action      string `json:"action"`
	AccountID   int64  `json:"account_id,omitempty"`
	FromProxyID int64  `json:"from_proxy_id,omitempty"`
	ToProxyID   int64  `json:"to_proxy_id,omitempty"`
	Accounts    int    `json:"accounts"`
	Migrated    int    `json:"migrated"`
	Failed      int    `json:"failed"`
	Skipped     bool   `json:"skipped,omitempty"`
	Message     string `json:"message,omitempty"`
}

type Operator struct {
	admin           Admin
	productionLanes int
	rollbackProxyID int64
	prepareCanary   func(context.Context) (string, error)
}

func (o *Operator) SetCanaryPreparer(prepare func(context.Context) (string, error)) {
	o.prepareCanary = prepare
}

func NewOperator(admin Admin, productionLanes int, rollbackProxyID int64) *Operator {
	return &Operator{admin: admin, productionLanes: productionLanes, rollbackProxyID: rollbackProxyID}
}

func (o *Operator) EnsureTopology(ctx context.Context) (sub2api.ManagedTopology, error) {
	return o.admin.EnsureManagedProxies(ctx, "mihomo", o.productionLanes)
}

func (o *Operator) StartCanary(ctx context.Context) (OperationResult, error) {
	topology, err := o.EnsureTopology(ctx)
	if err != nil {
		return OperationResult{Action: "canary"}, err
	}
	accounts, err := o.currentAccounts(ctx)
	if err != nil {
		return OperationResult{Action: "canary"}, err
	}
	if len(accounts) == 0 {
		return OperationResult{Action: "canary", Skipped: true, Message: "no eligible V2 OAuth account"}, nil
	}
	selectedNode := ""
	if o.prepareCanary != nil {
		selectedNode, err = o.prepareCanary(ctx)
		if err != nil {
			return OperationResult{Action: "canary", Accounts: len(accounts)}, err
		}
	}
	account := accounts[0]
	oldProxyID := o.accountProxyID(account)
	result := OperationResult{
		Action: "canary", AccountID: account.ID, FromProxyID: oldProxyID,
		ToProxyID: topology.Canary.ID, Accounts: len(accounts),
	}
	if err := pool.MigrateAccount(ctx, o.admin, pool.ManagedAccount{ID: account.ID, ProxyID: oldProxyID}, topology.Canary.ID); err != nil {
		result.Failed = 1
		return result, err
	}
	result.Migrated = 1
	if selectedNode != "" {
		result.Message = "canary node: " + selectedNode
	}
	return result, nil
}

func (o *Operator) Reconcile(ctx context.Context) (OperationResult, error) {
	topology, err := o.EnsureTopology(ctx)
	if err != nil {
		return OperationResult{Action: "reconcile"}, err
	}
	accounts, err := o.currentAccounts(ctx)
	if err != nil {
		return OperationResult{Action: "reconcile"}, err
	}
	result := OperationResult{Action: "reconcile", Accounts: len(accounts)}
	if len(accounts) == 0 {
		result.Skipped = true
		result.Message = "no eligible V2 OAuth account"
		return result, nil
	}
	lanes := make([]pool.Lane, 0, len(topology.Lanes))
	managed := make([]pool.ManagedAccount, 0, len(accounts))
	for index, proxy := range topology.Lanes {
		lanes = append(lanes, pool.Lane{Number: index + 1, ManagedProxyID: proxy.ID})
	}
	for _, account := range accounts {
		managed = append(managed, pool.ManagedAccount{ID: account.ID, ProxyID: o.accountProxyID(account)})
	}
	assignments := pool.AssignAccounts(managed, lanes)
	var failures []error
	for _, account := range managed {
		if err := pool.MigrateAccount(ctx, o.admin, account, assignments[account.ID]); err != nil {
			result.Failed++
			failures = append(failures, err)
			continue
		}
		result.Migrated++
	}
	return result, errors.Join(failures...)
}

func (o *Operator) Rollback(ctx context.Context) (OperationResult, error) {
	accounts, err := o.currentAccounts(ctx)
	if err != nil {
		return OperationResult{Action: "rollback"}, err
	}
	result := OperationResult{Action: "rollback", Accounts: len(accounts), ToProxyID: o.rollbackProxyID}
	var failures []error
	for _, account := range accounts {
		if o.accountProxyID(account) == o.rollbackProxyID {
			continue
		}
		if err := o.admin.UpdateAccountProxy(ctx, account.ID, o.rollbackProxyID); err != nil {
			result.Failed++
			failures = append(failures, fmt.Errorf("rollback V2 account %d: %w", account.ID, err))
			continue
		}
		result.Migrated++
	}
	if len(accounts) == 0 {
		result.Skipped = true
		result.Message = "no eligible V2 OAuth account"
	}
	return result, errors.Join(failures...)
}

func (o *Operator) currentAccounts(ctx context.Context) ([]sub2api.Account, error) {
	accounts, err := o.admin.ListEligibleOpenAIOAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("list current V2 OAuth accounts: %w", err)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

func (o *Operator) accountProxyID(account sub2api.Account) int64 {
	if account.ProxyID == nil || *account.ProxyID == 0 {
		return o.rollbackProxyID
	}
	return *account.ProxyID
}
