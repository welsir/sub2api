package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWritesStateAtomicallyAndLoadsIt(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	want := Snapshot{Mode: "observe", Generation: 7}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

func TestStoreAppendsJSONLAudit(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.AppendAudit(AuditEvent{Action: "lane_switch", Result: "confirmed"}); err != nil {
		t.Fatalf("AppendAudit() error = %v", err)
	}
	if err := store.AppendAudit(AuditEvent{Action: "account_rollback", Result: "restored"}); err != nil {
		t.Fatalf("AppendAudit() error = %v", err)
	}

	file, err := os.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var event AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid audit JSON: %v", err)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("audit lines = %d, want 2", count)
	}
}
