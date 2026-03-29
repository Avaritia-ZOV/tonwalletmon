package cursor

import (
	"path/filepath"
	"testing"

	"ton-monitoring/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_SaveAndLoad(t *testing.T) {
	s := openTestStore(t)

	want := domain.Cursor{
		AccountID: "EQAbc123",
		TxHash:    "aabbccdd",
		Lt:        42,
	}

	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load(want.AccountID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.AccountID != want.AccountID {
		t.Errorf("AccountID = %q, want %q", got.AccountID, want.AccountID)
	}
	if got.TxHash != want.TxHash {
		t.Errorf("TxHash = %q, want %q", got.TxHash, want.TxHash)
	}
	if got.Lt != want.Lt {
		t.Errorf("Lt = %d, want %d", got.Lt, want.Lt)
	}
}

func TestStore_LoadNotFound(t *testing.T) {
	s := openTestStore(t)

	got, err := s.Load("nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.AccountID != "nonexistent" {
		t.Errorf("AccountID = %q, want %q", got.AccountID, "nonexistent")
	}
	if got.TxHash != "" {
		t.Errorf("TxHash = %q, want empty", got.TxHash)
	}
	if got.Lt != 0 {
		t.Errorf("Lt = %d, want 0", got.Lt)
	}
}

func TestStore_SaveOverwrite(t *testing.T) {
	s := openTestStore(t)

	first := domain.Cursor{
		AccountID: "EQAbc123",
		TxHash:    "hash1",
		Lt:        10,
	}
	if err := s.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}

	second := domain.Cursor{
		AccountID: "EQAbc123",
		TxHash:    "hash2",
		Lt:        20,
	}
	if err := s.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	got, err := s.Load("EQAbc123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.TxHash != "hash2" {
		t.Errorf("TxHash = %q, want %q", got.TxHash, "hash2")
	}
	if got.Lt != 20 {
		t.Errorf("Lt = %d, want 20", got.Lt)
	}
}

func TestStore_LoadAll(t *testing.T) {
	s := openTestStore(t)

	accounts := []domain.Cursor{
		{AccountID: "acct-a", TxHash: "ha", Lt: 1},
		{AccountID: "acct-b", TxHash: "hb", Lt: 2},
		{AccountID: "acct-c", TxHash: "hc", Lt: 3},
	}

	for _, c := range accounts {
		if err := s.Save(c); err != nil {
			t.Fatalf("Save(%s): %v", c.AccountID, err)
		}
	}

	all, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(all) != len(accounts) {
		t.Fatalf("LoadAll returned %d cursors, want %d", len(all), len(accounts))
	}

	byID := make(map[string]domain.Cursor, len(all))
	for _, c := range all {
		byID[c.AccountID] = c
	}

	for _, want := range accounts {
		got, ok := byID[want.AccountID]
		if !ok {
			t.Errorf("missing cursor for %s", want.AccountID)
			continue
		}
		if got.TxHash != want.TxHash {
			t.Errorf("%s: TxHash = %q, want %q", want.AccountID, got.TxHash, want.TxHash)
		}
		if got.Lt != want.Lt {
			t.Errorf("%s: Lt = %d, want %d", want.AccountID, got.Lt, want.Lt)
		}
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	// Open, save, close.
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}

	want := domain.Cursor{
		AccountID: "EQPersist",
		TxHash:    "deadbeef",
		Lt:        999,
	}
	if err := s1.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close (first): %v", err)
	}

	// Reopen and verify.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer s2.Close()

	got, err := s2.Load(want.AccountID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.AccountID != want.AccountID {
		t.Errorf("AccountID = %q, want %q", got.AccountID, want.AccountID)
	}
	if got.TxHash != want.TxHash {
		t.Errorf("TxHash = %q, want %q", got.TxHash, want.TxHash)
	}
	if got.Lt != want.Lt {
		t.Errorf("Lt = %d, want %d", got.Lt, want.Lt)
	}
}

func TestStore_OpenCreatesDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "deep", "nested", "dir", "cursor.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open with nested path: %v", err)
	}
	defer s.Close()

	// Verify the store is functional.
	c := domain.Cursor{AccountID: "test", TxHash: "abc", Lt: 1}
	if err := s.Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.TxHash != "abc" {
		t.Errorf("TxHash = %q, want %q", got.TxHash, "abc")
	}
}
