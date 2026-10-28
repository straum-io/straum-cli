package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPositionStore(t *testing.T) {
	s := positionStore{filepath.Join(t.TempDir(), "straum-cli", "default")}
	if got, err := s.saved("c", "t", "n"); err != nil || len(got) != 0 {
		t.Fatalf("no file: %v %v", got, err)
	}
	if err := s.save("c", "t", "n", map[uint32]uint64{0: 5}); err != nil {
		t.Fatal(err)
	}
	// Different names, topics and clusters are kept apart; a save replaces
	// the entry.
	for _, k := range []struct{ c, t, n string }{{"c", "t", "m"}, {"c", "u", "n"}, {"d", "t", "n"}} {
		if err := s.save(k.c, k.t, k.n, map[uint32]uint64{0: 99}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.save("c", "t", "n", map[uint32]uint64{0: 7}); err != nil {
		t.Fatal(err)
	}
	got, err := s.saved("c", "t", "n")
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("%v %v", got, err)
	}
	f, _ := s.load()
	if len(f.Positions) != 4 {
		t.Fatalf("%+v", f.Positions)
	}
	// No temporary file is left next to the state file.
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("left over: %s", e.Name())
		}
	}

	// Concurrent saves of different names lose nothing (the file lock).
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := s.save("c", "t", "w"+string(rune('a'+i)), map[uint32]uint64{0: uint64(i)}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if f, _ := s.load(); len(f.Positions) != 24 {
		t.Fatalf("%d positions after concurrent saves, want 24", len(f.Positions))
	}

	if err := os.WriteFile(s.path(), []byte(`{"version":2,"positions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); exitOf(err) != exitUsage || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("future version: %v", err)
	}
}

// A consumer name is held by one process at a time.
func TestNameLock(t *testing.T) {
	s := positionStore{t.TempDir()}
	unlock, err := s.lockName("g")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.lockName("g"); exitOf(err) != exitUsage || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("second holder: %v", err)
	}
	other, err := s.lockName("h")
	if err != nil {
		t.Fatal(err)
	}
	other()
	unlock()
	again, err := s.lockName("g")
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	again()
}

func TestPositionsCommands(t *testing.T) {
	c := newCLI(t)
	s := positionStore{filepath.Join(c.env["XDG_STATE_HOME"], "straum-cli", "default")}
	for _, k := range []struct{ t, n string }{{"orders", "billing"}, {"orders", "audit"}, {"payments", "billing"}} {
		if err := s.save("prod", k.t, k.n, map[uint32]uint64{0: 3}); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := c.run("", "positions", "list", "--topic", "orders")
	if code != 0 || strings.Count(out, "\n") != 3 || !strings.Contains(out, "audit") || strings.Contains(out, "payments") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, _, errOut := c.run("", "positions", "reset", "orders", "--name", "billing"); code != 0 {
		t.Fatalf("reset: %d %s", code, errOut)
	}
	code, out, _ = c.run("", "positions", "list", "-o", "json")
	if code != 0 || strings.Count(out, "\n") != 2 || strings.Contains(out, `"topic":"orders","partition":0,"name":"billing"`) {
		t.Fatalf("after reset: %d %q", code, out)
	}
	// A name held by a running consume cannot be reset.
	unlock, err := s.lockName("audit")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if code, _, errOut := c.run("", "positions", "reset", "orders", "--name", "audit"); code != exitUsage || !strings.Contains(errOut, "in use") {
		t.Fatalf("reset while held: %d %s", code, errOut)
	}
}
