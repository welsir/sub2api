package pool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerObserveRecordsButDoesNotApplyProposal(t *testing.T) {
	var applied atomic.Int32
	controller := NewController(DefaultConfig(), func(context.Context) (CycleResult, error) {
		return CycleResult{Nodes: []Node{{Key: "wgetcloud/hk-01"}}, Lanes: []Lane{{Number: 1}}, Proposal: &Proposal{Description: "switch lane 1", Apply: func(context.Context) error {
			applied.Add(1)
			return nil
		}}}, nil
	})

	if err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applied.Load() != 0 {
		t.Fatal("observe mode applied a proposal")
	}
	if controller.Status().LastProposal != "switch lane 1" {
		t.Fatalf("status = %+v", controller.Status())
	}
	if len(controller.Status().Nodes) != 1 || len(controller.Status().Lanes) != 1 {
		t.Fatalf("status details = %+v", controller.Status())
	}
}

func TestControllerAutoAppliesProposal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeAuto
	var applied atomic.Int32
	controller := NewController(cfg, func(context.Context) (CycleResult, error) {
		return CycleResult{Proposal: &Proposal{Description: "switch lane 1", Apply: func(context.Context) error {
			applied.Add(1)
			return nil
		}}}, nil
	})

	if err := controller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applied.Load() != 1 {
		t.Fatalf("proposal applications = %d", applied.Load())
	}
}

func TestControllerDoesNotOverlapCycles(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	controller := NewController(DefaultConfig(), func(context.Context) (CycleResult, error) {
		close(entered)
		<-release
		return CycleResult{}, nil
	})
	done := make(chan error, 1)
	go func() { done <- controller.RunOnce(context.Background()) }()
	<-entered
	if err := controller.RunOnce(context.Background()); !errors.Is(err, ErrCycleAlreadyRunning) {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestControllerAPIRequiresSecretAndChangesMode(t *testing.T) {
	controller := NewController(DefaultConfig(), func(context.Context) (CycleResult, error) { return CycleResult{}, nil })
	server := httptest.NewServer(controller.Handler("internal-secret-value"))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPut, server.URL+"/mode", strings.NewReader(`{"mode":"auto"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPut, server.URL+"/mode", strings.NewReader(`{"mode":"auto"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Proxy-Pool-Secret", "internal-secret-value")
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("mode status = %d", response.StatusCode)
	}
	var status ControllerStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Mode != ModeAuto || controller.Status().Mode != ModeAuto {
		t.Fatalf("status = %+v", status)
	}
}

func TestControllerTickerUsesConfiguredInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProbeInterval = 10 * time.Millisecond
	var cycles atomic.Int32
	controller := NewController(cfg, func(context.Context) (CycleResult, error) {
		cycles.Add(1)
		return CycleResult{}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()
	controller.Start(ctx)
	<-ctx.Done()
	if cycles.Load() < 2 {
		t.Fatalf("cycles = %d", cycles.Load())
	}
}
