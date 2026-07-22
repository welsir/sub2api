package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Snapshot struct {
	Mode       string `json:"mode"`
	Generation int64  `json:"generation"`
}

type AuditEvent struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Result string    `json:"result"`
	Detail string    `json:"detail,omitempty"`
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) Save(snapshot Snapshot) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	temporary := filepath.Join(s.dir, "state.json.tmp")
	final := filepath.Join(s.dir, "state.json")
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := os.Rename(temporary, final); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (s *Store) Load() (Snapshot, error) {
	var snapshot Snapshot
	data, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err != nil {
		return snapshot, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode state: %w", err)
	}
	return snapshot, nil
}

func (s *Store) AppendAudit(event AuditEvent) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode audit: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.dir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}
