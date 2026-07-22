package pool

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var ErrCycleAlreadyRunning = errors.New("proxy pool cycle already running")

type Proposal struct {
	Description string
	Apply       func(context.Context) error
}

type CycleResult struct {
	Nodes    []Node
	Lanes    []Lane
	Proposal *Proposal
}

type CycleFunc func(context.Context) (CycleResult, error)
type OperationFunc func(context.Context) (any, error)

type ControllerStatus struct {
	Mode         Mode      `json:"mode"`
	Running      bool      `json:"running"`
	LastCycleAt  time.Time `json:"last_cycle_at,omitempty"`
	LastProposal string    `json:"last_proposal,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	NodeCount    int       `json:"node_count"`
	LaneCount    int       `json:"lane_count"`
	Nodes        []Node    `json:"nodes,omitempty"`
	Lanes        []Lane    `json:"lanes,omitempty"`
}

type Controller struct {
	cfg        Config
	cycle      CycleFunc
	running    atomic.Bool
	mu         sync.RWMutex
	status     ControllerStatus
	operations map[string]OperationFunc
}

func NewController(cfg Config, cycle CycleFunc) *Controller {
	return &Controller{
		cfg:   cfg,
		cycle: cycle,
		status: ControllerStatus{
			Mode: cfg.Mode,
		},
		operations: make(map[string]OperationFunc),
	}
}

func (c *Controller) SetOperation(name string, operation OperationFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if operation == nil {
		delete(c.operations, name)
		return
	}
	c.operations[name] = operation
}

func (c *Controller) RunOnce(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return ErrCycleAlreadyRunning
	}
	defer c.running.Store(false)

	result, err := c.cycle(ctx)
	c.mu.Lock()
	c.status.LastCycleAt = time.Now().UTC()
	c.status.NodeCount = len(result.Nodes)
	c.status.LaneCount = len(result.Lanes)
	c.status.Nodes = append([]Node(nil), result.Nodes...)
	c.status.Lanes = append([]Lane(nil), result.Lanes...)
	c.status.LastError = ""
	if result.Proposal != nil {
		c.status.LastProposal = result.Proposal.Description
	}
	mode := c.status.Mode
	c.mu.Unlock()
	if err != nil {
		c.setLastError(err)
		return err
	}
	if result.Proposal != nil && mode == ModeAuto {
		if result.Proposal.Apply == nil {
			err := fmt.Errorf("proposal %q has no apply function", result.Proposal.Description)
			c.setLastError(err)
			return err
		}
		if err := result.Proposal.Apply(ctx); err != nil {
			c.setLastError(err)
			return err
		}
	}
	return nil
}

func (c *Controller) Start(ctx context.Context) {
	go func() {
		_ = c.RunOnce(ctx)
		ticker := time.NewTicker(c.cfg.ProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = c.RunOnce(ctx)
			}
		}
	}()
}

func (c *Controller) Status() ControllerStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := c.status
	status.Running = c.running.Load()
	return status
}

func (c *Controller) SetMode(mode Mode) error {
	if mode != ModeObserve && mode != ModeAuto {
		return fmt.Errorf("mode must be observe or auto")
	}
	c.mu.Lock()
	c.status.Mode = mode
	c.mu.Unlock()
	return nil
}

func (c *Controller) setLastError(err error) {
	c.mu.Lock()
	c.status.LastError = err.Error()
	c.mu.Unlock()
}

func (c *Controller) Handler(secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, c.Status())
	})
	mux.HandleFunc("PUT /mode", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Mode Mode `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := c.SetMode(request.Mode); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, c.Status())
	})
	mux.HandleFunc("POST /operations/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Proxy-Pool-Confirm") != "v2-only" {
			writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "X-Proxy-Pool-Confirm must be v2-only"})
			return
		}
		name := r.PathValue("name")
		c.mu.RLock()
		operation := c.operations[name]
		c.mu.RUnlock()
		if operation == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown operation"})
			return
		}
		result, err := operation(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "result": result})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := []byte(r.Header.Get("X-Proxy-Pool-Secret"))
		expected := []byte(secret)
		if len(expected) < 16 || len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
